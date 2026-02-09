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

func TestSplitFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		wantHas  bool
		wantErr  bool
		wantBody string
	}{
		{
			name:     "no frontmatter",
			in:       "hello\nworld\n",
			wantHas:  false,
			wantErr:  false,
			wantBody: "hello\nworld\n",
		},
		{
			name:    "unterminated frontmatter",
			in:      "---\nname: x\n",
			wantHas: false,
			wantErr: true,
		},
		{
			name:     "frontmatter with body",
			in:       "---\nname: x\ndescription: y\n---\n\n# Title\n",
			wantHas:  true,
			wantErr:  false,
			wantBody: "\n# Title\n",
		},
		{
			name:     "windows newlines",
			in:       "---\r\nname: x\r\ndescription: y\r\n---\r\nBody\r\n",
			wantHas:  true,
			wantErr:  false,
			wantBody: "Body\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fm, body, has, err := splitFrontmatter(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if has != tt.wantHas {
				t.Fatalf("has=%v want=%v fm=%q body=%q", has, tt.wantHas, fm, body)
			}
			if tt.wantHas && body != tt.wantBody {
				t.Fatalf("body mismatch: got=%q want=%q", body, tt.wantBody)
			}
		})
	}
}

func TestValidateNameAndDescription(t *testing.T) {
	t.Parallel()

	nameTests := []struct {
		in      string
		wantErr bool
	}{
		{"", true},
		{"a", false},
		{"A", true},
		{"-a", true},
		{"a-", true},
		{"a--b", true},
		{"a_b", true},
		{strings.Repeat("a", 64), false},
		{strings.Repeat("a", 65), true},
	}

	for _, tt := range nameTests {
		t.Run("name_"+tt.in, func(t *testing.T) {
			t.Parallel()
			err := validateName(tt.in)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}

	descTests := []struct {
		in      string
		wantErr bool
	}{
		{"", true},
		{"x", false},
		{strings.Repeat("d", 1024), false},
		{strings.Repeat("d", 1025), true},
	}

	for _, tt := range descTests {
		t.Run("desc_len_"+string(rune(len(tt.in))), func(t *testing.T) {
			t.Parallel()
			err := validateDescription(tt.in)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

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

	name, desc, props, digest, err := indexSkillDir(t.Context(), root)
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
			wantSub: "must contain YAML frontmatter",
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
			wantSub: "invalid frontmatter YAML",
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
			wantSub: "must match directory name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(tmp, "case-"+strings.ReplaceAll(tt.name, " ", "-"))
			if err := tt.setup(root); err != nil {
				t.Fatalf("setup: %v", err)
			}
			_, _, _, _, err := indexSkillDir(t.Context(), root)
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

	if runtime.GOOS == "windows" {
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

	_, _, _, _, err := indexSkillDir(t.Context(), root)
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestIndexSkillDir_EmptyRootInvalidArgument(t *testing.T) {
	t.Parallel()

	_, _, _, _, err := indexSkillDir(t.Context(), " ")
	if err == nil || !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}
