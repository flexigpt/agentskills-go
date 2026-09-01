package integration

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/flexigpt/agentskills-go/document"
	"github.com/flexigpt/agentskills-go/provider"
	"github.com/flexigpt/agentskills-go/runtime"
	"github.com/flexigpt/agentskills-go/runtime/spec"
)

const (
	fakeStr    = "fake"
	dupStr     = "dup"
	missingStr = "missing"
)

func TestNew_RuntimeOptionsValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    []runtime.Option
		wantErr string
	}{
		{
			name:    "nil provider in runtime.WithProvider",
			opts:    []runtime.Option{runtime.WithProvider(nil)},
			wantErr: "nil provider",
		},
		{
			name:    "provider.Type empty",
			opts:    []runtime.Option{runtime.WithProvider(&fakeProvider{typ: ""})},
			wantErr: "provider.Type() returned an invalid type",
		},
		{
			name: "duplicate provider type via runtime.WithProvider",
			opts: []runtime.Option{
				runtime.WithProvider(&fakeProvider{typ: dupStr}),
				runtime.WithProvider(&fakeProvider{typ: dupStr}),
			},
			wantErr: "duplicate provider type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rt, err := runtime.New(tt.opts...)
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
		runtime.WithProvider(&fakeProvider{typ: "z"}),
		runtime.WithProvider(&fakeProvider{typ: "a"}),
		runtime.WithProvider(&fakeProvider{typ: "m"}),
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

	rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
	var nilCtx context.Context

	_, err := rt.AddSkill(nilCtx, provider.SkillDef{Type: "p", Name: "a", Location: "/a"})
	if !errors.Is(err, spec.ErrInvalidRuntimeArgument) {
		t.Fatalf("AddSkill(nil ctx): expected ErrInvalidRuntimeArgument, got %v", err)
	}

	_, err = rt.RemoveSkill(nilCtx, provider.SkillDef{Type: "p", Name: "a", Location: "/a"})
	if !errors.Is(err, spec.ErrInvalidRuntimeArgument) {
		t.Fatalf("RemoveSkill(nil ctx): expected ErrInvalidRuntimeArgument, got %v", err)
	}

	_, _, err = rt.NewSession(nilCtx)
	if !errors.Is(err, spec.ErrInvalidRuntimeArgument) {
		t.Fatalf("NewSession(nil ctx): expected ErrInvalidRuntimeArgument, got %v", err)
	}

	err = rt.CloseSession(nilCtx, "sid")
	if !errors.Is(err, spec.ErrInvalidRuntimeArgument) {
		t.Fatalf("CloseSession(nil ctx): expected ErrInvalidRuntimeArgument, got %v", err)
	}

	_, err = rt.SkillsPrompt(nilCtx, nil)
	if !errors.Is(err, spec.ErrInvalidRuntimeArgument) {
		t.Fatalf("SkillsPrompt(nil ctx): expected ErrInvalidRuntimeArgument, got %v", err)
	}

	_, err = rt.ListSkills(nilCtx, nil)
	if !errors.Is(err, spec.ErrInvalidRuntimeArgument) {
		t.Fatalf("ListSkills(nil ctx): expected ErrInvalidRuntimeArgument, got %v", err)
	}

	_, err = rt.NewSessionRegistry(nilCtx, "sid")
	if !errors.Is(err, spec.ErrInvalidRuntimeArgument) {
		t.Fatalf("NewSessionRegistry(nil ctx): expected ErrInvalidRuntimeArgument, got %v", err)
	}
}

func TestRuntime_AddSkill_RemoveSkill_Errors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	pCanon := &fakeProvider{
		typ: "p",
		indexFn: func(ctx context.Context, def provider.SkillDef) (provider.ProviderSkillIndexRecord, error) {
			return provider.ProviderSkillIndexRecord{
				Key: provider.ProviderSkillKey{
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
				rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.AddSkill(ctx, provider.SkillDef{})
				return err
			},
			wantErr: spec.ErrInvalidRuntimeArgument,
		},
		{
			name: "AddSkill invalid argument (leading/trailing whitespace is rejected)",
			do: func() error {
				rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.AddSkill(ctx, provider.SkillDef{Type: " p", Name: "a", Location: "/a"})
				return err
			},
			wantErr: spec.ErrInvalidRuntimeArgument,
		},
		{
			name: "AddSkill provider not found",
			do: func() error {
				rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.AddSkill(ctx, provider.SkillDef{
					Type:     missingStr,
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
				rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.RemoveSkill(ctx, provider.SkillDef{
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
				rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
				def := provider.SkillDef{Type: "p", Name: dupStr, Location: "/d"}
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
				rt := mustNewRuntime(t, runtime.WithProvider(pCanon))
				orig := provider.SkillDef{Type: "p", Name: "s1", Location: "/p1"}
				if _, err := rt.AddSkill(ctx, orig); err != nil {
					return err
				}
				_, err := rt.RemoveSkill(ctx, provider.SkillDef{Type: "p", Name: "s1", Location: "NORM:/p1"})
				return err
			},
			wantErr: spec.ErrSkillNotFound,
		},
		{
			name: "nil runtime receiver returns invalid argument",
			do: func() error {
				var nilRT *runtime.Runtime
				_, err := nilRT.AddSkill(ctx, provider.SkillDef{Type: "p", Name: "x", Location: "/x"})
				return err
			},
			wantErr: spec.ErrInvalidRuntimeArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.do()
			if !strings.Contains(err.Error(), tt.wantErr.Error()) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestRuntime_NewSession_InitialActiveSkills_ReturnsExactlyProvidedDefs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
	def := provider.SkillDef{Type: "p", Name: "s1", Location: "/pw1"}
	_ = mustAddSkill(t, rt, ctx, def)

	_, active := mustNewSession(t, rt, ctx, runtime.WithSessionActiveSkills([]provider.SkillDef{def}))
	if len(active) != 1 || active[0] != def {
		t.Fatalf("expected active defs [%+v], got %+v", def, active)
	}
}

func TestRuntime_NewSession_InitialActiveSkills_DuplicateDefErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
	def := provider.SkillDef{Type: "p", Name: "s1", Location: "/pz1"}
	_ = mustAddSkill(t, rt, ctx, def)

	_, _, err := rt.NewSession(ctx, runtime.WithSessionActiveSkills([]provider.SkillDef{def, def}))
	if !errors.Is(err, spec.ErrInvalidRuntimeArgument) {
		t.Fatalf("expected ErrInvalidRuntimeArgument, got %v", err)
	}
}

func TestRuntime_NewSession_MaxActiveOverride_AppliesToInitialActiveSkills(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
	defA := provider.SkillDef{Type: "p", Name: "a", Location: "/a"}
	defB := provider.SkillDef{Type: "p", Name: "b", Location: "/b"}
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defB)

	_, _, err := rt.NewSession(ctx,
		runtime.WithSessionMaxActivePerSession(1),
		runtime.WithSessionActiveSkills([]provider.SkillDef{defA, defB}),
	)
	if !strings.Contains(err.Error(), "invalid argument") {
		t.Fatalf("expected ErrInvalidRuntimeArgument, got %v", err)
	}
}

func TestRuntime_NewSession_UnknownActiveDef_ReturnsSkillNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
	_, _, err := rt.NewSession(ctx,
		runtime.WithSessionActiveSkills([]provider.SkillDef{{Type: "p", Name: missingStr, Location: "/missing"}}),
	)
	if !errors.Is(err, spec.ErrSkillNotFound) {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestRuntime_ListSkills_ActivityAndSessionFilters(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))

	defA := provider.SkillDef{Type: "p", Name: "a", Location: "/a"}
	defB := provider.SkillDef{Type: "p", Name: "b", Location: "/b"}
	defC := provider.SkillDef{Type: "p", Name: "c", Location: "/c"}
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defB)
	_ = mustAddSkill(t, rt, ctx, defC)

	sid, _ := mustNewSession(t, rt, ctx, runtime.WithSessionActiveSkills([]provider.SkillDef{defA, defB}))
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	tests := []struct {
		name      string
		filter    *runtime.SkillListFilter
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
			filter:    &runtime.SkillListFilter{SessionID: sid, Activity: spec.SkillActivityAny},
			wantCount: 3,
		},
		{
			name:      "activity active with session => only active",
			filter:    &runtime.SkillListFilter{SessionID: sid, Activity: spec.SkillActivityActive},
			wantCount: 2,
		},
		{
			name:      "activity inactive with session => only inactive",
			filter:    &runtime.SkillListFilter{SessionID: sid, Activity: spec.SkillActivityInactive},
			wantCount: 1,
		},
		{
			name:      "activity inactive without session => treated like all",
			filter:    &runtime.SkillListFilter{Activity: spec.SkillActivityInactive},
			wantCount: 3,
		},
		{
			name:    "activity active without session => invalid argument",
			filter:  &runtime.SkillListFilter{Activity: spec.SkillActivityActive},
			wantErr: spec.ErrInvalidRuntimeArgument,
		},
		{
			name:    "invalid activity => invalid argument",
			filter:  &runtime.SkillListFilter{Activity: spec.SkillActivity("nope")},
			wantErr: spec.ErrInvalidRuntimeArgument,
		},
		{
			name: "session missing => ErrSessionNotFound",
			filter: &runtime.SkillListFilter{
				SessionID: spec.SessionID(missingStr),
				Activity:  spec.SkillActivityAny,
			},
			wantErr: spec.ErrSessionNotFound,
		},
		{
			name: "allowSkills applies + inactive: allow only A and C, but A is active => only C remains",
			filter: &runtime.SkillListFilter{
				SessionID:      sid,
				Activity:       spec.SkillActivityInactive,
				AllowSkills:    []provider.SkillDef{defA, defC},
				NamePrefix:     "",
				Types:          nil,
				LocationPrefix: "",
			},
			wantCount: 1,
		},
		{
			name:      "types filter",
			filter:    &runtime.SkillListFilter{Types: []string{"p"}},
			wantCount: 3,
		},
		{
			name:      "name prefix filter uses host def name",
			filter:    &runtime.SkillListFilter{NamePrefix: "b"},
			wantCount: 1,
		},
		{
			name:      "location prefix filter uses host def location",
			filter:    &runtime.SkillListFilter{LocationPrefix: "/b"},
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
		indexFn: func(ctx context.Context, def provider.SkillDef) (provider.ProviderSkillIndexRecord, error) {
			return provider.ProviderSkillIndexRecord{
				Key: provider.ProviderSkillKey{
					Type:     def.Type,
					Name:     def.Name,
					Location: "CANON:" + def.Location,
				},
				Description: "desc:" + def.Name,
			}, nil
		},
		loadBodyFn: func(ctx context.Context, key provider.ProviderSkillKey) (string, error) {
			return "BODY<" + key.Name + ">&", nil
		},
	}

	rt := mustNewRuntime(t,
		runtime.WithProvider(p),
	)

	defB := provider.SkillDef{Type: "p", Name: "b", Location: "/b"}
	defA := provider.SkillDef{Type: "p", Name: "a", Location: "/a"}
	defC := provider.SkillDef{Type: "p", Name: "c", Location: "/c"}
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

	sid, activeDefs := mustNewSession(t, rt, ctx, runtime.WithSessionActiveSkills([]provider.SkillDef{defA, defB}))
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })
	if len(activeDefs) != 2 || activeDefs[0] != defA || activeDefs[1] != defB {
		t.Fatalf("expected NewSession active defs order [A B], got %+v", activeDefs)
	}

	before1 := p.loadBodyCalls.Load()
	prompt2, err := rt.SkillsPrompt(
		ctx,
		&runtime.SkillFilter{SessionID: sid, Activity: spec.SkillActivityAny},
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
		&runtime.SkillFilter{SessionID: sid, Activity: spec.SkillActivityAny},
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
		&runtime.SkillFilter{SessionID: sid, Activity: spec.SkillActivityActive},
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
		&runtime.SkillFilter{SessionID: sid, Activity: spec.SkillActivityInactive},
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

	prompt5, err := rt.SkillsPrompt(ctx, &runtime.SkillFilter{
		SessionID:      sid,
		Activity:       spec.SkillActivityAny,
		AllowSkills:    []provider.SkillDef{defC},
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
	rt := mustNewRuntime(t, runtime.WithProvider(pa), runtime.WithProvider(pb))

	defA := provider.SkillDef{Type: "a", Name: "x", Location: "/same"}
	defB := provider.SkillDef{Type: "b", Name: "x", Location: "/same"}
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defB)

	promptAll, err := rt.SkillsPrompt(ctx, &runtime.SkillFilter{Activity: spec.SkillActivityAny})
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
		&runtime.SkillFilter{NamePrefix: pick, Activity: spec.SkillActivityAny},
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

	recs, err := rt.ListSkills(ctx, &runtime.SkillListFilter{NamePrefix: pick})
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
		indexFn: func(ctx context.Context, def provider.SkillDef) (provider.ProviderSkillIndexRecord, error) {
			return provider.ProviderSkillIndexRecord{
				Key: provider.ProviderSkillKey{
					Type:     def.Type,
					Name:     def.Name,
					Location: "CANON:" + def.Location,
				},
				Description: "desc:" + def.Name,
			}, nil
		},
	}

	rt := mustNewRuntime(t, runtime.WithProvider(p))
	def := provider.SkillDef{Type: "p", Name: "skill", Location: "/user/location"}
	_ = mustAddSkill(t, rt, ctx, def)

	promptUser, err := rt.SkillsPrompt(ctx, &runtime.SkillFilter{
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

	promptCanon, err := rt.SkillsPrompt(ctx, &runtime.SkillFilter{
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

	rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))
	def := provider.SkillDef{Type: "p", Name: "a", Location: "/a"}
	_ = mustAddSkill(t, rt, ctx, def)

	sid, _ := mustNewSession(t, rt, ctx, runtime.WithSessionActiveSkills([]provider.SkillDef{def}))
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	if _, err := rt.RemoveSkill(ctx, def); err != nil {
		t.Fatalf("RemoveSkill: %v", err)
	}

	_ = mustAddSkill(t, rt, ctx, def)

	promptOut, err := rt.SkillsPrompt(
		ctx,
		&runtime.SkillFilter{SessionID: sid, Activity: spec.SkillActivityAny},
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

	rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: "p"}))

	tests := []struct {
		name    string
		do      func() error
		wantErr error
	}{
		{
			name: "nil runtime receiver",
			do: func() error {
				var nilRT *runtime.Runtime
				_, err := nilRT.SkillsPrompt(ctx, nil)
				return err
			},
			wantErr: spec.ErrInvalidRuntimeArgument,
		},
		{
			name: "nil context",
			do: func() error {
				var nilCtx context.Context
				_, err := rt.SkillsPrompt(nilCtx, nil)
				return err
			},
			wantErr: spec.ErrInvalidRuntimeArgument,
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
					&runtime.SkillFilter{Activity: spec.SkillActivityActive},
				)
				return err
			},
			wantErr: spec.ErrInvalidRuntimeArgument,
		},
		{
			name: "invalid activity",
			do: func() error {
				_, err := rt.SkillsPrompt(
					ctx,
					&runtime.SkillFilter{Activity: spec.SkillActivity("bad")},
				)
				return err
			},
			wantErr: spec.ErrInvalidRuntimeArgument,
		},
		{
			name: "missing session id => ErrSessionNotFound",
			do: func() error {
				_, err := rt.SkillsPrompt(
					ctx,
					&runtime.SkillFilter{SessionID: missingStr, Activity: spec.SkillActivityAny},
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

	rt := mustNewRuntime(t, runtime.WithProvider(&fakeProvider{typ: fakeStr}))

	_, err := rt.NewSessionRegistry(t.Context(), missingStr)
	if !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got: %v", err)
	}
}

func TestRuntime_RenderSkill_SkillsPrompt_AndInsertFiltering(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	p := &runtimeTestProvider{typ: "p"}
	rt, err := runtime.New(runtime.WithProvider(p))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	instructionsDef := provider.SkillDef{Type: "p", Name: "instructions", Location: "/skills/instructions"}
	templateDef := provider.SkillDef{Type: "p", Name: "template", Location: "/skills/template"}

	if _, err := rt.AddSkill(ctx, instructionsDef); err != nil {
		t.Fatalf("AddSkill(instructions): %v", err)
	}
	if _, err := rt.AddSkill(ctx, templateDef); err != nil {
		t.Fatalf("AddSkill(template): %v", err)
	}

	prompt, err := rt.SkillsPrompt(ctx, &runtime.SkillFilter{
		Types:       []string{" p ", "p", ""},
		AllowSkills: []provider.SkillDef{instructionsDef, instructionsDef},
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

	recs, err := rt.ListSkills(ctx, &runtime.SkillListFilter{
		Types:       []string{" p ", "p", ""},
		AllowSkills: []provider.SkillDef{templateDef, templateDef},
		Inserts:     []document.SkillInsert{document.SkillInsertUserMessage, document.SkillInsertUserMessage},
	})
	if err != nil {
		t.Fatalf("ListSkills(insert filter): %v", err)
	}
	if len(recs) != 1 || recs[0].Def != templateDef {
		t.Fatalf("expected only user-message skill in insert-filtered list, got %+v", recs)
	}

	rendered, err := rt.RenderSkill(ctx, runtime.RenderSkillParams{
		Def:       templateDef,
		Arguments: map[string]string{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("RenderSkill: %v", err)
	}
	if rendered.Name != "template" {
		t.Fatalf("expected fallback name from key, got %q", rendered.Name)
	}
	if rendered.Insert != document.SkillInsertUserMessage {
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
		[]document.SkillArgument{{Name: "name", Default: "World"}, {Name: "mood", Default: "calm"}},
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

	rt, err := runtime.New(runtime.WithProvider(&runtimeTestProvider{typ: "p"}))
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

	rt, err := runtime.New(runtime.WithProvider(&runtimeTestProvider{typ: "p"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	validDef := provider.SkillDef{Type: "p", Name: "template", Location: "/skills/template"}
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
				var nilRT *runtime.Runtime
				_, err := nilRT.RenderSkill(ctx, runtime.RenderSkillParams{Def: validDef})
				return err
			},
			want: spec.ErrInvalidRuntimeArgument,
		},
		{
			name: "nil context",
			do: func() error {
				var nilCtx context.Context
				_, err := rt.RenderSkill(nilCtx, runtime.RenderSkillParams{Def: validDef})
				return err
			},
			want: spec.ErrInvalidRuntimeArgument,
		},
		{
			name: "unknown skill def",
			do: func() error {
				_, err := rt.RenderSkill(
					ctx,
					runtime.RenderSkillParams{
						Def: provider.SkillDef{Type: "p", Name: "missing", Location: "/skills/missing"},
					},
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
					runtime.RenderSkillParams{
						Def: provider.SkillDef{Type: " p", Name: "template", Location: "/skills/template"},
					},
				)
				return err
			},
			want: spec.ErrInvalidRuntimeArgument,
		},
		{
			name: "missing required fields are rejected",
			do: func() error {
				_, err := rt.RenderSkill(
					ctx,
					runtime.RenderSkillParams{
						Def: provider.SkillDef{Type: "p", Name: "", Location: "/skills/template"},
					},
				)
				return err
			},
			want: spec.ErrInvalidRuntimeArgument,
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
