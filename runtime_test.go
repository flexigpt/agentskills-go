package agentskills

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

type fakeProvider struct {
	typ string

	indexCalls    atomic.Int32
	loadBodyCalls atomic.Int32

	indexFn    func(context.Context, spec.SkillKey) (spec.SkillRecord, error)
	loadBodyFn func(context.Context, spec.SkillKey) (string, error)
	readFn     func(context.Context, spec.SkillKey, string, spec.ReadResourceEncoding) ([]llmtoolsgoSpec.ToolOutputUnion, error)
	runFn      func(context.Context, spec.SkillKey, string, []string, map[string]string, string) (spec.RunScriptOut, error)
}

func (p *fakeProvider) Type() string { return p.typ }

func (p *fakeProvider) Index(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	p.indexCalls.Add(1)
	if p.indexFn != nil {
		return p.indexFn(ctx, key)
	}
	return spec.SkillRecord{Key: key, Description: "desc"}, nil
}

func (p *fakeProvider) LoadBody(ctx context.Context, key spec.SkillKey) (string, error) {
	p.loadBodyCalls.Add(1)
	if p.loadBodyFn != nil {
		return p.loadBodyFn(ctx, key)
	}
	return "BODY:" + key.Name, nil
}

func (p *fakeProvider) ReadResource(
	ctx context.Context,
	key spec.SkillKey,
	resourcePath string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	if p.readFn != nil {
		return p.readFn(ctx, key, resourcePath, encoding)
	}
	return nil, spec.ErrInvalidArgument
}

func (p *fakeProvider) RunScript(
	ctx context.Context,
	key spec.SkillKey,
	scriptPath string,
	args []string,
	env map[string]string,
	workdir string,
) (spec.RunScriptOut, error) {
	if p.runFn != nil {
		return p.runFn(ctx, key, scriptPath, args, env, workdir)
	}
	return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
}

func TestNew_RuntimeOptionsValidation(t *testing.T) {
	t.Parallel()

	pOK := &fakeProvider{typ: "ok"}

	tests := []struct {
		name    string
		opts    []Option
		wantErr string
	}{
		{
			name:    "nil provider in WithProvider",
			opts:    []Option{WithProvider(nil)},
			wantErr: "nil provider",
		},
		{
			name:    "provider.Type empty",
			opts:    []Option{WithProvider(&fakeProvider{typ: ""})},
			wantErr: "provider.Type() returned empty string",
		},
		{
			name: "duplicate provider type via WithProvider",
			opts: []Option{
				WithProvider(&fakeProvider{typ: "dup"}),
				WithProvider(&fakeProvider{typ: "dup"}),
			},
			wantErr: "duplicate provider type",
		},
		{
			name: "nil provider in WithProviders map",
			opts: []Option{
				WithProviders(map[string]spec.SkillProvider{"x": nil}),
			},
			wantErr: "nil provider for type",
		},
		{
			name: "empty type key in WithProviders map",
			opts: []Option{
				WithProviders(map[string]spec.SkillProvider{"": pOK}),
			},
			wantErr: "empty provider type key",
		},
		{
			name: "type mismatch key vs provider.Type",
			opts: []Option{
				WithProviders(map[string]spec.SkillProvider{"a": &fakeProvider{typ: "b"}}),
			},
			wantErr: "provider type mismatch",
		},
		{
			name: "logger nil allowed and normalized",
			opts: []Option{
				WithLogger(nil),
				WithProvider(pOK),
			},
			wantErr: "",
		},
		{
			name: "WithProviders map is copied (caller mutation doesn't affect runtime)",
			opts: func() []Option {
				m := map[string]spec.SkillProvider{"ok": pOK}
				opts := []Option{WithProviders(m)}
				delete(m, "ok")
				return opts
			}(),
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt, err := New(tt.opts...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got runtime=%v err=%v", tt.wantErr, rt, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if rt == nil {
				t.Fatalf("expected non-nil runtime")
			}
		})
	}
}

