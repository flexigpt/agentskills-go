package skill

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

// IndexSkillDir reads and validates required frontmatter, but does NOT cache the SKILL.md body.
// (Body is loaded on skills.load for progressive disclosure.)
func IndexSkillDir(ctx context.Context, dir string) (spec.SkillRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	d := strings.TrimSpace(dir)
	if d == "" {
		return spec.SkillRecord{}, errors.New("empty dir")
	}

	root, err := filepath.Abs(filepath.Clean(d))
	if err != nil {
		return spec.SkillRecord{}, err
	}
	if resolved, rerr := filepath.EvalSymlinks(root); rerr == nil && resolved != "" {
		root = resolved
	}

	st, err := os.Stat(root)
	if err != nil {
		return spec.SkillRecord{}, err
	}
	if !st.IsDir() {
		return spec.SkillRecord{}, fmt.Errorf("not a directory: %s", root)
	}

	loc := filepath.Join(root, skillFileName)

	// Disallow SKILL.md being a symlink.
	if lst, lerr := os.Lstat(loc); lerr == nil {
		if lst.Mode()&os.ModeSymlink != 0 {
			return spec.SkillRecord{}, errors.New("SKILL.md must not be a symlink")
		}
	}

	b, digest, err := readAllLimitedAndDigest(loc)
	if err != nil {
		return spec.SkillRecord{}, err
	}

	fm, _, hasFM, err := splitFrontmatter(string(b))
	if err != nil {
		return spec.SkillRecord{}, err
	}
	if !hasFM {
		return spec.SkillRecord{}, errors.New("SKILL.md must contain YAML frontmatter")
	}

	props := map[string]any{}
	if err := yaml.Unmarshal([]byte(fm), &props); err != nil {
		return spec.SkillRecord{}, fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	name := strings.TrimSpace(asString(props["name"]))
	desc := strings.TrimSpace(asString(props["description"]))

	if err := validateName(name, filepath.Base(root)); err != nil {
		return spec.SkillRecord{}, err
	}
	if err := validateDescription(desc); err != nil {
		return spec.SkillRecord{}, err
	}

	return spec.SkillRecord{
		Name:        name,
		Description: desc,
		Location:    loc,
		RootDir:     root,
		Properties:  props,
		Digest:      "sha256:" + digest,
	}, nil
}

func LoadSkillBody(ctx context.Context, skillMDPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
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

	// Validate frontmatter parses (even if already validated at index time).
	props := map[string]any{}
	if err := yaml.Unmarshal([]byte(fm), &props); err != nil {
		return "", fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	// Preserve body content as much as possible; remove only the leading newline after delimiter.
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

	// Read first line.
	first, ferr := br.ReadString('\n')
	if ferr != nil && !errors.Is(ferr, io.EOF) {
		return "", "", false, ferr
	}
	first = strings.TrimRight(first, "\r\n")
	if strings.TrimSpace(first) != "---" {
		// No frontmatter.
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

func validateName(name, dirBase string) error {
	if name == "" {
		return errors.New("frontmatter.name is required")
	}
	if len(name) > 64 {
		return errors.New("frontmatter.name too long (max 64)")
	}
	if name != dirBase {
		return fmt.Errorf("frontmatter.name %q must match directory name %q", name, dirBase)
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
