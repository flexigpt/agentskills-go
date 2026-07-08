package agentskills

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/flexigpt/agentskills-go/spec"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

type runtimeTestProvider struct {
	typ string
}

func (p *runtimeTestProvider) Type() string { return p.typ }

func (p *runtimeTestProvider) Index(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.ProviderSkillIndexRecord{}, err
	}

	key := spec.ProviderSkillKey{Type: def.Type, Name: def.Name, Location: "CANON:" + def.Location}

	switch def.Name {
	case "instructions":
		return spec.ProviderSkillIndexRecord{
			Key:         key,
			Description: "instruction skill",
			Insert:      spec.SkillInsertInstructions,
		}, nil
	case "template":
		return spec.ProviderSkillIndexRecord{
			Key:         key,
			Description: "template skill",
			Insert:      spec.SkillInsertUserMessage,
			Arguments: []spec.SkillArgument{
				{Name: "name", Default: "World"},
				{Name: "mood", Default: "calm"},
			},
			RawFrontmatter: map[string]any{"insert": "user-message", "kind": "template"},
		}, nil
	default:
		return spec.ProviderSkillIndexRecord{Key: key, Description: "skill"}, nil
	}
}

func (p *runtimeTestProvider) LoadBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch key.Name {
	case "instructions":
		return "# Instructions\nUse this skill for guidance.\n", nil
	case "template":
		return "Hello $name, mood={{ mood }}. Escaped \\$name and {{ unknown }}.\n", nil
	default:
		return "BODY:" + key.Name, nil
	}
}

func (p *runtimeTestProvider) ReadResource(
	ctx context.Context,
	key spec.ProviderSkillKey,
	resourceLocation string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, spec.ErrInvalidArgument
}

func (p *runtimeTestProvider) RunScript(
	ctx context.Context,
	key spec.ProviderSkillKey,
	scriptLocation string,
	args []string,
	env map[string]string,
	workDir string,
) (spec.RunScriptOut, error) {
	return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
}

