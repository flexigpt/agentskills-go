package agentskills

import (
	"context"
	"encoding/xml"
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

	indexFn    func(context.Context, spec.SkillDef) (spec.ProviderSkillIndexRecord, error)
	loadBodyFn func(context.Context, spec.ProviderSkillKey) (string, error)
}

func (p *fakeProvider) Type() string { return p.typ }

func (p *fakeProvider) Index(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
	p.indexCalls.Add(1)
	if p.indexFn != nil {
		return p.indexFn(ctx, def)
	}
	return spec.ProviderSkillIndexRecord{
		Key:         spec.ProviderSkillKey(def),
		Description: "desc:" + def.Type + ":" + def.Name,
	}, nil
}

func (p *fakeProvider) LoadBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	p.loadBodyCalls.Add(1)
	if p.loadBodyFn != nil {
		return p.loadBodyFn(ctx, key)
	}
	// Include markup + '&' so we can detect CDATA vs escaping.
	return "BODY<" + key.Name + ">&", nil
}

func (p *fakeProvider) ReadResource(
	ctx context.Context,
	key spec.ProviderSkillKey,
	resourceLocation string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, spec.ErrInvalidArgument
}

func (p *fakeProvider) RunScript(
	ctx context.Context,
	key spec.ProviderSkillKey,
	scriptLocation string,
	args []string,
	env map[string]string,
	workDir string,
) (spec.RunScriptOut, error) {
	return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
}

func mustNewRuntime(t *testing.T, opts ...Option) *Runtime {
	t.Helper()
	rt, err := New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if rt == nil {
		t.Fatalf("New: got nil runtime")
	}
	return rt
}

func mustAddSkill(t *testing.T, rt *Runtime, ctx context.Context, def spec.SkillDef) spec.SkillRecord {
	t.Helper()
	rec, err := rt.AddSkill(ctx, def)
	if err != nil {
		t.Fatalf("AddSkill(%+v): %v", def, err)
	}
	if rec.Def != def {
		t.Fatalf("AddSkill: returned record.Def mismatch: got=%+v want=%+v", rec.Def, def)
	}
	return rec
}

func mustNewSession(
	t *testing.T,
	rt *Runtime,
	ctx context.Context,
	opts ...SessionOption,
) (spec.SessionID, []spec.SkillDef) {
	t.Helper()
	sid, active, err := rt.NewSession(ctx, opts...)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sid == "" {
		t.Fatalf("NewSession: got empty session id")
	}
	return sid, active
}

func xmlRootName(t *testing.T, s string) string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(s))
	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("xmlRootName token: %v\nxml=%s", err, s)
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local
		}
	}
}

type availableSkillsDoc struct {
	XMLName xml.Name `xml:"availableSkills"`
	Skills  []struct {
		Name        string `xml:"name"`
		Description string `xml:"description"`
		Location    string `xml:"location"`
	} `xml:"skill"`
}

type activeSkillsDoc struct {
	XMLName xml.Name `xml:"activeSkills"`
	Skills  []struct {
		Name string `xml:"name,attr"`
		Body string `xml:",chardata"`
	} `xml:"skill"`
}

type skillsPromptDoc struct {
	XMLName   xml.Name            `xml:"skillsPrompt"`
	Available *availableSkillsDoc `xml:"availableSkills"`
	Active    *activeSkillsDoc    `xml:"activeSkills"`
}

const availableSkillsRoot = "availableSkills"

func mustUnmarshalAvailable(t *testing.T, s string) availableSkillsDoc {
	t.Helper()
	var doc availableSkillsDoc
	if err := xml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("unmarshal availableSkills: %v\nxml=%s", err, s)
	}
	if doc.XMLName.Local != availableSkillsRoot {
		t.Fatalf("expected root %s, got %q", availableSkillsRoot, doc.XMLName.Local)
	}
	return doc
}

