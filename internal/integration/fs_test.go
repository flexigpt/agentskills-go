package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexigpt/agentskills-go"
	"github.com/flexigpt/agentskills-go/fsskillprovider"
	"github.com/flexigpt/agentskills-go/spec"
)

const skillMD = `---
name: hello-skill
description: Says hello.
---
# Hello Skill

This is the body (frontmatter removed on load).

- Use it after activation.
`

func TestRuntime_FSProvider_EndToEnd(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	tmp := t.TempDir()

	// Create a minimal FS skill.
	skillDir := filepath.Join(tmp, "hello-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Build runtime.
	fsp, err := fsskillprovider.New()
	if err != nil {
		t.Fatalf("new fs provider: %v", err)
	}

	rt, err := agentskills.New(
		agentskills.WithProvider(fsp),
	)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	// Add skill.
	rec, err := rt.AddSkill(
		ctx,
		spec.SkillDef{
			Type:     fsskillprovider.Type,
			Name:     "hello-skill",
			Location: skillDir,
		},
	)
	if err != nil {
		t.Fatalf("add skill: %v", err)
	}
	if rec.Def.Location == "" {
		t.Fatalf("expected normalized path in record")
	}

	// Available skills prompt (metadata only).
	availPrompt, err := rt.SkillsPrompt(t.Context(), nil)
	if err != nil {
		t.Fatalf("available prompt %v", err)
	}
	if !strings.Contains(availPrompt, "<<<AVAILABLE_SKILLS>>>") {
		t.Fatalf("expected <<<AVAILABLE_SKILLS>>> prompt, got: %s", availPrompt)
	}
	if !strings.Contains(availPrompt, "name: hello-skill") {
		t.Fatalf("expected skill name in prompt, got: %s", availPrompt)
	}

	// Create session and activate skill (progressive disclosure).
	sid, handles, err := rt.NewSession(ctx, agentskills.WithSessionActiveSkills([]spec.SkillDef{rec.Def}))
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	if len(handles) != 1 {
		t.Fatalf("expected 1 active handle, got %d", len(handles))
	}

	activePrompt, err := rt.SkillsPrompt(ctx, &agentskills.SkillFilter{SessionID: sid})
	if err != nil {
		t.Fatalf("active prompt prompt: %v", err)
	}
	if !strings.Contains(activePrompt, "<<<ACTIVE_SKILLS>>>") {
		t.Fatalf("expected <<<ACTIVE_SKILLS>>> prompt, got: %s", activePrompt)
	}
	if !strings.Contains(activePrompt, "Hello Skill") {
		t.Fatalf("expected skill body in active prompt, got: %s", activePrompt)
	}

	// Registry creation (tool wiring).
	reg, err := rt.NewSessionRegistry(ctx, sid)
	if err != nil {
		t.Fatalf("new session registry: %v", err)
	}
	if reg == nil {
		t.Fatalf("expected non-nil registry")
	}
}

func TestRuntime_FSProvider_TolerantDocumentAndTemplateWorkflow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	tmp := t.TempDir()
	instructionDir := filepath.Join(tmp, "writing-skill")
	templateDir := filepath.Join(tmp, "message-template")
	if err := os.MkdirAll(filepath.Join(instructionDir, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir instruction skill: %v", err)
	}
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatalf("mkdir template skill: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(instructionDir, "SKILL.md"),
		[]byte(
			"---\nname: writing-skill\ndescription: \" Write carefully. \"\ninsert: unsupported\narguments: topic\ntags: writing\nsource: integration-test\n---\n\n# Writing Guide\n\nWrite about $topic.\n",
		),
		0o600,
	); err != nil {
		t.Fatalf("write instruction SKILL.md: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(instructionDir, "docs", "reference.txt"),
		[]byte("reference"),
		0o600,
	); err != nil {
		t.Fatalf("write instruction resource: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(templateDir, "SKILL.md"),
		[]byte(
			"---\nname: message-template\ndescription: A chat message template.\ninsert: user-message\narguments:\n  - name: recipient\n    default: friend\ntags:\n  - template\n---\n\n# Greeting\n\nHello, $recipient!\n",
		),
		0o600,
	); err != nil {
		t.Fatalf("write template SKILL.md: %v", err)
	}

	fsp, err := fsskillprovider.New()
	if err != nil {
		t.Fatalf("new fs provider: %v", err)
	}
	rt, err := agentskills.New(agentskills.WithProvider(fsp))
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	instructionDef := spec.SkillDef{
		Type: fsskillprovider.Type, Name: "writing-skill", Location: instructionDir,
	}
	templateDef := spec.SkillDef{
		Type: fsskillprovider.Type, Name: "message-template", Location: templateDir,
	}
	instruction, err := rt.AddSkill(ctx, instructionDef)
	if err != nil {
		t.Fatalf("AddSkill(instruction): %v", err)
	}
	if instruction.Insert != spec.SkillInsertInstructions || instruction.Description != "Write carefully." {
		t.Fatalf("instruction record = %+v", instruction)
	}
	if !instruction.Resources.HasResources || instruction.Resources.TotalCount != 1 {
		t.Fatalf("instruction resources = %+v", instruction.Resources)
	}
	if instruction.RawFrontmatter["source"] != "integration-test" {
		t.Fatalf("instruction raw frontmatter = %+v", instruction.RawFrontmatter)
	}
	if !strings.Contains(strings.Join(instruction.Warnings, "\n"), "unsupported frontmatter.insert") {
		t.Fatalf("instruction warnings = %v", instruction.Warnings)
	}
	if _, err := rt.AddSkill(ctx, templateDef); err != nil {
		t.Fatalf("AddSkill(template): %v", err)
	}

	available, err := rt.SkillsPrompt(ctx, nil)
	if err != nil {
		t.Fatalf("SkillsPrompt(available): %v", err)
	}
	if !strings.Contains(available, "name: writing-skill") || strings.Contains(available, "message-template") {
		t.Fatalf("available prompt did not apply insert filtering:\n%s", available)
	}

	rendered, err := rt.RenderSkill(ctx, agentskills.RenderSkillParams{
		Def:       templateDef,
		Arguments: map[string]string{"recipient": "Ada"},
	})
	if err != nil {
		t.Fatalf("RenderSkill(template): %v", err)
	}
	if rendered.Insert != spec.SkillInsertUserMessage || !strings.Contains(rendered.Text, "Hello, Ada!") {
		t.Fatalf("rendered template = %+v", rendered)
	}
	if len(rendered.Tags) != 1 || rendered.Tags[0] != "template" {
		t.Fatalf("rendered tags = %v", rendered.Tags)
	}

	sid, active, err := rt.NewSession(
		ctx,
		agentskills.WithSessionActiveSkills([]spec.SkillDef{instructionDef}),
	)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })
	if len(active) != 1 || active[0] != instructionDef {
		t.Fatalf("active defs = %+v", active)
	}

	activePrompt, err := rt.SkillsPrompt(ctx, &agentskills.SkillFilter{
		SessionID: sid,
		Activity:  spec.SkillActivityActive,
	})
	if err != nil {
		t.Fatalf("SkillsPrompt(active): %v", err)
	}
	if !strings.Contains(activePrompt, "Writing Guide") || strings.Contains(activePrompt, "message-template") {
		t.Fatalf("active prompt = %s", activePrompt)
	}

	if _, err := rt.AddSkill(ctx, spec.SkillDef{
		Type: fsskillprovider.Type, Name: " writing-skill", Location: instructionDir,
	}); !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("AddSkill(whitespace def): expected ErrInvalidArgument, got %v", err)
	}
	badDir := filepath.Join(tmp, "bad-skill")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatalf("mkdir bad skill: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(badDir, "SKILL.md"),
		[]byte("---\nname: another-skill\ndescription: x\n---\n"),
		0o600,
	); err != nil {
		t.Fatalf("write bad SKILL.md: %v", err)
	}
	if _, err := rt.AddSkill(ctx, spec.SkillDef{
		Type: fsskillprovider.Type, Name: "bad-skill", Location: badDir,
	}); err == nil || !strings.Contains(err.Error(), "must match expected name") {
		t.Fatalf("AddSkill(mismatched document): expected name validation error, got %v", err)
	}
}