func TestRuntime_RenderSkill_SkillsPrompt_AndInsertFiltering(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	p := &runtimeTestProvider{typ: "p"}
	rt, err := New(WithProvider(p))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	instructionsDef := spec.SkillDef{Type: "p", Name: "instructions", Location: "/skills/instructions"}
	templateDef := spec.SkillDef{Type: "p", Name: "template", Location: "/skills/template"}

	if _, err := rt.AddSkill(ctx, instructionsDef); err != nil {
		t.Fatalf("AddSkill(instructions): %v", err)
	}
	if _, err := rt.AddSkill(ctx, templateDef); err != nil {
		t.Fatalf("AddSkill(template): %v", err)
	}

	prompt, err := rt.SkillsPrompt(ctx, &SkillFilter{
		Types:       []string{" p ", "p", ""},
		AllowSkills: []spec.SkillDef{instructionsDef, instructionsDef},
		Activity:    spec.SkillActivityAny,
	})
	if err != nil {
		t.Fatalf("SkillsPrompt(nil): %v", err)
	}
	if !strings.Contains(prompt, "instructions") || strings.Contains(prompt, "template") {
		t.Fatalf("expected only instructions skill in available prompt, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "CANON:") {
		t.Fatalf("prompt should not leak canonical locations, got:\n%s", prompt)
	}

	recs, err := rt.ListSkills(ctx, &SkillListFilter{
		Types:       []string{" p ", "p", ""},
		AllowSkills: []spec.SkillDef{templateDef, templateDef},
		Inserts:     []spec.SkillInsert{spec.SkillInsertUserMessage, spec.SkillInsertUserMessage},
	})
	if err != nil {
		t.Fatalf("ListSkills(insert filter): %v", err)
	}
	if len(recs) != 1 || recs[0].Def != templateDef {
		t.Fatalf("expected only user-message skill in insert-filtered list, got %+v", recs)
	}

	rendered, err := rt.RenderSkill(ctx, RenderSkillParams{
		Def:       templateDef,
		Arguments: map[string]string{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("RenderSkill: %v", err)
	}
	if rendered.Name != "template" {
		t.Fatalf("expected fallback name from key, got %q", rendered.Name)
	}
	if rendered.Insert != spec.SkillInsertUserMessage {
		t.Fatalf("expected user-message insert, got %q", rendered.Insert)
	}
	if rendered.Description != "template skill" {
		t.Fatalf("unexpected description: %q", rendered.Description)
	}
	if rendered.DisplayName != "" {
		t.Fatalf("expected empty display name, got %q", rendered.DisplayName)
	}
	wantText := "Hello Alice, mood=calm. Escaped $name and {{ unknown }}.\n"
	if rendered.Text != wantText {
		t.Fatalf("unexpected rendered text\n\ngot:\n%s\n\nwant:\n%s", rendered.Text, wantText)
	}
	if rendered.AppliedArguments["name"] != "Alice" || rendered.AppliedArguments["mood"] != "calm" {
		t.Fatalf("unexpected applied arguments: %+v", rendered.AppliedArguments)
	}
	if !reflect.DeepEqual(
		rendered.Arguments,
		[]spec.SkillArgument{{Name: "name", Default: "World"}, {Name: "mood", Default: "calm"}},
	) {
		t.Fatalf("unexpected declared arguments: %+v", rendered.Arguments)
	}
	if rendered.RawFrontmatter["kind"] != "template" {
		t.Fatalf("expected raw frontmatter to round-trip, got %+v", rendered.RawFrontmatter)
	}
	if len(rendered.Warnings) != 1 || rendered.Warnings[0] != "unknown placeholder left unchanged: unknown" {
		t.Fatalf("unexpected warnings: %+v", rendered.Warnings)
	}
}

func TestRuntime_CloseSession_EmptyIDAndDelete(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt, err := New(WithProvider(&runtimeTestProvider{typ: "p"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := rt.CloseSession(ctx, ""); err != nil {
		t.Fatalf("CloseSession(empty): %v", err)
	}

	sid, active, err := rt.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected no active skills in new session, got %+v", active)
	}
	if err := rt.CloseSession(ctx, sid); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := rt.NewSessionRegistry(ctx, sid); !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("expected session to be removed, got %v", err)
	}
}

func TestRuntime_RenderSkill_Errors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt, err := New(WithProvider(&runtimeTestProvider{typ: "p"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	validDef := spec.SkillDef{Type: "p", Name: "template", Location: "/skills/template"}
	if _, err := rt.AddSkill(ctx, validDef); err != nil {
		t.Fatalf("AddSkill: %v", err)
	}

	tests := []struct {
		name string
		do   func() error
		want error
	}{
		{
			name: "nil runtime receiver",
			do: func() error {
				var nilRT *Runtime
				_, err := nilRT.RenderSkill(ctx, RenderSkillParams{Def: validDef})
				return err
			},
			want: spec.ErrInvalidArgument,
		},
		{
			name: "nil context",
			do: func() error {
				var nilCtx context.Context
				_, err := rt.RenderSkill(nilCtx, RenderSkillParams{Def: validDef})
				return err
			},
			want: spec.ErrInvalidArgument,
		},
		{
			name: "unknown skill def",
			do: func() error {
				_, err := rt.RenderSkill(
					ctx,
					RenderSkillParams{Def: spec.SkillDef{Type: "p", Name: "missing", Location: "/skills/missing"}},
				)
				return err
			},
			want: spec.ErrSkillNotFound,
		},
		{
			name: "leading whitespace in def is rejected",
			do: func() error {
				_, err := rt.RenderSkill(
					ctx,
					RenderSkillParams{Def: spec.SkillDef{Type: " p", Name: "template", Location: "/skills/template"}},
				)
				return err
			},
			want: spec.ErrInvalidArgument,
		},
		{
			name: "missing required fields are rejected",
			do: func() error {
				_, err := rt.RenderSkill(
					ctx,
					RenderSkillParams{Def: spec.SkillDef{Type: "p", Name: "", Location: "/skills/template"}},
				)
				return err
			},
			want: spec.ErrInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.do()
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}
