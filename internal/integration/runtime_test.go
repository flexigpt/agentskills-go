package integration

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/flexigpt/agentskills-go"
	"github.com/flexigpt/agentskills-go/spec"
)

func TestNew_RuntimeOptionsValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    []agentskills.Option
		wantErr string
	}{
		{
			name:    "nil provider in agentskills.WithProvider",
			opts:    []agentskills.Option{agentskills.WithProvider(nil)},
			wantErr: "nil provider",
		},
		{
			name:    "provider.Type empty",
			opts:    []agentskills.Option{agentskills.WithProvider(&fakeProvider{typ: ""})},
			wantErr: "provider.Type() returned empty string",
		},
		{
			name: "duplicate provider type via agentskills.WithProvider",
			opts: []agentskills.Option{
				agentskills.WithProvider(&fakeProvider{typ: "dup"}),
				agentskills.WithProvider(&fakeProvider{typ: "dup"}),
			},
			wantErr: "duplicate provider type",
		},
		{
			name: "nil provider in agentskills.WithProvidersByType map",
			opts: []agentskills.Option{
				agentskills.WithProvidersByType(map[string]spec.SkillProvider{"x": nil}),
			},
			wantErr: "nil provider for type",
		},
		{
			name: "empty type key in agentskills.WithProvidersByType map",
			opts: []agentskills.Option{
				agentskills.WithProvidersByType(map[string]spec.SkillProvider{"": &fakeProvider{typ: "ok"}}),
			},
			wantErr: "empty provider type key",
		},
		{
			name: "type mismatch key vs provider.Type",
			opts: []agentskills.Option{
				agentskills.WithProvidersByType(map[string]spec.SkillProvider{"a": &fakeProvider{typ: "b"}}),
			},
			wantErr: "provider type mismatch",
		},
		{
			name: "duplicate provider type across agentskills.WithProvider and agentskills.WithProvidersByType",
			opts: []agentskills.Option{
				agentskills.WithProvider(&fakeProvider{typ: "x"}),
				agentskills.WithProvidersByType(map[string]spec.SkillProvider{"x": &fakeProvider{typ: "x"}}),
			},
			wantErr: "duplicate provider type",
		},
		{
			name: "logger nil allowed and normalized",
			opts: []agentskills.Option{
				agentskills.WithLogger(nil),
				agentskills.WithProvider(&fakeProvider{typ: "ok"}),
			},
			wantErr: "",
		},
		{
			name: "agentskills.WithProvidersByType input is snapshotted (caller mutation after agentskills.WithProvidersByType does not affect runtime)",
			opts: func() []agentskills.Option {
				pOK := &fakeProvider{typ: "ok"}
				m := map[string]spec.SkillProvider{"ok": pOK}
				o := agentskills.WithProvidersByType(m)
				delete(m, "ok")
				return []agentskills.Option{o}
			}(),
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rt, err := agentskills.New(tt.opts...)
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

			if strings.Contains(tt.name, "snapshotted") {
				got := rt.ProviderTypes()
				found := slices.Contains(got, "ok")
				if !found {
					t.Fatalf("expected ProviderTypes to include %q, got %v", "ok", got)
				}
			}
		})
	}
}

func TestRuntime_ProviderTypesSorted(t *testing.T) {
	t.Parallel()

	rt := mustNewRuntime(t,
		agentskills.WithProvider(&fakeProvider{typ: "z"}),
		agentskills.WithProvider(&fakeProvider{typ: "a"}),
		agentskills.WithProvider(&fakeProvider{typ: "m"}),
	)

	got := rt.ProviderTypes()
	want := append([]string(nil), got...)
	sort.Strings(want)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ProviderTypes not sorted: got=%v want=%v", got, want)
	}
}

func TestRuntime_NilContext_ReturnsInvalidArgument(t *testing.T) {
	t.Parallel()

	rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
	var nilCtx context.Context

	_, err := rt.AddSkill(nilCtx, spec.SkillDef{Type: "p", Name: "a", Location: "/a"})
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("AddSkill(nil ctx): expected ErrInvalidArgument, got %v", err)
	}

	_, err = rt.RemoveSkill(nilCtx, spec.SkillDef{Type: "p", Name: "a", Location: "/a"})
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("RemoveSkill(nil ctx): expected ErrInvalidArgument, got %v", err)
	}

	_, _, err = rt.NewSession(nilCtx)
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("NewSession(nil ctx): expected ErrInvalidArgument, got %v", err)
	}

	err = rt.CloseSession(nilCtx, "sid")
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("CloseSession(nil ctx): expected ErrInvalidArgument, got %v", err)
	}

	_, err = rt.SkillsPrompt(nilCtx, nil)
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("SkillsPrompt(nil ctx): expected ErrInvalidArgument, got %v", err)
	}

	_, err = rt.ListSkills(nilCtx, nil)
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("ListSkills(nil ctx): expected ErrInvalidArgument, got %v", err)
	}

	_, err = rt.NewSessionRegistry(nilCtx, "sid")
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("NewSessionRegistry(nil ctx): expected ErrInvalidArgument, got %v", err)
	}
}

