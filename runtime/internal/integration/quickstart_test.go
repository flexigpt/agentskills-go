package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/document"
	"github.com/flexigpt/agentskills-go/provider"
	"github.com/flexigpt/agentskills-go/provider/fs"
	"github.com/flexigpt/agentskills-go/runtime"
	"github.com/flexigpt/agentskills-go/runtime/spec"
)

func TestReadmeQuickstart(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()

	writingDir := writeReadmeSkill(t, root, "writing-guide", `---
name: writing-guide
description: Helps write concise technical guides.
arguments:
  - name: topic
    description: Topic to cover.
    default: Go
tags:
  - writing
---

# Writing Guide

Write a concise guide about $topic.
`)
	if err := os.MkdirAll(filepath.Join(writingDir, "references"), 0o755); err != nil {
		t.Fatalf("create writing resource directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(writingDir, "references", "style.md"),
		[]byte("Prefer direct language and concrete examples.\n"),
		0o600,
	); err != nil {
		t.Fatalf("write writing resource: %v", err)
	}

	reviewDir := writeReadmeSkill(t, root, "review-guide", `---
name: review-guide
description: Reviews a draft for clarity.
---

# Review Guide

Review the draft for clarity and concrete next steps.
`)

	templateDir := writeReadmeSkill(t, root, "message-template", `---
name: message-template
description: Creates a greeting message.
insert: user-message
arguments:
  - name: recipient
    description: Recipient name.
    default: friend
tags:
  - template
---

# Greeting

Hello, $recipient!
`)

	filesystemProvider, err := fs.New()
	if err != nil {
		t.Fatalf("fs.New() error = %v", err)
	}

	rt, err := runtime.New(runtime.WithProvider(filesystemProvider))
	if err != nil {
		t.Fatalf("runtime.New() error = %v", err)
	}

	writingDef := provider.SkillDef{
		Type:     fs.Type,
		Name:     "writing-guide",
		Location: writingDir,
	}
	reviewDef := provider.SkillDef{
		Type:     fs.Type,
		Name:     "review-guide",
		Location: reviewDir,
	}
	templateDef := provider.SkillDef{
		Type:     fs.Type,
		Name:     "message-template",
		Location: templateDir,
	}

	writing, err := rt.AddSkill(ctx, writingDef)
	if err != nil {
		t.Fatalf("AddSkill(writing guide) error = %v", err)
	}
	if !writing.Resources.HasResources || writing.Resources.TotalCount != 1 {
		t.Fatalf("writing resource metadata = %+v", writing.Resources)
	}

	if _, err := rt.AddSkill(ctx, reviewDef); err != nil {
		t.Fatalf("AddSkill(review guide) error = %v", err)
	}
	template, err := rt.AddSkill(ctx, templateDef)
	if err != nil {
		t.Fatalf("AddSkill(message template) error = %v", err)
	}

	instructions, err := rt.ListSkills(ctx, &runtime.SkillListFilter{
		Inserts: []document.SkillInsert{document.SkillInsertInstructions},
	})
	if err != nil {
		t.Fatalf("ListSkills(instructions) error = %v", err)
	}
	if len(instructions) != 2 {
		t.Fatalf("ListSkills(instructions) returned %d records, want 2", len(instructions))
	}

	availablePrompt, err := rt.SkillsPrompt(ctx, &runtime.SkillFilter{
		Activity: spec.SkillActivityInactive,
	})
	if err != nil {
		t.Fatalf("SkillsPrompt(available) error = %v", err)
	}
	if !strings.Contains(availablePrompt, "name: writing-guide") ||
		!strings.Contains(availablePrompt, "name: review-guide") {
		t.Fatalf("available prompt does not contain instruction skills:\n%s", availablePrompt)
	}
	if strings.Contains(availablePrompt, "message-template") {
		t.Fatalf("available prompt unexpectedly contains user-message template:\n%s", availablePrompt)
	}

	sessionID, active, err := rt.NewSession(
		ctx,
		runtime.WithSessionAllowedSkills(
			[]provider.SkillDef{writing.Def, reviewDef},
		),
		runtime.WithSessionActiveSkills(
			[]provider.SkillDef{writing.Def},
		),
	)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if len(active) != 1 || active[0] != writing.Def {
		t.Fatalf("initial active definitions = %+v, want [%+v]", active, writing.Def)
	}

	activePrompt, err := rt.SkillsPrompt(ctx, &runtime.SkillFilter{
		SessionID: sessionID,
		Activity:  spec.SkillActivityActive,
	})
	if err != nil {
		t.Fatalf("SkillsPrompt(active) error = %v", err)
	}
	if !strings.Contains(activePrompt, "Write a concise guide about Go.") {
		t.Fatalf("active prompt did not render default arguments:\n%s", activePrompt)
	}

	rendered, err := rt.RenderSkill(ctx, runtime.RenderSkillParams{
		Def: template.Def,
		Arguments: map[string]string{
			"recipient": "Ada",
		},
	})
	if err != nil {
		t.Fatalf("RenderSkill(template) error = %v", err)
	}
	if rendered.Insert != document.SkillInsertUserMessage {
		t.Fatalf("rendered insert = %q, want user-message", rendered.Insert)
	}
	if !strings.Contains(rendered.Text, "Hello, Ada!") {
		t.Fatalf("rendered template text = %q", rendered.Text)
	}

	combinedPrompt, err := rt.SkillsPrompt(ctx, &runtime.SkillFilter{
		SessionID: sessionID,
		Activity:  spec.SkillActivityAny,
	})
	if err != nil {
		t.Fatalf("SkillsPrompt(combined) error = %v", err)
	}
	for _, marker := range []string{
		"<<<SKILLS_PROMPT>>>",
		"<<<AVAILABLE_SKILLS>>>",
		"<<<ACTIVE_SKILLS>>>",
		"<<<END_SKILLS_PROMPT>>>",
	} {
		if !strings.Contains(combinedPrompt, marker) {
			t.Fatalf("combined prompt missing %q:\n%s", marker, combinedPrompt)
		}
	}
	if !strings.Contains(combinedPrompt, "name: review-guide") ||
		!strings.Contains(combinedPrompt, "name: writing-guide") {
		t.Fatalf("combined prompt is missing expected skills:\n%s", combinedPrompt)
	}
	if strings.Contains(combinedPrompt, "message-template") {
		t.Fatalf("combined prompt unexpectedly contains template:\n%s", combinedPrompt)
	}

	registry, err := rt.NewSessionRegistry(ctx, sessionID)
	if err != nil {
		t.Fatalf("NewSessionRegistry() error = %v", err)
	}
	if registry == nil {
		t.Fatal("NewSessionRegistry() returned nil registry")
	}

	if err := rt.CloseSession(ctx, sessionID); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
}

func TestReadmeRunScriptConfiguration(t *testing.T) {
	disabled, err := fs.New()
	if err != nil {
		t.Fatalf("fs.New() error = %v", err)
	}
	if disabled.SupportsRunScript() {
		t.Fatal("filesystem provider should disable script execution by default")
	}

	enabled, err := fs.New(fs.WithRunScripts(true))
	if err != nil {
		t.Fatalf("fs.New(WithRunScripts) error = %v", err)
	}
	if !enabled.SupportsRunScript() {
		t.Fatal("filesystem provider should report script support when enabled")
	}

	rt, err := runtime.New(runtime.WithProvider(enabled))
	if err != nil {
		t.Fatalf("runtime.New() error = %v", err)
	}
	if !rt.SupportsRunScript() {
		t.Fatal("runtime should report script support from the configured provider")
	}
}

func writeReadmeSkill(
	t *testing.T,
	parent string,
	name string,
	content string,
) string {
	t.Helper()

	directory := filepath.Join(parent, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create skill directory %q: %v", name, err)
	}
	if err := os.WriteFile(
		filepath.Join(directory, document.SkillDocumentFileName),
		[]byte(content),
		0o600,
	); err != nil {
		t.Fatalf("write SKILL.md for %q: %v", name, err)
	}
	return directory
}
