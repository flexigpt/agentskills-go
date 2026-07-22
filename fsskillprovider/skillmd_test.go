package fsskillprovider

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestIndexSkillDirAndLoadSkillBody_HappyPath(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "hello-skill")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	md := []byte("---\nname: hello-skill\ndescription: Says hello.\nlicense: MIT\n---\n\n# Hello\nBody\n")
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), md, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	skillIndex, err := indexSkillDir(t.Context(), root)
	name := skillIndex.Name
	desc := skillIndex.Description
	props := skillIndex.Props
	digest := skillIndex.Digest
	if err != nil {
		t.Fatalf("indexSkillDir: %v", err)
	}
	if name != "hello-skill" || desc != "Says hello." {
		t.Fatalf("unexpected metadata: name=%q desc=%q", name, desc)
	}
	if props["license"] != "MIT" {
		t.Fatalf("expected license=MIT, got props=%v", props)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("expected sha256 digest, got %q", digest)
	}

	body, err := loadSkillBody(t.Context(), root)
	if err != nil {
		t.Fatalf("loadSkillBody: %v", err)
	}
	if !strings.Contains(body, "# Hello") || strings.Contains(body, "name: hello-skill") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestIndexSkillDir_Errors(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	tests := []struct {
		name    string
		setup   func(root string) error
		wantSub string
	}{
		{
			name:    "missing SKILL.md",
			setup:   func(root string) error { return os.MkdirAll(root, 0o755) },
			wantSub: "open",
		},
		{
			name: "no frontmatter",
			setup: func(root string) error {
				if err := os.MkdirAll(root, 0o755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("no fm"), 0o600)
			},
			wantSub: "SKILL.md requires YAML frontmatter",
		},
		{
			name: "invalid yaml",
			setup: func(root string) error {
				if err := os.MkdirAll(root, 0o755); err != nil {
					return err
				}
				return os.WriteFile(
					filepath.Join(root, "SKILL.md"),
					[]byte("---\nname: [\ndescription: x\n---\n"),
					0o600,
				)
			},
			wantSub: "not found",
		},
		{
			name: "name mismatch dir",
			setup: func(root string) error {
				if err := os.MkdirAll(root, 0o755); err != nil {
					return err
				}
				return os.WriteFile(
					filepath.Join(root, "SKILL.md"),
					[]byte("---\nname: other\ndescription: x\n---\n"),
					0o600,
				)
			},
			wantSub: "must match expected name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(tmp, "case-"+strings.ReplaceAll(tt.name, " ", "-"))
			if err := tt.setup(root); err != nil {
				t.Fatalf("setup: %v", err)
			}
			_, err := indexSkillDir(t.Context(), root)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("expected err containing %q, got %v", tt.wantSub, err)
			}
		})
	}
}

func TestReadAllLimitedAndDigest_SizeCap(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	p := filepath.Join(tmp, "SKILL.md")
	blob := bytes.Repeat([]byte("a"), maxSkillMDBytes+1)
	if err := os.WriteFile(p, blob, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err := readAllLimitedAndDigest(p)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too large error, got %v", err)
	}
}

func TestIndexSkillDir_DisallowSymlink(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == goosWindows {
		// Symlink creation often requires elevated privileges; skip to avoid flakiness.
		t.Skip("skip symlink test on windows")
	}

	tmp := t.TempDir()
	root := filepath.Join(tmp, "hello-skill")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	realMD := filepath.Join(root, "REAL.md")
	if err := os.WriteFile(realMD, []byte("---\nname: hello-skill\ndescription: x\n---\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(root, "SKILL.md")
	if err := os.Symlink(realMD, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	_, err := indexSkillDir(t.Context(), root)
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestIndexSkillDir_EmptyRootInvalidArgument(t *testing.T) {
	t.Parallel()

	_, err := indexSkillDir(t.Context(), " ")
	if err == nil || !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}