func TestRuntime_AddSkill_RemoveSkill_Errors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	pCanon := &fakeProvider{
		typ: "p",
		indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
			return spec.ProviderSkillIndexRecord{
				Key: spec.ProviderSkillKey{
					Type:     def.Type,
					Name:     def.Name,
					Location: "NORM:" + def.Location,
				},
				Description: "d:" + def.Name,
			}, nil
		},
	}

	tests := []struct {
		name    string
		do      func() error
		wantErr error
	}{
		{
			name: "AddSkill invalid argument (missing fields)",
			do: func() error {
				rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.AddSkill(ctx, spec.SkillDef{})
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "AddSkill invalid argument (leading/trailing whitespace is rejected)",
			do: func() error {
				rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.AddSkill(ctx, spec.SkillDef{Type: " p", Name: "a", Location: "/a"})
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "AddSkill provider not found",
			do: func() error {
				rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.AddSkill(ctx, spec.SkillDef{
					Type:     "missing",
					Name:     "s",
					Location: "/x",
				})
				return err
			},
			wantErr: spec.ErrProviderNotFound,
		},
		{
			name: "RemoveSkill missing",
			do: func() error {
				rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.RemoveSkill(ctx, spec.SkillDef{
					Type:     "p",
					Name:     "nope",
					Location: "/nope",
				})
				return err
			},
			wantErr: spec.ErrSkillNotFound,
		},
		{
			name: "AddSkill duplicate",
			do: func() error {
				rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
				def := spec.SkillDef{Type: "p", Name: "dup", Location: "/d"}
				if _, err := rt.AddSkill(ctx, def); err != nil {
					return err
				}
				_, err := rt.AddSkill(ctx, def)
				return err
			},
			wantErr: spec.ErrSkillAlreadyExists,
		},
		{
			name: "RemoveSkill does not match provider-canonicalized location (must match exact user-provided def)",
			do: func() error {
				rt := mustNewRuntime(t, agentskills.WithProvider(pCanon))
				orig := spec.SkillDef{Type: "p", Name: "s1", Location: "/p1"}
				if _, err := rt.AddSkill(ctx, orig); err != nil {
					return err
				}
				_, err := rt.RemoveSkill(ctx, spec.SkillDef{Type: "p", Name: "s1", Location: "NORM:/p1"})
				return err
			},
			wantErr: spec.ErrSkillNotFound,
		},
		{
			name: "nil runtime receiver returns invalid argument",
			do: func() error {
				var nilRT *agentskills.Runtime
				_, err := nilRT.AddSkill(ctx, spec.SkillDef{Type: "p", Name: "x", Location: "/x"})
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.do()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestRuntime_NewSession_InitialActiveSkills_ReturnsExactlyProvidedDefs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
	def := spec.SkillDef{Type: "p", Name: "s1", Location: "/p1"}
	_ = mustAddSkill(t, rt, ctx, def)

	_, active := mustNewSession(t, rt, ctx, agentskills.WithSessionActiveSkills([]spec.SkillDef{def}))
	if len(active) != 1 || active[0] != def {
		t.Fatalf("expected active defs [%+v], got %+v", def, active)
	}
}

func TestRuntime_NewSession_InitialActiveSkills_DuplicateDefErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
	def := spec.SkillDef{Type: "p", Name: "s1", Location: "/p1"}
	_ = mustAddSkill(t, rt, ctx, def)

	_, _, err := rt.NewSession(ctx, agentskills.WithSessionActiveSkills([]spec.SkillDef{def, def}))
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestRuntime_NewSession_MaxActiveOverride_AppliesToInitialActiveSkills(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
	defA := spec.SkillDef{Type: "p", Name: "a", Location: "/a"}
	defB := spec.SkillDef{Type: "p", Name: "b", Location: "/b"}
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defB)

	_, _, err := rt.NewSession(ctx,
		agentskills.WithSessionMaxActivePerSession(1),
		agentskills.WithSessionActiveSkills([]spec.SkillDef{defA, defB}),
	)
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestRuntime_NewSession_UnknownActiveDef_ReturnsSkillNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
	_, _, err := rt.NewSession(ctx,
		agentskills.WithSessionActiveSkills([]spec.SkillDef{{Type: "p", Name: "missing", Location: "/missing"}}),
	)
	if !errors.Is(err, spec.ErrSkillNotFound) {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestRuntime_ListSkills_ActivityAndSessionFilters(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))

	defA := spec.SkillDef{Type: "p", Name: "a", Location: "/a"}
	defB := spec.SkillDef{Type: "p", Name: "b", Location: "/b"}
	defC := spec.SkillDef{Type: "p", Name: "c", Location: "/c"}
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defB)
	_ = mustAddSkill(t, rt, ctx, defC)

	sid, _ := mustNewSession(t, rt, ctx, agentskills.WithSessionActiveSkills([]spec.SkillDef{defA, defB}))
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	tests := []struct {
		name      string
		filter    *agentskills.SkillListFilter
		wantCount int
		wantErr   error
	}{
		{
			name:      "nil filter => all",
			filter:    nil,
			wantCount: 3,
		},
		{
			name:      "activity any with session => all records",
			filter:    &agentskills.SkillListFilter{SessionID: sid, Activity: spec.SkillActivityAny},
			wantCount: 3,
		},
		{
			name:      "activity active with session => only active",
			filter:    &agentskills.SkillListFilter{SessionID: sid, Activity: spec.SkillActivityActive},
			wantCount: 2,
		},
		{
			name:      "activity inactive with session => only inactive",
			filter:    &agentskills.SkillListFilter{SessionID: sid, Activity: spec.SkillActivityInactive},
			wantCount: 1,
		},
		{
			name:      "activity inactive without session => treated like all",
			filter:    &agentskills.SkillListFilter{Activity: spec.SkillActivityInactive},
			wantCount: 3,
		},
		{
			name:    "activity active without session => invalid argument",
			filter:  &agentskills.SkillListFilter{Activity: spec.SkillActivityActive},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name:    "invalid activity => invalid argument",
			filter:  &agentskills.SkillListFilter{Activity: spec.SkillActivity("nope")},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "session missing => ErrSessionNotFound",
			filter: &agentskills.SkillListFilter{
				SessionID: spec.SessionID("missing"),
				Activity:  spec.SkillActivityAny,
			},
			wantErr: spec.ErrSessionNotFound,
		},
		{
			name: "allowSkills applies + inactive: allow only A and C, but A is active => only C remains",
			filter: &agentskills.SkillListFilter{
				SessionID:      sid,
				Activity:       spec.SkillActivityInactive,
				AllowSkills:    []spec.SkillDef{defA, defC},
				NamePrefix:     "",
				Types:          nil,
				LocationPrefix: "",
			},
			wantCount: 1,
		},
		{
			name:      "types filter",
			filter:    &agentskills.SkillListFilter{Types: []string{"p"}},
			wantCount: 3,
		},
		{
			name:      "name prefix filter uses host def name",
			filter:    &agentskills.SkillListFilter{NamePrefix: "b"},
			wantCount: 1,
		},
		{
			name:      "location prefix filter uses host def location",
			filter:    &agentskills.SkillListFilter{LocationPrefix: "/b"},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rt.ListSkills(ctx, tt.filter)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected err %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ListSkills: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Fatalf("expected %d records, got %d: %+v", tt.wantCount, len(got), got)
			}
		})
	}
}

