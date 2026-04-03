package integration

import (
	"context"
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