func mustUnmarshalActive(t *testing.T, s string) activeSkillsDoc {
	t.Helper()
	var doc activeSkillsDoc
	if err := xml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("unmarshal activeSkills: %v\nxml=%s", err, s)
	}
	if doc.XMLName.Local != "activeSkills" {
		t.Fatalf("expected root activeSkills, got %q", doc.XMLName.Local)
	}
	return doc
}

func mustUnmarshalPrompt(t *testing.T, s string) skillsPromptDoc {
	t.Helper()
	var doc skillsPromptDoc
	if err := xml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("unmarshal skillsPrompt: %v\nxml=%s", err, s)
	}
	if doc.XMLName.Local != "skillsPrompt" {
		t.Fatalf("expected root skillsPrompt, got %q", doc.XMLName.Local)
	}
	return doc
}

func TestNew_RuntimeOptionsValidation(t *testing.T) {
	t.Parallel()

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
				WithProviders(map[string]spec.SkillProvider{"": &fakeProvider{typ: "ok"}}),
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
			name: "duplicate provider type across WithProvider and WithProviders",
			opts: []Option{
				WithProvider(&fakeProvider{typ: "x"}),
				WithProviders(map[string]spec.SkillProvider{"x": &fakeProvider{typ: "x"}}),
			},
			wantErr: "duplicate provider type",
		},
		{
			name: "logger nil allowed and normalized",
			opts: []Option{
				WithLogger(nil),
				WithProvider(&fakeProvider{typ: "ok"}),
			},
			wantErr: "",
		},
		{
			name: "WithProviders input is snapshotted (caller mutation after WithProviders does not affect runtime)",
			opts: func() []Option {
				pOK := &fakeProvider{typ: "ok"}
				m := map[string]spec.SkillProvider{"ok": pOK}
				o := WithProviders(m)
				delete(m, "ok") // should not affect if WithProviders snapshots
				return []Option{o}
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

			if strings.Contains(tt.name, "snapshotted") {
				got := rt.ProviderTypes()
				found := false
				for _, v := range got {
					if v == "ok" {
						found = true
					}
				}
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
		WithProvider(&fakeProvider{typ: "z"}),
		WithProvider(&fakeProvider{typ: "a"}),
		WithProvider(&fakeProvider{typ: "m"}),
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

	rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
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

	_, err = rt.SkillsPromptXML(nilCtx, nil)
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("SkillsPromptXML(nil ctx): expected ErrInvalidArgument, got %v", err)
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
				rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.AddSkill(ctx, spec.SkillDef{})
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "AddSkill invalid argument (leading/trailing whitespace is rejected)",
			do: func() error {
				rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
				_, err := rt.AddSkill(ctx, spec.SkillDef{Type: " p", Name: "a", Location: "/a"})
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "AddSkill provider not found",
			do: func() error {
				rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
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
				rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
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
				rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
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
				rt := mustNewRuntime(t, WithProvider(pCanon))
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
				var nilRT *Runtime
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
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
	def := spec.SkillDef{Type: "p", Name: "s1", Location: "/p1"}
	_ = mustAddSkill(t, rt, ctx, def)

	_, active := mustNewSession(t, rt, ctx, WithSessionActiveSkills([]spec.SkillDef{def}))
	if len(active) != 1 || active[0] != def {
		t.Fatalf("expected active defs [%+v], got %+v", def, active)
	}
}

func TestRuntime_NewSession_InitialActiveSkills_DuplicateDefErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
	def := spec.SkillDef{Type: "p", Name: "s1", Location: "/p1"}
	_ = mustAddSkill(t, rt, ctx, def)

	_, _, err := rt.NewSession(ctx, WithSessionActiveSkills([]spec.SkillDef{def, def}))
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestRuntime_NewSession_MaxActiveOverride_AppliesToInitialActiveSkills(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
	defA := spec.SkillDef{Type: "p", Name: "a", Location: "/a"}
	defB := spec.SkillDef{Type: "p", Name: "b", Location: "/b"}
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defB)

	_, _, err := rt.NewSession(ctx,
		WithSessionMaxActivePerSession(1),
		WithSessionActiveSkills([]spec.SkillDef{defA, defB}),
	)
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestRuntime_NewSession_UnknownActiveDef_ReturnsSkillNotFound(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
	// Provider exists, but skill is not in catalog.
	_, _, err := rt.NewSession(ctx,
		WithSessionActiveSkills([]spec.SkillDef{{Type: "p", Name: "missing", Location: "/missing"}}),
	)
	if !errors.Is(err, spec.ErrSkillNotFound) {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestRuntime_ListSkills_ActivityAndSessionFilters(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))

	defA := spec.SkillDef{Type: "p", Name: "a", Location: "/a"}
	defB := spec.SkillDef{Type: "p", Name: "b", Location: "/b"}
	defC := spec.SkillDef{Type: "p", Name: "c", Location: "/c"}
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defB)
	_ = mustAddSkill(t, rt, ctx, defC)

	sid, _ := mustNewSession(t, rt, ctx, WithSessionActiveSkills([]spec.SkillDef{defA, defB}))
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	tests := []struct {
		name      string
		filter    *SkillListFilter
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
			filter:    &SkillListFilter{SessionID: sid, Activity: SkillActivityAny},
			wantCount: 3,
		},
		{
			name:      "activity active with session => only active",
			filter:    &SkillListFilter{SessionID: sid, Activity: SkillActivityActive},
			wantCount: 2,
		},
		{
			name:      "activity inactive with session => only inactive",
			filter:    &SkillListFilter{SessionID: sid, Activity: SkillActivityInactive},
			wantCount: 1,
		},
		{
			name:      "activity inactive without session => treated like all",
			filter:    &SkillListFilter{Activity: SkillActivityInactive},
			wantCount: 3,
		},
		{
			name:    "activity active without session => invalid argument",
			filter:  &SkillListFilter{Activity: SkillActivityActive},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name:    "invalid activity => invalid argument",
			filter:  &SkillListFilter{Activity: SkillActivity("nope")},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name:    "session missing => ErrSessionNotFound",
			filter:  &SkillListFilter{SessionID: spec.SessionID("missing"), Activity: SkillActivityAny},
			wantErr: spec.ErrSessionNotFound,
		},
		{
			name: "allowSkills applies + inactive: allow only A and C, but A is active => only C remains",
			filter: &SkillListFilter{
				SessionID:      sid,
				Activity:       SkillActivityInactive,
				AllowSkills:    []spec.SkillDef{defA, defC},
				NamePrefix:     "",
				Types:          nil,
				LocationPrefix: "",
			},
			wantCount: 1,
		},
		{
			name:      "types filter",
			filter:    &SkillListFilter{Types: []string{"p"}},
			wantCount: 3,
		},
		{
			name:      "name prefix filter uses host def name",
			filter:    &SkillListFilter{NamePrefix: "b"},
			wantCount: 1,
		},
		{
			name:      "location prefix filter uses host def location",
			filter:    &SkillListFilter{LocationPrefix: "/b"},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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

func TestRuntime_SkillsPromptXML_RootsSectionsCDATAAndFiltering(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	p := &fakeProvider{
		typ: "p",
		indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
			// Canonicalize internally (must NOT leak via prompt handle location or lifecycle record.Def).
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
			// Ensure body contains markup + '&' so CDATA is detectable.
			return "BODY<" + key.Name + ">&", nil
		},
	}

	rt := mustNewRuntime(t,
		WithLogger(slog.New(slog.DiscardHandler)),
		WithProvider(p),
	)

	// Add out of order to validate availableSkills sorting.
	defB := spec.SkillDef{Type: "p", Name: "b", Location: "/b"}
	defA := spec.SkillDef{Type: "p", Name: "a", Location: "/a"}
	defC := spec.SkillDef{Type: "p", Name: "c", Location: "/c"}
	_ = mustAddSkill(t, rt, ctx, defB)
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defC)

	// No session, default (nil filter) => availableSkills root only, sorted by name.
	xml1, err := rt.SkillsPromptXML(ctx, nil)
	if err != nil {
		t.Fatalf("SkillsPromptXML: %v", err)
	}
	if gotRoot := xmlRootName(t, xml1); gotRoot != availableSkillsRoot {
		t.Fatalf("expected root availableSkills, got %q\nxml=%s", gotRoot, xml1)
	}
	av1 := mustUnmarshalAvailable(t, xml1)
	if len(av1.Skills) != 3 {
		t.Fatalf("expected 3 available skills, got %d", len(av1.Skills))
	}
	gotNames := []string{av1.Skills[0].Name, av1.Skills[1].Name, av1.Skills[2].Name}
	if strings.Join(gotNames, ",") != "a,b,c" {
		t.Fatalf("expected available sorted by name a,b,c; got %v\nxml=%s", gotNames, xml1)
	}
	// Location MUST be user-provided (not canonicalized).
	for _, it := range av1.Skills {
		if strings.HasPrefix(it.Location, "CANON:") {
			t.Fatalf(
				"expected prompt location to be user-provided, got %q (should not start with CANON:)\nxml=%s",
				it.Location,
				xml1,
			)
		}
	}

	// Create session with initial active skills A then B (order should be preserved).
	sid, activeDefs := mustNewSession(t, rt, ctx, WithSessionActiveSkills([]spec.SkillDef{defA, defB}))
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })
	if len(activeDefs) != 2 || activeDefs[0] != defA || activeDefs[1] != defB {
		t.Fatalf("expected NewSession active defs order [A B], got %+v", activeDefs)
	}

	// ActivityAny + session => skillsPrompt wrapper with both sections.
	before1 := p.loadBodyCalls.Load()
	xml2, err := rt.SkillsPromptXML(ctx, &SkillFilter{SessionID: sid, Activity: SkillActivityAny})
	if err != nil {
		t.Fatalf("SkillsPromptXML(any+session): %v", err)
	}
	after1 := p.loadBodyCalls.Load()

	if gotRoot := xmlRootName(t, xml2); gotRoot != "skillsPrompt" {
		t.Fatalf("expected root skillsPrompt, got %q\nxml=%s", gotRoot, xml2)
	}
	if !strings.Contains(xml2, "<![CDATA[") {
		t.Fatalf("expected CDATA in activeSkills output\nxml=%s", xml2)
	}
	// In CDATA, bodies should appear unescaped.
	if !strings.Contains(xml2, "BODY<a>&") || !strings.Contains(xml2, "BODY<b>&") {
		t.Fatalf("expected raw (unescaped) body in CDATA\nxml=%s", xml2)
	}

	// Calling prompt again should not trigger extra LoadBody calls (catalog cache).
	xml2b, err := rt.SkillsPromptXML(ctx, &SkillFilter{SessionID: sid, Activity: SkillActivityAny})
	if err != nil {
		t.Fatalf("SkillsPromptXML(any+session) again: %v", err)
	}
	_ = xml2b
	after2 := p.loadBodyCalls.Load()
	if after2 != after1 {
		t.Fatalf(
			"expected LoadBody call count not to increase on second prompt call, got before=%d after1=%d after2=%d",
			before1,
			after1,
			after2,
		)
	}

	doc := mustUnmarshalPrompt(t, xml2)
	if doc.Active == nil || doc.Available == nil {
		t.Fatalf("expected both active and available sections present\nxml=%s", xml2)
	}

	// Active should be A then B.
	if len(doc.Active.Skills) != 2 {
		t.Fatalf("expected 2 active skills, got %d\nxml=%s", len(doc.Active.Skills), xml2)
	}
	if doc.Active.Skills[0].Name != "a" || doc.Active.Skills[1].Name != "b" {
		t.Fatalf("expected active order [a b], got [%s %s]\nxml=%s",
			doc.Active.Skills[0].Name, doc.Active.Skills[1].Name, xml2)
	}
	if strings.TrimSpace(doc.Active.Skills[0].Body) != "BODY<a>&" {
		t.Fatalf("expected active body %q, got %q\nxml=%s", "BODY<a>&", doc.Active.Skills[0].Body, xml2)
	}

	// Available should be only inactive => C.
	if len(doc.Available.Skills) != 1 || doc.Available.Skills[0].Name != "c" {
		t.Fatalf("expected available(inactive) to contain only c, got %+v\nxml=%s", doc.Available.Skills, xml2)
	}

	// ActivityActive => activeSkills root only.
	xml3, err := rt.SkillsPromptXML(ctx, &SkillFilter{SessionID: sid, Activity: SkillActivityActive})
	if err != nil {
		t.Fatalf("SkillsPromptXML(active): %v", err)
	}
	if gotRoot := xmlRootName(t, xml3); gotRoot != "activeSkills" {
		t.Fatalf("expected root activeSkills, got %q\nxml=%s", gotRoot, xml3)
	}
	act3 := mustUnmarshalActive(t, xml3)
	if len(act3.Skills) != 2 {
		t.Fatalf("expected 2 active skills, got %d\nxml=%s", len(act3.Skills), xml3)
	}

	// ActivityInactive => availableSkills root only (inactive skills).
	xml4, err := rt.SkillsPromptXML(ctx, &SkillFilter{SessionID: sid, Activity: SkillActivityInactive})
	if err != nil {
		t.Fatalf("SkillsPromptXML(inactive): %v", err)
	}
	if gotRoot := xmlRootName(t, xml4); gotRoot != availableSkillsRoot {
		t.Fatalf("expected root availableSkills, got %q\nxml=%s", gotRoot, xml4)
	}
	av4 := mustUnmarshalAvailable(t, xml4)
	if len(av4.Skills) != 1 || av4.Skills[0].Name != "c" {
		t.Fatalf("expected only c inactive, got %+v\nxml=%s", av4.Skills, xml4)
	}

	// AllowSkills filter should apply to both active and available sections.
	// Allow only C (inactive). Active should become empty, available should contain only C.
	xml5, err := rt.SkillsPromptXML(ctx, &SkillFilter{
		SessionID:      sid,
		Activity:       SkillActivityAny,
		AllowSkills:    []spec.SkillDef{defC},
		NamePrefix:     "",
		Types:          nil,
		LocationPrefix: "",
	})
	if err != nil {
		t.Fatalf("SkillsPromptXML(allowSkills): %v", err)
	}
	doc5 := mustUnmarshalPrompt(t, xml5)
	if doc5.Active == nil || doc5.Available == nil {
		t.Fatalf("expected both sections in wrapper\nxml=%s", xml5)
	}
	if len(doc5.Active.Skills) != 0 {
		t.Fatalf(
			"expected 0 active skills after allowSkills restriction, got %d\nxml=%s",
			len(doc5.Active.Skills),
			xml5,
		)
	}
	if len(doc5.Available.Skills) != 1 || doc5.Available.Skills[0].Name != "c" {
		t.Fatalf("expected available to contain only c after allowSkills restriction, got %+v\nxml=%s",
			doc5.Available.Skills, xml5)
	}
}

func TestRuntime_SkillsPromptXML_NamePrefixIsLLMHandleNotHostName(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	pa := &fakeProvider{typ: "a"}
	pb := &fakeProvider{typ: "b"}
	rt := mustNewRuntime(t, WithProvider(pa), WithProvider(pb))

	// Two skills with the same host name/location but different types must disambiguate at the LLM handle layer.
	defA := spec.SkillDef{Type: "a", Name: "x", Location: "/same"}
	defB := spec.SkillDef{Type: "b", Name: "x", Location: "/same"}
	_ = mustAddSkill(t, rt, ctx, defA)
	_ = mustAddSkill(t, rt, ctx, defB)

	xmlAll, err := rt.SkillsPromptXML(ctx, &SkillFilter{Activity: SkillActivityAny})
	if err != nil {
		t.Fatalf("SkillsPromptXML: %v", err)
	}
	av := mustUnmarshalAvailable(t, xmlAll)
	if len(av.Skills) != 2 {
		t.Fatalf("expected 2 available skills, got %d\nxml=%s", len(av.Skills), xmlAll)
	}

	name1 := av.Skills[0].Name
	name2 := av.Skills[1].Name
	if name1 == name2 {
		t.Fatalf("expected distinct LLM-visible handle names, got both=%q\nxml=%s", name1, xmlAll)
	}

	// Pick a name that is NOT equal to the host name "x" if possible,
	// so we can prove that ListSkills (host filter) uses host name, not LLM handle name.
	pick := name1
	if pick == "x" && name2 != "x" {
		pick = name2
	}

	// Prompt NamePrefix uses LLM handle name.
	xmlOne, err := rt.SkillsPromptXML(ctx, &SkillFilter{NamePrefix: pick, Activity: SkillActivityAny})
	if err != nil {
		t.Fatalf("SkillsPromptXML(NamePrefix=%q): %v", pick, err)
	}
	avOne := mustUnmarshalAvailable(t, xmlOne)
	if len(avOne.Skills) != 1 {
		t.Fatalf("expected 1 available skill for NamePrefix=%q, got %d\nxml=%s", pick, len(avOne.Skills), xmlOne)
	}
	if !strings.HasPrefix(avOne.Skills[0].Name, pick) {
		t.Fatalf("expected available skill name %q to have prefix %q\nxml=%s", avOne.Skills[0].Name, pick, xmlOne)
	}

	// Host listing NamePrefix uses host def name; using the LLM handle prefix should produce 0
	// whenever the handle differs from "x".
	recs, err := rt.ListSkills(ctx, &SkillListFilter{NamePrefix: pick})
	if err != nil {
		t.Fatalf("ListSkills(NamePrefix=%q): %v", pick, err)
	}
	if pick != "x" && len(recs) != 0 {
		t.Fatalf("expected 0 host records for NamePrefix=%q (LLM handle), got %d: %+v", pick, len(recs), recs)
	}
}

func TestRuntime_RemoveSkill_PrunesFromAllSessions_ReAddDoesNotResurrectActive(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))
	def := spec.SkillDef{Type: "p", Name: "a", Location: "/a"}
	_ = mustAddSkill(t, rt, ctx, def)

	sid, _ := mustNewSession(t, rt, ctx, WithSessionActiveSkills([]spec.SkillDef{def}))
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	// Remove skill; this should prune from all sessions.
	if _, err := rt.RemoveSkill(ctx, def); err != nil {
		t.Fatalf("RemoveSkill: %v", err)
	}

	// Add it back.
	_ = mustAddSkill(t, rt, ctx, def)

	// If prune didn't happen, the session would still consider it active after re-add.
	xmlOut, err := rt.SkillsPromptXML(ctx, &SkillFilter{SessionID: sid, Activity: SkillActivityAny})
	if err != nil {
		t.Fatalf("SkillsPromptXML: %v", err)
	}
	doc := mustUnmarshalPrompt(t, xmlOut)

	if doc.Active == nil || doc.Available == nil {
		t.Fatalf("expected wrapper to contain both sections\nxml=%s", xmlOut)
	}
	if len(doc.Active.Skills) != 0 {
		t.Fatalf("expected 0 active skills after remove+readd, got %d\nxml=%s", len(doc.Active.Skills), xmlOut)
	}
	if len(doc.Available.Skills) != 1 {
		t.Fatalf("expected 1 available skill after remove+readd, got %d\nxml=%s", len(doc.Available.Skills), xmlOut)
	}
}

