package fsskillprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/flexigpt/agentskills-go"
	"github.com/flexigpt/agentskills-go/spec"
)

const (
	skillFileName   = agentskills.SkillDocumentFileName
	maxSkillMDBytes = agentskills.MaxSkillDocumentBytes
)

type skillDirIndex struct {
	Name        string
	Description string
	DisplayName string

	Insert    spec.SkillInsert
	Arguments []spec.SkillArgument

	Tags []string

	Resources spec.SkillResourceInfo

	Props map[string]any

	Warnings []string

	Digest string
}

// indexSkillDir reads and validates SKILL.md frontmatter, returning metadata and digest.
// It does NOT return the body; body is loaded separately for progressive disclosure.
// Assumes canonical root passed. All validations already done.
func indexSkillDir(
	ctx context.Context,
	rootDir string,
) (skillDirIndex, error) {
	if err := ctx.Err(); err != nil {
		return skillDirIndex{}, fmt.Errorf("indexSkillDir: %w", err)
	}

	root := strings.TrimSpace(rootDir)
	if root == "" {
		return skillDirIndex{}, fmt.Errorf("%w: empty rootDir", spec.ErrInvalidArgument)
	}

	skillMDPath := filepath.Join(root, skillFileName)

	// Disallow SKILL.md being a symlink.
	if lst, lerr := os.Lstat(skillMDPath); lerr == nil {
		if lst.Mode()&os.ModeSymlink != 0 {
			return skillDirIndex{}, errors.New("SKILL.md must not be a symlink")
		}
		if !lst.Mode().IsRegular() {
			return skillDirIndex{}, errors.New("SKILL.md must be a regular file")
		}
	}

	b, sha, err := readAllLimitedAndDigest(skillMDPath)
	if err != nil {
		return skillDirIndex{}, fmt.Errorf("indexSkillDir: %w", err)
	}

	document, parseWarnings, err := agentskills.ParseSkillDocument(
		b,
		spec.ParseSkillDocumentOptions{
			ExpectedName: filepath.Base(root),
		},
	)
	if err != nil {
		return skillDirIndex{}, fmt.Errorf("indexSkillDir: %w", err)
	}

	resources, resourceWarnings, err := indexSkillResources(ctx, root)
	if err != nil {
		return skillDirIndex{}, fmt.Errorf("indexSkillDir: %w", err)
	}
	warnings := append(
		append([]string(nil), parseWarnings...),
		resourceWarnings...,
	)

	return skillDirIndex{
		Name:        document.Name,
		Description: document.Description,
		DisplayName: document.DisplayName,
		Insert:      document.Insert,
		Arguments:   append([]spec.SkillArgument(nil), document.Arguments...),
		Tags:        append([]string(nil), document.Tags...),
		Resources:   resources,
		Props:       document.RawFrontmatter,
		Warnings:    uniqueStrings(warnings),
		Digest:      "sha256:" + sha,
	}, nil
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

	document, _, err := agentskills.ParseSkillDocument(
		b,
		spec.ParseSkillDocumentOptions{
			ExpectedName: filepath.Base(root),
		},
	)
	if err != nil {
		return "", fmt.Errorf("loadSkillBody: %w", err)
	}
	return document.MarkdownBody, nil
}

func indexSkillResources(
	ctx context.Context,
	rootDir string,
) (spec.SkillResourceInfo, []string, error) {
	if err := ctx.Err(); err != nil {
		return spec.SkillResourceInfo{}, nil, err
	}

	root := strings.TrimSpace(rootDir)
	if root == "" {
		return spec.SkillResourceInfo{}, nil, fmt.Errorf("%w: empty rootDir", spec.ErrInvalidArgument)
	}

	var info spec.SkillResourceInfo
	var warnings []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		rel := resourceRelLocation(root, path)
		if walkErr != nil {
			warnings = append(warnings, fmt.Sprintf("resource scan skipped %q: %v", rel, walkErr))
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d == nil {
			return nil
		}

		if rel == "." {
			return nil
		}
		if rel == skillFileName {
			return nil
		}

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		st, err := d.Info()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("resource scan skipped %q: %v", rel, err))
			return nil
		}
		if !st.Mode().IsRegular() {
			return nil
		}

		info.TotalCount++
		if len(info.Locations) < spec.MaxSkillResourceLocations {
			info.Locations = append(info.Locations, rel)
		}
		return nil
	})
	if err != nil {
		return spec.SkillResourceInfo{}, warnings, err
	}

	info.HasResources = info.TotalCount > 0
	info.MoreLocations = info.TotalCount > len(info.Locations)
	return info, uniqueStrings(warnings), nil
}

func resourceRelLocation(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.TrimSpace(rel) == "" {
		return "."
	}
	return filepath.ToSlash(rel)
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

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
