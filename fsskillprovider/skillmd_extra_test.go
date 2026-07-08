package fsskillprovider

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestParseSkillInsertAndArgumentsHelpers(t *testing.T) {
	t.Parallel()

	insert, warnings := parseSkillInsert(" user-message ")
	if insert != spec.SkillInsertUserMessage || len(warnings) != 0 {
		t.Fatalf("parseSkillInsert valid mismatch: insert=%q warnings=%v", insert, warnings)
	}

	insert, warnings = parseSkillInsert(123)
	if insert != spec.SkillInsertInstructions || len(warnings) != 1 ||
		warnings[0] != "frontmatter.insert must be a string; defaulted to instructions" {
		t.Fatalf("parseSkillInsert invalid mismatch: insert=%q warnings=%v", insert, warnings)
	}

	args, warnings := parseSkillArguments("name")
	if args != nil || len(warnings) != 1 || warnings[0] != "frontmatter.arguments must be a list, not a string: name" {
		t.Fatalf("parseSkillArguments string mismatch: args=%v warnings=%v", args, warnings)
	}

	rawArgs := []any{
		"text",
		map[string]any{"name": "tone", "description": 42, "default": true},
		map[string]any{"name": "tone", "description": "duplicate"},
		map[any]any{"name": "bad name"},
		123,
	}
	args, warnings = parseSkillArgumentsValue(rawArgs)
	wantArgs := []spec.SkillArgument{
		{Name: "text"},
		{Name: "tone", Description: "42", Default: "true"},
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("parseSkillArgumentsValue args mismatch: got=%#v want=%#v", args, wantArgs)
	}
	wantWarnings := []string{
		"duplicate argument ignored: tone",
		"frontmatter.arguments[3].name is invalid",
		"frontmatter.arguments[4] must be an object or string",
	}
	if !reflect.DeepEqual(warnings, wantWarnings) {
		t.Fatalf("parseSkillArgumentsValue warnings mismatch: got=%#v want=%#v", warnings, wantWarnings)
	}

	if m, ok := asStringMap(map[any]any{"name": "skill", 1: "bad"}); ok || m != nil {
		t.Fatalf("expected asStringMap to reject non-string keys, got=%v ok=%v", m, ok)
	}
	m, ok := asStringMap(map[any]any{"name": "skill", "description": 7})
	if !ok || m["name"] != "skill" || m["description"] != 7 {
		t.Fatalf("unexpected asStringMap result: m=%v ok=%v", m, ok)
	}

	if got := valueAsString(nil); got != "" {
		t.Fatalf("valueAsString(nil) = %q", got)
	}
	if got := valueAsString(123); got != "123" {
		t.Fatalf("valueAsString(123) = %q", got)
	}

	if got := uniqueStrings([]string{" b ", "a", "", "a", "b", "c"}); !reflect.DeepEqual(got, []string{"b", "a", "c"}) {
		t.Fatalf("uniqueStrings mismatch: got=%#v", got)
	}

	if got := firstMarkdownH1("\n# Title\n## Sub\n", "fallback"); got != "Title" {
		t.Fatalf("firstMarkdownH1 mismatch: got=%q", got)
	}
	if got := firstMarkdownH1("no heading here", "fallback"); got != "fallback" {
		t.Fatalf("firstMarkdownH1 fallback mismatch: got=%q", got)
	}
}

func TestIndexSkillDir_WithInsertArgumentsAndDisplayName(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "hello-skill")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := []byte(`---
name: hello-skill
description: Says hello.
insert: user-message
arguments:
  - text
  - name: tone
    description: Tone to use.
    default: warm
  - name: text
    description: duplicate should be ignored
  - name: bad name
license: MIT
---
# Hello Title

Body
`)
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), md, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	idx, err := indexSkillDir(t.Context(), root)
	if err != nil {
		t.Fatalf("indexSkillDir: %v", err)
	}
	if idx.Name != "hello-skill" || idx.Description != "Says hello." {
		t.Fatalf("unexpected metadata: %+v", idx)
	}
	if idx.DisplayName != "Hello Title" {
		t.Fatalf("unexpected display name: %q", idx.DisplayName)
	}
	if idx.Insert != spec.SkillInsertUserMessage {
		t.Fatalf("expected user-message insert, got %q", idx.Insert)
	}
	if len(idx.Arguments) != 2 || idx.Arguments[0].Name != "text" || idx.Arguments[1].Name != "tone" {
		t.Fatalf("unexpected parsed args: %+v", idx.Arguments)
	}
	if idx.Props["license"] != "MIT" {
		t.Fatalf("expected license in props, got %+v", idx.Props)
	}
	if len(idx.Warnings) == 0 {
		t.Fatalf("expected parse warnings for duplicate/invalid arguments")
	}
	if got, ok := idx.Props["insert"].(string); !ok || got != "user-message" {
		t.Fatalf("expected raw insert in props, got %+v", idx.Props["insert"])
	}
	if !strings.HasPrefix(idx.Digest, "sha256:") {
		t.Fatalf("expected sha256 digest, got %q", idx.Digest)
	}

	body, err := loadSkillBody(t.Context(), root)
	if err != nil {
		t.Fatalf("loadSkillBody: %v", err)
	}
	if strings.Contains(body, "name: hello-skill") || !strings.Contains(body, "Body") {
		t.Fatalf("unexpected body: %q", body)
	}
}