func TestRuntime_SkillsPromptXML_Errors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "p"}))

	tests := []struct {
		name    string
		do      func() error
		wantErr error
	}{
		{
			name: "nil runtime receiver",
			do: func() error {
				var nilRT *Runtime
				_, err := nilRT.SkillsPromptXML(ctx, nil)
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "nil context",
			do: func() error {
				var nilCtx context.Context
				_, err := rt.SkillsPromptXML(nilCtx, nil)
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "context canceled",
			do: func() error {
				cctx, ccancel := context.WithCancel(ctx)
				ccancel()
				_, err := rt.SkillsPromptXML(cctx, nil)
				return err
			},
			wantErr: context.Canceled,
		},
		{
			name: "activity active requires session",
			do: func() error {
				_, err := rt.SkillsPromptXML(ctx, &SkillFilter{Activity: SkillActivityActive})
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "invalid activity",
			do: func() error {
				_, err := rt.SkillsPromptXML(ctx, &SkillFilter{Activity: SkillActivity("bad")})
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "missing session id => ErrSessionNotFound",
			do: func() error {
				_, err := rt.SkillsPromptXML(ctx, &SkillFilter{SessionID: "missing", Activity: SkillActivityAny})
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

func TestNormalizeSkillsPromptFilter(t *testing.T) {
	t.Parallel()

	in := &SkillFilter{
		Types: []string{"  a  ", "a", "", " b "},
		AllowSkills: []spec.SkillDef{
			{}, // dropped
			{Type: "t", Name: "n", Location: "l"},
			{Type: "t", Name: "n", Location: "l"}, // dedupe
			{Type: " ", Name: "n", Location: "l"}, // dropped
		},
		SessionID: "  sid  ",
		Activity:  "",
	}

	got := normalizeSkillsPromptFilter(in)

	// Types are trimmed + deduped (order not guaranteed).
	if len(got.Types) != 2 {
		t.Fatalf("expected 2 types, got %+v", got.Types)
	}
	hasA, hasB := false, false
	for _, tpe := range got.Types {
		if tpe == "a" {
			hasA = true
		}
		if tpe == "b" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Fatalf("unexpected types: %+v", got.Types)
	}

	if got.Activity != SkillActivityAny {
		t.Fatalf("expected default activity any, got %q", got.Activity)
	}
	if got.SessionID != "sid" {
		t.Fatalf("expected trimmed sessionID %q, got %q", "sid", got.SessionID)
	}
	if len(got.AllowSkills) != 1 {
		t.Fatalf("expected 1 allow skill, got %d: %+v", len(got.AllowSkills), got.AllowSkills)
	}
	if got.AllowSkills[0] != (spec.SkillDef{Type: "t", Name: "n", Location: "l"}) {
		t.Fatalf("unexpected allow skill: %+v", got.AllowSkills[0])
	}
}

func TestNormalizeSkillListFilter(t *testing.T) {
	t.Parallel()

	in := &SkillListFilter{
		Types: []string{"  a  ", "a", "", " b "},
		AllowSkills: []spec.SkillDef{
			{}, // dropped
			{Type: "t", Name: "n", Location: "l"},
			{Type: "t", Name: "n", Location: "l"}, // dedupe
			{Type: " ", Name: "n", Location: "l"}, // dropped
		},
		SessionID: "  sid  ",
		Activity:  "",
	}

	got := normalizeSkillListFilter(in)

	if len(got.Types) != 2 {
		t.Fatalf("expected 2 types, got %+v", got.Types)
	}
	hasA, hasB := false, false
	for _, tpe := range got.Types {
		if tpe == "a" {
			hasA = true
		}
		if tpe == "b" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Fatalf("unexpected types: %+v", got.Types)
	}

	if got.Activity != SkillActivityAny {
		t.Fatalf("expected default activity any, got %q", got.Activity)
	}
	if got.SessionID != "sid" {
		t.Fatalf("expected trimmed sessionID %q, got %q", "sid", got.SessionID)
	}
	if len(got.AllowSkills) != 1 {
		t.Fatalf("expected 1 allow skill, got %d: %+v", len(got.AllowSkills), got.AllowSkills)
	}
	if got.AllowSkills[0] != (spec.SkillDef{Type: "t", Name: "n", Location: "l"}) {
		t.Fatalf("unexpected allow skill: %+v", got.AllowSkills[0])
	}
}

func TestRuntime_NewSessionRegistry_UnknownSession(t *testing.T) {
	t.Parallel()

	rt := mustNewRuntime(t, WithProvider(&fakeProvider{typ: "fake"}))

	_, err := rt.NewSessionRegistry(t.Context(), "missing")
	if !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got: %v", err)
	}
}