func TestRuntime_SkillsPrompt_SectionsOrderingAndFiltering(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	p := &fakeProvider{
		typ: "p",
		indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
			return spec.ProviderSkillIndexRecord{
				Key: spec.ProviderSkillKey{
					Type:     def.Type,
					Name:     def.Name,
					Location: "CANON:" + def.Location,
				},
				Description: "desc:" + def.Name,
			}, nil
		},
		loadBodyFn: func(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
			return "BODY<" + key.Name + ">&", nil
		},
	}

	rt := mustNewRuntime(t,
		agentskills.WithLogger(slog.New(slog.DiscardHandler)),
		agentskills.WithProvider(p),
	)

	defB := spec.SkillDef{Type: "p", Name: "b", Location: "/b"}
	defA := spec.SkillDef{Type: "p", Name: "a", Location: "/a"}
	defC := spec.SkillDef{Type: "p", Name: "c", Location: "/c"}
	_ = mustAddSkill(t, rt, ctx, defB)
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defC)

	prompt1, err := rt.SkillsPrompt(ctx, nil)
	if err != nil {
		t.Fatalf("SkillsPrompt: %v", err)
	}
	assertStandaloneAvailablePrompt(t, prompt1)
	assertFirstRecordNotPrefixedBySeparator(t, prompt1, availableSkillsStart, availableSkillsEnd, false)

	av1 := mustParseAvailableSkillsPrompt(t, prompt1)
	if len(av1.Skills) != 3 {
		t.Fatalf("expected 3 available skills, got %d", len(av1.Skills))
	}
	gotNames := []string{av1.Skills[0].Name, av1.Skills[1].Name, av1.Skills[2].Name}
	if strings.Join(gotNames, ",") != "a,b,c" {
		t.Fatalf("expected available sorted by name a,b,c; got %v\nprompt=%s", gotNames, prompt1)
	}
	for _, it := range av1.Skills {
		if strings.HasPrefix(it.Location, "CANON:") {
			t.Fatalf(
				"expected prompt location to be user-provided, got %q (should not start with CANON:)\nprompt=%s",
				it.Location,
				prompt1,
			)
		}
	}

	sid, activeDefs := mustNewSession(t, rt, ctx, agentskills.WithSessionActiveSkills([]spec.SkillDef{defA, defB}))
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })
	if len(activeDefs) != 2 || activeDefs[0] != defA || activeDefs[1] != defB {
		t.Fatalf("expected NewSession active defs order [A B], got %+v", activeDefs)
	}

	before1 := p.loadBodyCalls.Load()
	prompt2, err := rt.SkillsPrompt(
		ctx,
		&agentskills.SkillFilter{SessionID: sid, Activity: spec.SkillActivityAny},
	)
	if err != nil {
		t.Fatalf("SkillsPrompt(any+session): %v", err)
	}
	after1 := p.loadBodyCalls.Load()

	assertWrappedSkillsPrompt(t, prompt2)
	assertFirstRecordNotPrefixedBySeparator(t, prompt2, availableSkillsStart, availableSkillsEnd, false)
	assertFirstRecordNotPrefixedBySeparator(t, prompt2, activeSkillsStart, activeSkillsEnd, true)

	if !strings.Contains(prompt2, "BODY<a>&") || !strings.Contains(prompt2, "BODY<b>&") {
		t.Fatalf("expected raw active bodies in prompt\nprompt=%s", prompt2)
	}

	prompt2b, err := rt.SkillsPrompt(
		ctx,
		&agentskills.SkillFilter{SessionID: sid, Activity: spec.SkillActivityAny},
	)
	if err != nil {
		t.Fatalf("SkillsPrompt(any+session) again: %v", err)
	}
	_ = prompt2b

	after2 := p.loadBodyCalls.Load()
	if after2 != after1 {
		t.Fatalf(
			"expected LoadBody call count not to increase on second prompt call, got before=%d after1=%d after2=%d",
			before1,
			after1,
			after2,
		)
	}

	doc := mustParseSkillsPromptDocument(t, prompt2)
	if doc.Active == nil || doc.Available == nil {
		t.Fatalf("expected both active and available sections present\nprompt=%s", prompt2)
	}

	if len(doc.Active.Skills) != 2 {
		t.Fatalf("expected 2 active skills, got %d\nprompt=%s", len(doc.Active.Skills), prompt2)
	}
	if doc.Active.Skills[0].Name != "a" || doc.Active.Skills[1].Name != "b" {
		t.Fatalf("expected active order [a b], got [%s %s]\nprompt=%s",
			doc.Active.Skills[0].Name, doc.Active.Skills[1].Name, prompt2)
	}
	if strings.TrimSpace(doc.Active.Skills[0].Body) != "BODY<a>&" {
		t.Fatalf("expected active body %q, got %q\nprompt=%s", "BODY<a>&", doc.Active.Skills[0].Body, prompt2)
	}

	if len(doc.Available.Skills) != 1 || doc.Available.Skills[0].Name != "c" {
		t.Fatalf("expected available(inactive) to contain only c, got %+v\nprompt=%s", doc.Available.Skills, prompt2)
	}

	prompt3, err := rt.SkillsPrompt(
		ctx,
		&agentskills.SkillFilter{SessionID: sid, Activity: spec.SkillActivityActive},
	)
	if err != nil {
		t.Fatalf("SkillsPrompt(active): %v", err)
	}
	assertStandaloneActivePrompt(t, prompt3)
	assertFirstRecordNotPrefixedBySeparator(t, prompt3, activeSkillsStart, activeSkillsEnd, true)

	act3 := mustParseActiveSkillsPrompt(t, prompt3)
	if len(act3.Skills) != 2 {
		t.Fatalf("expected 2 active skills, got %d\nprompt=%s", len(act3.Skills), prompt3)
	}

	prompt4, err := rt.SkillsPrompt(
		ctx,
		&agentskills.SkillFilter{SessionID: sid, Activity: spec.SkillActivityInactive},
	)
	if err != nil {
		t.Fatalf("SkillsPrompt(inactive): %v", err)
	}
	assertStandaloneAvailablePrompt(t, prompt4)
	assertFirstRecordNotPrefixedBySeparator(t, prompt4, availableSkillsStart, availableSkillsEnd, false)

	av4 := mustParseAvailableSkillsPrompt(t, prompt4)
	if len(av4.Skills) != 1 || av4.Skills[0].Name != "c" {
		t.Fatalf("expected only c inactive, got %+v\nprompt=%s", av4.Skills, prompt4)
	}

	prompt5, err := rt.SkillsPrompt(ctx, &agentskills.SkillFilter{
		SessionID:      sid,
		Activity:       spec.SkillActivityAny,
		AllowSkills:    []spec.SkillDef{defC},
		NamePrefix:     "",
		Types:          nil,
		LocationPrefix: "",
	})
	if err != nil {
		t.Fatalf("SkillsPrompt(allowSkills): %v", err)
	}
	assertWrappedSkillsPrompt(t, prompt5)

	doc5 := mustParseSkillsPromptDocument(t, prompt5)
	if doc5.Active == nil || doc5.Available == nil {
		t.Fatalf("expected both sections in wrapper\nprompt=%s", prompt5)
	}
	if len(doc5.Active.Skills) != 0 {
		t.Fatalf(
			"expected 0 active skills after allowSkills restriction, got %d\nprompt=%s",
			len(doc5.Active.Skills),
			prompt5,
		)
	}
	if len(doc5.Available.Skills) != 1 || doc5.Available.Skills[0].Name != "c" {
		t.Fatalf("expected available to contain only c after allowSkills restriction, got %+v\nprompt=%s",
			doc5.Available.Skills, prompt5)
	}
}

