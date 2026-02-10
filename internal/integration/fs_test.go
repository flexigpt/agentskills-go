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
		spec.SkillKey{
			Type:        fsskillprovider.Type,
			SkillHandle: spec.SkillHandle{Name: "hello-skill", Location: skillDir},
		},
	)
	if err != nil {
		t.Fatalf("add skill: %v", err)
	}
	if rec.Key.Location == "" {
		t.Fatalf("expected normalized path in record")
	}

	// Available skills prompt (metadata only).
	availXML, err := rt.AvailableSkillsPromptXML(nil)
	if err != nil {
		t.Fatalf("available prompt xml: %v", err)
	}
	if !strings.Contains(availXML, "<availableSkills") {
		t.Fatalf("expected <availableSkills> xml, got: %s", availXML)
	}
	if !strings.Contains(availXML, "<name>hello-skill</name>") {
		t.Fatalf("expected skill name in xml, got: %s", availXML)
	}

	// Create session and activate skill (progressive disclosure).
	sid, err := rt.NewSession(ctx)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	handles, err := rt.SessionActivateKeys(ctx, sid, []spec.SkillKey{rec.Key}, spec.LoadModeReplace)
	if err != nil {
		t.Fatalf("activate keys: %v", err)
	}
	if len(handles) != 1 {
		t.Fatalf("expected 1 active handle, got %d", len(handles))
	}

	activeXML, err := rt.ActiveSkillsPromptXML(ctx, sid)
	if err != nil {
		t.Fatalf("active prompt xml: %v", err)
	}
	if !strings.Contains(activeXML, "<activeSkills") {
		t.Fatalf("expected <activeSkills> xml, got: %s", activeXML)
	}
	if !strings.Contains(activeXML, "Hello Skill") {
		t.Fatalf("expected skill body in active xml, got: %s", activeXML)
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
