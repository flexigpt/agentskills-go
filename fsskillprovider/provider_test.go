package fsskillprovider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestCanonicalRoot(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	f := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tests := []struct {
		name    string
		in      string
		wantErr bool
		wantIs  error
	}{
		{"empty", " ", true, spec.ErrInvalidArgument},
		{"nul", "a\x00b", true, spec.ErrInvalidArgument},
		{"missing", filepath.Join(tmp, "missing"), true, spec.ErrInvalidArgument},
		{"not dir", f, true, spec.ErrInvalidArgument},
		{"dir ok", tmp, false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := canonicalRoot(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
					t.Fatalf("expected errors.Is(err,%v)=true, got %v", tt.wantIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if out == "" {
				t.Fatalf("expected non-empty canonical path")
			}
		})
	}
}

func TestProvider_IndexAndLoadBody(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "hello-skill")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "SKILL.md"),
		[]byte("---\nname: hello-skill\ndescription: x\n---\n\nBody\n"),
		0o600,
	); err != nil {
		t.Fatalf("write: %v", err)
	}

	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.Index(
		t.Context(),
		spec.SkillKey{Type: "wrong", SkillHandle: spec.SkillHandle{Name: "hello-skill", Location: root}},
	)
	if err == nil || !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for wrong type, got %v", err)
	}

	rec, err := p.Index(
		t.Context(),
		spec.SkillKey{Type: Type, SkillHandle: spec.SkillHandle{Name: "hello-skill", Location: root}},
	)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if rec.Key.Location == "" {
		t.Fatalf("expected normalized path")
	}

	body, err := p.LoadBody(t.Context(), rec.Key)
	if err != nil {
		t.Fatalf("LoadBody: %v", err)
	}
	if strings.Contains(body, "name: hello-skill") {
		t.Fatalf("expected frontmatter removed, got %q", body)
	}
}

func TestProvider_RunScriptDisabled(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "hello-skill")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "SKILL.md"),
		[]byte("---\nname: hello-skill\ndescription: x\n---\n"),
		0o600,
	); err != nil {
		t.Fatalf("write: %v", err)
	}

	p, err := New() // scripts disabled by default
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.RunScript(
		t.Context(),
		spec.SkillKey{Type: Type, SkillHandle: spec.SkillHandle{Name: "hello-skill", Location: root}},
		"scripts/x.sh",
		nil,
		nil,
		"",
	)
	if !errors.Is(err, spec.ErrRunScriptUnsupported) {
		t.Fatalf("expected ErrRunScriptUnsupported, got %v", err)
	}
}

func TestWithAllowedScriptExtensions_Normalizes(t *testing.T) {
	t.Parallel()

	p, err := New(WithAllowedScriptExtensions([]string{"PY", " sh ", "txt", "", ".Go"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Internal field access (same package): verify normalization occurred.
	got := strings.Join(p.runScriptPolicy.AllowedExtensions, ",")
	for _, want := range []string{".py", ".sh", ".txt", ".go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in AllowedExtensions, got %q", want, got)
		}
	}
}
