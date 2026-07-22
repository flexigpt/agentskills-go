package fsskillprovider

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
	"github.com/flexigpt/llmtools-go/exectool"
)

const helloSkillName = "hello-skill"

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
	root := filepath.Join(tmp, helloSkillName)
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
		spec.SkillDef{Type: "wrong", Name: helloSkillName, Location: root},
	)
	if err == nil || !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for wrong type, got %v", err)
	}

	rec, err := p.Index(
		t.Context(),
		spec.SkillDef{Type: Type, Name: helloSkillName, Location: root},
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
	root := filepath.Join(tmp, helloSkillName)
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
		spec.ProviderSkillKey{Type: Type, Name: helloSkillName, Location: root},
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
	for _, want := range []string{extPy, extSh, ".txt", ".go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in AllowedExtensions, got %q", want, got)
		}
	}
}

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

func TestProvider_ContextCancellationAndResourceBoundary(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "boundary-skill")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "SKILL.md"),
		[]byte("---\nname: boundary-skill\ndescription: x\n---\nbody\n"),
		0o600,
	); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatalf("write inside resource: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside resource: %v", err)
	}

	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	key := spec.ProviderSkillKey{Type: Type, Name: "boundary-skill", Location: root}
	if _, err := p.ReadResource(t.Context(), key, "inside.txt", spec.ReadResourceEncodingText); err != nil {
		t.Fatalf("ReadResource(inside): %v", err)
	}
	if _, err := p.ReadResource(t.Context(), key, "../outside.txt", spec.ReadResourceEncodingText); err == nil {
		t.Fatal("expected ReadResource to reject a path outside the skill root")
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.Index(
		canceled,
		spec.SkillDef{Type: Type, Name: "boundary-skill", Location: root},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Index(canceled): expected context.Canceled, got %v", err)
	}
	if _, err := p.LoadBody(canceled, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadBody(canceled): expected context.Canceled, got %v", err)
	}
	if _, err := p.ReadResource(
		canceled,
		key,
		"inside.txt",
		spec.ReadResourceEncodingText,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("ReadResource(canceled): expected context.Canceled, got %v", err)
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

func TestIndexSkillDir_TolerantMetadataAndResourceDiscovery(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "tolerant-skill")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "SKILL.md"),
		[]byte(
			"---\nname: tolerant-skill\ndescription: \" Tolerates optional metadata. \"\ninsert: nowhere\narguments: topic\ntags: writing\ncustom: retained\n---\n\n# Tolerant Skill\n\nUse $topic.\n",
		),
		0o600,
	); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("note"), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("guide"), 0o600); err != nil {
		t.Fatalf("write guide: %v", err)
	}

	indexed, err := indexSkillDir(t.Context(), root)
	if err != nil {
		t.Fatalf("indexSkillDir: %v", err)
	}
	if indexed.DisplayName != "Tolerant Skill" {
		t.Fatalf("DisplayName = %q", indexed.DisplayName)
	}
	if indexed.Description != "Tolerates optional metadata." {
		t.Fatalf("Description = %q", indexed.Description)
	}
	if indexed.Insert != spec.SkillInsertInstructions {
		t.Fatalf("Insert = %q", indexed.Insert)
	}
	if len(indexed.Arguments) != 1 || indexed.Arguments[0].Name != "topic" {
		t.Fatalf("Arguments = %+v", indexed.Arguments)
	}
	if len(indexed.Tags) != 1 || indexed.Tags[0] != "writing" {
		t.Fatalf("Tags = %v", indexed.Tags)
	}
	if !indexed.Resources.HasResources || indexed.Resources.TotalCount != 2 {
		t.Fatalf("Resources = %+v", indexed.Resources)
	}
	if strings.Join(indexed.Resources.Locations, ",") != "docs/guide.md,note.txt" {
		t.Fatalf("Resource locations = %v", indexed.Resources.Locations)
	}
	if indexed.Props["custom"] != "retained" {
		t.Fatalf("custom property = %#v", indexed.Props["custom"])
	}
	allWarnings := strings.Join(indexed.Warnings, "\n")
	for _, expected := range []string{
		"whitespace removed",
		"unsupported frontmatter.insert",
		"arguments string",
		"tags string",
	} {
		if !strings.Contains(allWarnings, expected) {
			t.Fatalf("warnings %q do not contain %q", allWarnings, expected)
		}
	}
}

func TestLoadSkillBody_RevalidatesDocumentAfterIndex(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "stable-skill")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(
		path,
		[]byte("---\nname: stable-skill\ndescription: x\n---\nbody\n"),
		0o600,
	); err != nil {
		t.Fatalf("write valid SKILL.md: %v", err)
	}
	if _, err := indexSkillDir(t.Context(), root); err != nil {
		t.Fatalf("indexSkillDir: %v", err)
	}
	if err := os.WriteFile(
		path,
		[]byte("---\nname: replaced-skill\ndescription: x\n---\nbody\n"),
		0o600,
	); err != nil {
		t.Fatalf("replace SKILL.md: %v", err)
	}

	_, err := loadSkillBody(t.Context(), root)
	if err == nil || !strings.Contains(err.Error(), "must match expected name") {
		t.Fatalf("expected revalidation error, got %v", err)
	}
}