func TestRuntime_ProviderTypesSorted(t *testing.T) {
	t.Parallel()

	rt, err := New(
		WithProvider(&fakeProvider{typ: "z"}),
		WithProvider(&fakeProvider{typ: "a"}),
		WithProvider(&fakeProvider{typ: "m"}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := rt.ProviderTypes()
	want := append([]string(nil), got...)
	sort.Strings(want)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ProviderTypes not sorted: got=%v want=%v", got, want)
	}
}

func TestRuntime_AddRemoveListAndSessionActivation(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{
		typ: "fake",
		indexFn: func(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
			// Normalize path in the returned record.
			key.Location = "NORM:" + key.Location
			return spec.SkillRecord{
				Key:         key,
				Description: "d-" + key.Name,
				Properties:  map[string]any{"name": key.Name},
				Digest:      "sha256:deadbeef",
			}, nil
		},
		loadBodyFn: func(ctx context.Context, key spec.SkillKey) (string, error) {
			return "SKILL BODY for " + key.Name, nil
		},
	}

	rt, err := New(
		WithLogger(slog.New(slog.NewTextHandler(nil, nil))),
		WithProvider(p),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rec, err := rt.AddSkill(
		ctx,
		spec.SkillKey{Type: "fake", SkillHandle: spec.SkillHandle{Name: "s1", Location: "/p1"}},
	)
	if err != nil {
		t.Fatalf("AddSkill: %v", err)
	}
	if rec.Key.Location == "/p1" || !strings.HasPrefix(rec.Key.Location, "NORM:") {
		t.Fatalf("expected normalized path in record, got: %q", rec.Key.Location)
	}

	_, _ = rt.AddSkill(ctx, spec.SkillKey{Type: "fake", SkillHandle: spec.SkillHandle{Name: "s2", Location: "/p2"}})
	all := rt.ListSkills(nil)
	if len(all) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(all))
	}
	only := rt.ListSkills(&SkillFilter{Types: []string{"fake"}, NamePrefix: "s"})
	if len(only) != 2 {
		t.Fatalf("expected 2 skills from filter, got %d", len(only))
	}

	// Session activation + active XML includes body.
	sid, err := rt.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	handles, err := rt.SessionActivateKeys(ctx, sid, []spec.SkillKey{rec.Key}, spec.LoadModeReplace)
	if err != nil {
		t.Fatalf("SessionActivateKeys: %v", err)
	}
	if len(handles) != 1 {
		t.Fatalf("expected 1 handle, got %d", len(handles))
	}

	ax, err := rt.ActiveSkillsPromptXML(ctx, sid)
	if err != nil {
		t.Fatalf("ActiveSkillsPromptXML: %v", err)
	}
	if !strings.Contains(ax, "<activeSkills") || !strings.Contains(ax, "SKILL BODY") {
		t.Fatalf("unexpected active XML: %s", ax)
	}

	// Remove prunes session.
	_, err = rt.RemoveSkill(ctx, rec.Key)
	if err != nil {
		t.Fatalf("RemoveSkill: %v", err)
	}
	_, err = rt.ActiveSkillsPromptXML(ctx, sid)
	if err != nil && !errors.Is(err, spec.ErrSessionNotFound) {
		// Session still exists, but the skill is pruned; ActiveSkillsPromptXML should succeed with empty list.
		// If it errors, it must not be "skill not found" leaking; this is a regression guard.
		if errors.Is(err, spec.ErrSkillNotFound) {
			t.Fatalf("unexpected ErrSkillNotFound after prune: %v", err)
		}
	}
}

func TestRuntime_NewSessionRegistry_UnknownSession(t *testing.T) {
	t.Parallel()

	rt, err := New(WithProvider(&fakeProvider{typ: "fake"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = rt.NewSessionRegistry(t.Context(), "missing")
	if !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got: %v", err)
	}
}