func TestRuntime_SkillsPrompt_NamePrefixIsLLMHandleNotHostName(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	pa := &fakeProvider{typ: "a"}
	pb := &fakeProvider{typ: "b"}
	rt := mustNewRuntime(t, agentskills.WithProvider(pa), agentskills.WithProvider(pb))

	defA := spec.SkillDef{Type: "a", Name: "x", Location: "/same"}
	defB := spec.SkillDef{Type: "b", Name: "x", Location: "/same"}
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defB)

	promptAll, err := rt.SkillsPrompt(ctx, &agentskills.SkillFilter{Activity: spec.SkillActivityAny})
	if err != nil {
		t.Fatalf("SkillsPrompt: %v", err)
	}
	av := mustParseAvailableSkillsPrompt(t, promptAll)
	if len(av.Skills) != 2 {
		t.Fatalf("expected 2 available skills, got %d\nprompt=%s", len(av.Skills), promptAll)
	}

	name1 := av.Skills[0].Name
	name2 := av.Skills[1].Name
	if name1 == name2 {
		t.Fatalf("expected distinct LLM-visible handle names, got both=%q\nprompt=%s", name1, promptAll)
	}

	pick := name1
	if pick == "x" && name2 != "x" {
		pick = name2
	}

	promptOne, err := rt.SkillsPrompt(
		ctx,
		&agentskills.SkillFilter{NamePrefix: pick, Activity: spec.SkillActivityAny},
	)
	if err != nil {
		t.Fatalf("SkillsPrompt(NamePrefix=%q): %v", pick, err)
	}
	avOne := mustParseAvailableSkillsPrompt(t, promptOne)
	if len(avOne.Skills) != 1 {
		t.Fatalf("expected 1 available skill for NamePrefix=%q, got %d\nprompt=%s", pick, len(avOne.Skills), promptOne)
	}
	if !strings.HasPrefix(avOne.Skills[0].Name, pick) {
		t.Fatalf("expected available skill name %q to have prefix %q\nprompt=%s", avOne.Skills[0].Name, pick, promptOne)
	}

	recs, err := rt.ListSkills(ctx, &agentskills.SkillListFilter{NamePrefix: pick})
	if err != nil {
		t.Fatalf("ListSkills(NamePrefix=%q): %v", pick, err)
	}
	if pick != "x" && len(recs) != 0 {
		t.Fatalf("expected 0 host records for NamePrefix=%q (LLM handle), got %d: %+v", pick, len(recs), recs)
	}
}

