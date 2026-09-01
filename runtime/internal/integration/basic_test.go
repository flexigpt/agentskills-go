package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/document"
	"github.com/flexigpt/agentskills-go/provider/fs"
	"github.com/flexigpt/agentskills-go/runtime"
	"github.com/flexigpt/agentskills-go/runtime/spec"

	"github.com/flexigpt/agentskills-go/provider"
)

func TestBasicRuntimeFlow(t *testing.T) {
	ctx := t.Context()
	skillDirectory := filepath.Join(t.TempDir(), "hello-skill")
	if err := os.MkdirAll(skillDirectory, 0o755); err != nil {
		t.Fatalf("create skill directory: %v", err)
	}

	skillDocument := `---
name: hello-skill
description: Greets a named person.
arguments:
  - name: recipient
    default: world
---

# Hello Skill

Say hello to $recipient.
`
	if err := os.WriteFile(
		filepath.Join(skillDirectory, document.SkillDocumentFileName),
		[]byte(skillDocument),
		0o600,
	); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	filesystemProvider, err := fs.New()
	if err != nil {
		t.Fatalf("create filesystem provider: %v", err)
	}

	rt, err := runtime.New(
		runtime.WithProvider(filesystemProvider),
	)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	record, err := rt.AddSkill(ctx, provider.SkillDef{
		Type:     fs.Type,
		Name:     "hello-skill",
		Location: skillDirectory,
	})
	if err != nil {
		t.Fatalf("add skill: %v", err)
	}

	sessionID, active, err := rt.NewSession(
		ctx,
		runtime.WithSessionAllowedSkills(
			[]provider.SkillDef{record.Def},
		),
		runtime.WithSessionActiveSkills(
			[]provider.SkillDef{record.Def},
		),
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if len(active) != 1 || active[0] != record.Def {
		t.Fatalf("unexpected initial active skills: %#v", active)
	}

	prompt, err := rt.SkillsPrompt(ctx, &runtime.SkillFilter{
		SessionID: sessionID,
		Activity:  spec.SkillActivityActive,
	})
	if err != nil {
		t.Fatalf("build active-skills prompt: %v", err)
	}
	if !strings.Contains(prompt, "Say hello to world.") {
		t.Fatalf("active prompt did not render default argument: %q", prompt)
	}

	rendered, err := rt.RenderSkill(ctx, runtime.RenderSkillParams{
		Def: record.Def,
		Arguments: map[string]string{
			"recipient": "Ada",
		},
	})
	if err != nil {
		t.Fatalf("render skill: %v", err)
	}
	if !strings.Contains(rendered.Text, "Say hello to Ada.") {
		t.Fatalf("rendered skill did not apply argument: %q", rendered.Text)
	}

	registry, err := rt.NewSessionRegistry(ctx, sessionID)
	if err != nil {
		t.Fatalf("create session tool registry: %v", err)
	}
	if registry == nil {
		t.Fatal("session tool registry is nil")
	}

	if err := rt.CloseSession(ctx, sessionID); err != nil {
		t.Fatalf("close session: %v", err)
	}
}
