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

	"github.com/flexigpt/agentskills-go/spec"
	"gopkg.in/yaml.v3"
)

const (
	skillFileName   = "SKILL.md"
	maxSkillMDBytes = 2 << 20 // 2 MiB
)

// indexSkillDir reads and validates SKILL.md frontmatter, returning metadata and digest.
// It does NOT return the body; body is loaded separately for progressive disclosure.
// Assumes canonical root passed. All validations already done.
func indexSkillDir(
	ctx context.Context,
	rootDir string,
) (name, description string, props map[string]any, digest string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", nil, "", fmt.Errorf("indexSkillDir: %w", err)
	}

	root := strings.TrimSpace(rootDir)
	if root == "" {
		return "", "", nil, "", fmt.Errorf("%w: empty rootDir", spec.ErrInvalidArgument)
	}

	skillMDPath := filepath.Join(root, skillFileName)

	// Disallow SKILL.md being a symlink.
	if lst, lerr := os.Lstat(skillMDPath); lerr == nil {
		if lst.Mode()&os.ModeSymlink != 0 {
			return "", "", nil, "", errors.New("SKILL.md must not be a symlink")
		}
		if !lst.Mode().IsRegular() {
			return "", "", nil, "", errors.New("SKILL.md must be a regular file")
		}
	}

	b, sha, err := readAllLimitedAndDigest(skillMDPath)
	if err != nil {
		return "", "", nil, "", fmt.Errorf("indexSkillDir: %w", err)
	}

	fm, _, hasFM, err := splitFrontmatter(string(b))
	if err != nil {
		return "", "", nil, "", fmt.Errorf("indexSkillDir: %w", err)
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
		return "", "", nil, "", fmt.Errorf("indexSkillDir: %w", err)
	}
	if err := validateDescription(description); err != nil {
		return "", "", nil, "", fmt.Errorf("indexSkillDir: %w", err)
	}

	// FS convention: name must match directory name.
	if base := filepath.Base(root); base != "" && name != base {
		return "", "", nil, "", fmt.Errorf("frontmatter.name %q must match directory name %q", name, base)
	}

	return name, description, props, "sha256:" + sha, nil
}

func loadSkillBody(ctx context.Context, rootDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("loadSkillBody: %w", err)
	}

	// Assumes canonical root passed. All validations already done.
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return "", fmt.Errorf("%w: empty rootDir", spec.ErrInvalidArgument)
	}

	skillMDPath := filepath.Join(root, skillFileName)

	// Match index hardening: disallow symlink / non-regular file (check before reading).
	if lst, lerr := os.Lstat(skillMDPath); lerr == nil {
		if lst.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("SKILL.md must not be a symlink")
		}
		if !lst.Mode().IsRegular() {
			return "", errors.New("SKILL.md must be a regular file")
		}
	}
	b, _, err := readAllLimitedAndDigest(skillMDPath)
	if err != nil {
		return "", fmt.Errorf("loadSkillBody: %w", err)
	}

	fm, body, hasFM, err := splitFrontmatter(string(b))
	if err != nil {
		return "", fmt.Errorf("loadSkillBody: %w", err)
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
		return nil, "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	data, err = io.ReadAll(io.LimitReader(f, int64(maxSkillMDBytes)+1))
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
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
		return "", "", false, fmt.Errorf("read first line: %w", ferr)
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
			return "", "", false, fmt.Errorf("read frontmatter line: %w", lerr)
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
		return "", "", false, fmt.Errorf("read body: %w", err)
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