func TestRuntime_SkillsPrompt_LocationPrefixUsesUserProvidedLocation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	p := &fakeProvider{
		typ: "p",
		indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
			return spec.ProviderSkillIndexRecord{
				Key: spec.ProviderSkillKey{
					Type:     def.Type,
					Name:     def.Name,
					Location: "CANON:" + def.Location,
				},
				Description: "desc:" + def.Name,
			}, nil
		},
	}

	rt := mustNewRuntime(t, agentskills.WithProvider(p))
	def := spec.SkillDef{Type: "p", Name: "skill", Location: "/user/location"}
	_ = mustAddSkill(t, rt, ctx, def)

	promptUser, err := rt.SkillsPrompt(ctx, &agentskills.SkillFilter{
		Activity:       spec.SkillActivityAny,
		LocationPrefix: "/user/",
	})
	if err != nil {
		t.Fatalf("SkillsPrompt(user location prefix): %v", err)
	}
	avUser := mustParseAvailableSkillsPrompt(t, promptUser)
	if len(avUser.Skills) != 1 {
		t.Fatalf(
			"expected 1 available skill for user-provided location prefix, got %d\nprompt=%s",
			len(avUser.Skills),
			promptUser,
		)
	}

	promptCanon, err := rt.SkillsPrompt(ctx, &agentskills.SkillFilter{
		Activity:       spec.SkillActivityAny,
		LocationPrefix: "CANON:",
	})
	if err != nil {
		t.Fatalf("SkillsPrompt(canonical location prefix): %v", err)
	}
	avCanon := mustParseAvailableSkillsPrompt(t, promptCanon)
	if len(avCanon.Skills) != 0 {
		t.Fatalf(
			"expected 0 available skills for canonicalized location prefix, got %d\nprompt=%s",
			len(avCanon.Skills),
			promptCanon,
		)
	}
}

