package fsskillprovider

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	skillFileName   = "SKILL.md"
	maxSkillMDBytes = 2 << 20 // 2 MiB
)

// indexSkillDir reads and validates SKILL.md frontmatter, returning metadata and digest.
// It does NOT return the body; body is loaded separately for progressive disclosure.
func indexSkillDir(
	ctx context.Context,
	rootDir string,
) (name, description string, props map[string]any, digest string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", nil, "", err
	}

	root := strings.TrimSpace(rootDir)
	if root == "" {
		return "", "", nil, "", errors.New("empty rootDir")
	}

	root, err = filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", "", nil, "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(root); rerr == nil && resolved != "" {
		root = resolved
	}

	st, err := os.Stat(root)
	if err != nil {
		return "", "", nil, "", err
	}
	if !st.IsDir() {
		return "", "", nil, "", fmt.Errorf("not a directory: %s", root)
	}

	skillMDPath := filepath.Join(root, skillFileName)

	// Disallow SKILL.md being a symlink.
	if lst, lerr := os.Lstat(skillMDPath); lerr == nil {
		if lst.Mode()&os.ModeSymlink != 0 {
			return "", "", nil, "", errors.New("SKILL.md must not be a symlink")
		}
	}

	b, sha, err := readAllLimitedAndDigest(skillMDPath)
	if err != nil {
		return "", "", nil, "", err
	}

	fm, _, hasFM, err := splitFrontmatter(string(b))
	if err != nil {
		return "", "", nil, "", err
	}
	if !hasFM {
		return "", "", nil, "", errors.New("SKILL.md must contain YAML frontmatter")
	}

	props = map[string]any{}
	if err := yaml.Unmarshal([]byte(fm), &props); err != nil {
		return "", "", nil, "", fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	name = strings.TrimSpace(asString(props["name"]))
	description = strings.TrimSpace(asString(props["description"]))

	if err := validateName(name); err != nil {
		return "", "", nil, "", err
	}
	if err := validateDescription(description); err != nil {
		return "", "", nil, "", err
	}

	// FS convention: name must match directory name.
	if base := filepath.Base(root); base != "" && name != base {
		return "", "", nil, "", fmt.Errorf("frontmatter.name %q must match directory name %q", name, base)
	}

	return name, description, props, "sha256:" + sha, nil
}

func loadSkillBody(ctx context.Context, rootDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	root := strings.TrimSpace(rootDir)
	if root == "" {
		return "", errors.New("empty rootDir")
	}

	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(root); rerr == nil && resolved != "" {
		root = resolved
	}

	skillMDPath := filepath.Join(root, skillFileName)

	b, _, err := readAllLimitedAndDigest(skillMDPath)
	if err != nil {
		return "", err
	}

	fm, body, hasFM, err := splitFrontmatter(string(b))
	if err != nil {
		return "", err
	}
	if !hasFM {
		return "", errors.New("SKILL.md must contain YAML frontmatter")
	}

	// Validate frontmatter parses.
	props := map[string]any{}
	if err := yaml.Unmarshal([]byte(fm), &props); err != nil {
		return "", fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	body = strings.TrimLeft(body, "\r\n")
	return body, nil
}

func readAllLimitedAndDigest(path string) (data []byte, dataSHA string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	data, err = io.ReadAll(io.LimitReader(f, int64(maxSkillMDBytes)+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxSkillMDBytes {
		return nil, "", fmt.Errorf("SKILL.md too large (max %d bytes)", maxSkillMDBytes)
	}

	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

func splitFrontmatter(s string) (frontmatter, body string, has bool, err error) {
	br := bufio.NewReader(strings.NewReader(s))

	first, ferr := br.ReadString('\n')
	if ferr != nil && !errors.Is(ferr, io.EOF) {
		return "", "", false, ferr
	}
	first = strings.TrimRight(first, "\r\n")
	if strings.TrimSpace(first) != "---" {
		return "", s, false, nil
	}

	var fmLines []string
	foundEnd := false
	for {
		line, lerr := br.ReadString('\n')
		if lerr != nil && !errors.Is(lerr, io.EOF) {
			return "", "", false, lerr
		}
		lineTrim := strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(lineTrim) == "---" {
			foundEnd = true
			break
		}
		fmLines = append(fmLines, lineTrim)
		if errors.Is(lerr, io.EOF) {
			break
		}
	}

	if !foundEnd {
		return "", "", false, errors.New("unterminated frontmatter (missing closing ---)")
	}

	rest, err := io.ReadAll(br)
	if err != nil {
		return "", "", false, err
	}

	return strings.Join(fmLines, "\n"), string(rest), true, nil
}

func validateName(name string) error {
	if name == "" {
		return errors.New("frontmatter.name is required")
	}
	if len(name) > 64 {
		return errors.New("frontmatter.name too long (max 64)")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return errors.New("frontmatter.name must not start or end with '-'")
	}
	if strings.Contains(name, "--") {
		return errors.New("frontmatter.name must not contain consecutive '--'")
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("frontmatter.name contains invalid character %q", string(r))
	}
	return nil
}

func validateDescription(desc string) error {
	if desc == "" {
		return errors.New("frontmatter.description is required")
	}
	if len(desc) > 1024 {
		return errors.New("frontmatter.description too long (max 1024)")
	}
	return nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
