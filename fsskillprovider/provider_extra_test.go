package fsskillprovider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
	"github.com/flexigpt/llmtools-go/exectool"
)

func TestProviderNew_DefaultExtensionsAndValidationBranches(t *testing.T) {
	t.Parallel()

	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p == nil {
		t.Fatalf("expected non-nil provider")
	}

	// Wrong type is rejected before any filesystem work.
	if _, err := p.Index(
		t.Context(),
		spec.SkillDef{Type: "wrong", Name: "skill", Location: "/tmp"},
	); err == nil ||
		!errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for wrong type, got %v", err)
	}

	tests := []struct {
		name string
		def  spec.SkillDef
		want error
	}{
		{name: "missing type", def: spec.SkillDef{Name: "skill", Location: "/tmp"}, want: spec.ErrInvalidArgument},
		{name: "missing name", def: spec.SkillDef{Type: Type, Location: "/tmp"}, want: spec.ErrInvalidArgument},
		{name: "missing location", def: spec.SkillDef{Type: Type, Name: "skill"}, want: spec.ErrInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := p.Index(t.Context(), tt.def)
			if err == nil || !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}

func TestProviderNew_PreservesExplicitRunScriptPolicyExtensions(t *testing.T) {
	t.Parallel()

	p, err := New(WithRunScriptPolicy(exectool.RunScriptPolicy{AllowedExtensions: []string{".rb"}}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(p.runScriptPolicy.AllowedExtensions) != 1 || p.runScriptPolicy.AllowedExtensions[0] != ".rb" {
		t.Fatalf(
			"expected caller-provided allowed extensions to be preserved, got=%v",
			p.runScriptPolicy.AllowedExtensions,
		)
	}
}

func TestProviderReadResource_DefaultEncodingAndSuccess(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "hello-skill")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}

	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := p.ReadResource(
		t.Context(),
		spec.ProviderSkillKey{Type: Type, Name: "hello-skill", Location: root},
		"note.txt",
		"",
	)
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("expected at least one tool output, got %#v", out)
	}
}

func TestProviderIndexLoadBodyAndExecutionValidationBranches(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "hello-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: hello-skill\ndescription: hello\n---\nbody\n"),
		0o600,
	); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	p, err := New(WithRunScripts(true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Name mismatch between def and frontmatter should be rejected.
	_, err = p.Index(t.Context(), spec.SkillDef{Type: Type, Name: "other", Location: skillDir})
	if err == nil || !strings.Contains(err.Error(), "does not match SKILL.md frontmatter.name") {
		t.Fatalf("expected name mismatch error, got %v", err)
	}

	rec, err := p.Index(t.Context(), spec.SkillDef{Type: Type, Name: "hello-skill", Location: skillDir})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if rec.Key.Type != Type || rec.Key.Name != "hello-skill" {
		t.Fatalf("unexpected index record: %+v", rec)
	}

	body, err := p.LoadBody(t.Context(), rec.Key)
	if err != nil {
		t.Fatalf("LoadBody: %v", err)
	}
	if !strings.Contains(body, "body") {
		t.Fatalf("expected body text, got %q", body)
	}

	// Wrong type is rejected before filesystem lookup.
	if _, err := p.LoadBody(
		t.Context(),
		spec.ProviderSkillKey{Type: "wrong", Name: "hello-skill", Location: skillDir},
	); err == nil ||
		!errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for wrong type, got %v", err)
	}

	// Missing root should fail canonicalization.
	if _, err := p.LoadBody(
		t.Context(),
		spec.ProviderSkillKey{Type: Type, Name: "hello-skill", Location: filepath.Join(tmp, "missing")},
	); err == nil ||
		!strings.Contains(err.Error(), "invalid skill location") {
		t.Fatalf("expected invalid location error, got %v", err)
	}

	// ReadResource validation branches.
	if _, err := p.ReadResource(
		t.Context(),
		rec.Key,
		"",
		spec.ReadResourceEncodingText,
	); err == nil ||
		!errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for missing resource, got %v", err)
	}
	if _, err := p.ReadResource(
		t.Context(),
		rec.Key,
		"README.md",
		spec.ReadResourceEncoding("yaml"),
	); err == nil ||
		!errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for unknown encoding, got %v", err)
	}
	if _, err := p.ReadResource(
		t.Context(),
		spec.ProviderSkillKey{Type: "wrong", Name: "hello-skill", Location: skillDir},
		"README.md",
		spec.ReadResourceEncodingText,
	); err == nil ||
		!errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for wrong type read, got %v", err)
	}

	// RunScript validation branches.
	if _, err := p.RunScript(
		t.Context(),
		spec.ProviderSkillKey{Type: "wrong", Name: "hello-skill", Location: skillDir},
		"script.sh",
		nil,
		nil,
		"",
	); err == nil ||
		!errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for wrong type runscript, got %v", err)
	}
	if _, err := p.RunScript(
		t.Context(),
		rec.Key,
		"",
		nil,
		nil,
		"",
	); err == nil ||
		!errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for empty script location, got %v", err)
	}
}