func TestRuntime_RemoveSkill_PrunesFromAllSessions_ReAddDoesNotResurrectActive(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))
	def := spec.SkillDef{Type: "p", Name: "a", Location: "/a"}
	_ = mustAddSkill(t, rt, ctx, def)

	sid, _ := mustNewSession(t, rt, ctx, agentskills.WithSessionActiveSkills([]spec.SkillDef{def}))
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	if _, err := rt.RemoveSkill(ctx, def); err != nil {
		t.Fatalf("RemoveSkill: %v", err)
	}

	_ = mustAddSkill(t, rt, ctx, def)

	promptOut, err := rt.SkillsPrompt(
		ctx,
		&agentskills.SkillFilter{SessionID: sid, Activity: spec.SkillActivityAny},
	)
	if err != nil {
		t.Fatalf("SkillsPrompt: %v", err)
	}
	assertWrappedSkillsPrompt(t, promptOut)

	doc := mustParseSkillsPromptDocument(t, promptOut)
	if doc.Active == nil || doc.Available == nil {
		t.Fatalf("expected wrapper to contain both sections\nprompt=%s", promptOut)
	}
	if len(doc.Active.Skills) != 0 {
		t.Fatalf("expected 0 active skills after remove+readd, got %d\nprompt=%s", len(doc.Active.Skills), promptOut)
	}
	if len(doc.Available.Skills) != 1 {
		t.Fatalf(
			"expected 1 available skill after remove+readd, got %d\nprompt=%s",
			len(doc.Available.Skills),
			promptOut,
		)
	}
}

func TestRuntime_SkillsPrompt_Errors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "p"}))

	tests := []struct {
		name    string
		do      func() error
		wantErr error
	}{
		{
			name: "nil runtime receiver",
			do: func() error {
				var nilRT *agentskills.Runtime
				_, err := nilRT.SkillsPrompt(ctx, nil)
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "nil context",
			do: func() error {
				var nilCtx context.Context
				_, err := rt.SkillsPrompt(nilCtx, nil)
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "context canceled",
			do: func() error {
				cctx, ccancel := context.WithCancel(ctx)
				ccancel()
				_, err := rt.SkillsPrompt(cctx, nil)
				return err
			},
			wantErr: context.Canceled,
		},
		{
			name: "activity active requires session",
			do: func() error {
				_, err := rt.SkillsPrompt(
					ctx,
					&agentskills.SkillFilter{Activity: spec.SkillActivityActive},
				)
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "invalid activity",
			do: func() error {
				_, err := rt.SkillsPrompt(
					ctx,
					&agentskills.SkillFilter{Activity: spec.SkillActivity("bad")},
				)
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "missing session id => ErrSessionNotFound",
			do: func() error {
				_, err := rt.SkillsPrompt(
					ctx,
					&agentskills.SkillFilter{SessionID: "missing", Activity: spec.SkillActivityAny},
				)
				return err
			},
			wantErr: spec.ErrSessionNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.do()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestRuntime_NewSessionRegistry_UnknownSession(t *testing.T) {
	t.Parallel()

	rt := mustNewRuntime(t, agentskills.WithProvider(&fakeProvider{typ: "fake"}))

	_, err := rt.NewSessionRegistry(t.Context(), "missing")
	if !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got: %v", err)
	}
}

type parsedAvailableSkill struct {
	Name        string
	Location    string
	Description string
}

type parsedAvailablePrompt struct {
	Skills []parsedAvailableSkill
}

type parsedActiveSkill struct {
	Name string
	Body string
}

type parsedActivePrompt struct {
	Skills []parsedActiveSkill
}

type parsedSkillsPrompt struct {
	Available *parsedAvailablePrompt
	Active    *parsedActivePrompt
}

func assertStandaloneAvailablePrompt(t *testing.T, s string) {
	t.Helper()

	if !strings.HasPrefix(s, availableSkillsStart) {
		t.Fatalf("expected available-skills standalone prompt root, got:\n%s", s)
	}
	if strings.Contains(s, skillsPromptStart) {
		t.Fatalf("did not expect combined wrapper in standalone available prompt:\n%s", s)
	}
	if strings.Contains(s, activeSkillsStart) {
		t.Fatalf("did not expect active section in standalone available prompt:\n%s", s)
	}
	if !strings.Contains(s, availableSkillsEnd) {
		t.Fatalf("missing available-skills end delimiter:\n%s", s)
	}
}

func assertStandaloneActivePrompt(t *testing.T, s string) {
	t.Helper()

	if !strings.HasPrefix(s, activeSkillsStart) {
		t.Fatalf("expected active-skills standalone prompt root, got:\n%s", s)
	}
	if strings.Contains(s, skillsPromptStart) {
		t.Fatalf("did not expect combined wrapper in standalone active prompt:\n%s", s)
	}
	if strings.Contains(s, availableSkillsStart) {
		t.Fatalf("did not expect available section in standalone active prompt:\n%s", s)
	}
	if !strings.Contains(s, activeSkillsEnd) {
		t.Fatalf("missing active-skills end delimiter:\n%s", s)
	}
}

func assertWrappedSkillsPrompt(t *testing.T, s string) {
	t.Helper()

	if !strings.HasPrefix(s, skillsPromptStart) {
		t.Fatalf("expected combined wrapper start delimiter, got:\n%s", s)
	}
	if !strings.Contains(s, skillsPromptEnd) {
		t.Fatalf("expected combined wrapper end delimiter, got:\n%s", s)
	}
	if !strings.Contains(s, availableSkillsStart) || !strings.Contains(s, availableSkillsEnd) {
		t.Fatalf("expected combined wrapper to contain available section, got:\n%s", s)
	}
	if !strings.Contains(s, activeSkillsStart) || !strings.Contains(s, activeSkillsEnd) {
		t.Fatalf("expected combined wrapper to contain active section, got:\n%s", s)
	}
}

func assertFirstRecordNotPrefixedBySeparator(t *testing.T, s, start, end string, isActiveSkill bool) {
	t.Helper()

	body := mustExtractPromptBlock(t, s, start, end)
	first := firstNonEmptyLine(body)
	if first == "" || first == nonePromptString {
		return
	}
	if isActiveSkill && first == nextActiveSkillsSeparator {
		t.Fatalf("unexpected leading separator after %s:\n%s", start, s)
	} else if first == nextAvailableSkillsSeparator {
		t.Fatalf("unexpected leading separator after %s:\n%s", start, s)
	}

	if !strings.HasPrefix(first, "name: ") {
		t.Fatalf("expected first content line after %s to begin with %q, got %q\nprompt=%s", start, "name: ", first, s)
	}
}

func mustParseSkillsPromptDocument(t *testing.T, s string) parsedSkillsPrompt {
	t.Helper()

	if strings.Contains(s, skillsPromptStart) {
		s = mustExtractPromptBlock(t, s, skillsPromptStart, skillsPromptEnd)
	}

	var doc parsedSkillsPrompt
	if strings.Contains(s, availableSkillsStart) {
		av := mustParseAvailableSkillsPrompt(t, s)
		doc.Available = &av
	}
	if strings.Contains(s, activeSkillsStart) {
		act := mustParseActiveSkillsPrompt(t, s)
		doc.Active = &act
	}
	return doc
}

func mustParseAvailableSkillsPrompt(t *testing.T, s string) parsedAvailablePrompt {
	t.Helper()

	body := mustExtractPromptBlock(t, s, availableSkillsStart, availableSkillsEnd)
	if strings.TrimSpace(body) == "" || strings.TrimSpace(body) == nonePromptString {
		return parsedAvailablePrompt{}
	}

	recs := splitPromptRecords(body, false)
	out := make([]parsedAvailableSkill, 0, len(recs))

	for _, rec := range recs {
		lines := strings.Split(strings.TrimRight(rec, "\r\n"), "\n")
		var item parsedAvailableSkill

		for _, line := range lines {
			switch {
			case strings.HasPrefix(line, "name: "):
				item.Name = strings.TrimPrefix(line, "name: ")
			case strings.HasPrefix(line, "location: "):
				item.Location = strings.TrimPrefix(line, "location: ")
			case strings.HasPrefix(line, "description: "):
				item.Description = strings.TrimPrefix(line, "description: ")
			default:
				t.Fatalf("unexpected available-skill line %q in record:\n%s", line, rec)
			}
		}

		if item.Name == "" {
			t.Fatalf("available-skill record missing name:\n%s", rec)
		}

		out = append(out, item)
	}

	return parsedAvailablePrompt{Skills: out}
}

func mustParseActiveSkillsPrompt(t *testing.T, s string) parsedActivePrompt {
	t.Helper()

	body := mustExtractPromptBlock(t, s, activeSkillsStart, activeSkillsEnd)
	if strings.TrimSpace(body) == "" || strings.TrimSpace(body) == nonePromptString {
		return parsedActivePrompt{}
	}

	recs := splitPromptRecords(body, true)
	out := make([]parsedActiveSkill, 0, len(recs))

	for _, rec := range recs {
		lines := strings.Split(strings.TrimRight(rec, "\r\n"), "\n")
		if len(lines) < 2 {
			t.Fatalf("active-skill record too short:\n%s", rec)
		}
		if !strings.HasPrefix(lines[0], "name: ") {
			t.Fatalf("active-skill record missing name header:\n%s", rec)
		}
		if lines[1] != "body:" {
			t.Fatalf("active-skill record missing body header:\n%s", rec)
		}

		item := parsedActiveSkill{
			Name: strings.TrimPrefix(lines[0], "name: "),
		}
		if len(lines) > 2 {
			item.Body = strings.Join(lines[2:], "\n")
		}

		out = append(out, item)
	}

	return parsedActivePrompt{Skills: out}
}

func mustExtractPromptBlock(t *testing.T, s, start, end string) string {
	t.Helper()

	startIdx := strings.Index(s, start)
	if startIdx < 0 {
		t.Fatalf("missing start delimiter %q in prompt:\n%s", start, s)
	}
	startIdx += len(start)

	if startIdx < len(s) && s[startIdx] == '\n' {
		startIdx++
	}

	rest := s[startIdx:]
	before, _, ok := strings.Cut(rest, end)
	if !ok {
		t.Fatalf("missing end delimiter %q in prompt:\n%s", end, s)
	}

	return strings.TrimRight(before, "\r\n")
}

func splitPromptRecords(body string, isActiveSkill bool) []string {
	body = strings.TrimRight(body, "\r\n")
	if body == "" {
		return nil
	}

	lines := strings.Split(body, "\n")
	recs := make([]string, 0, 1)
	cur := make([]string, 0, len(lines))

	flush := func() {
		if len(cur) == 0 {
			return
		}
		rec := strings.Join(cur, "\n")
		if strings.TrimSpace(rec) != "" {
			recs = append(recs, rec)
		}
		cur = nil
	}

	for _, line := range lines {
		if (isActiveSkill && line == nextActiveSkillsSeparator) || line == nextAvailableSkillsSeparator {
			flush()
			continue
		}

		cur = append(cur, line)
	}
	flush()

	return recs
}

func firstNonEmptyLine(s string) string {
	for line := range strings.SplitSeq(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}
