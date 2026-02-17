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

	indexFn    func(context.Context, spec.SkillKey) (spec.SkillRecord, error)
	loadBodyFn func(context.Context, spec.SkillKey) (string, error)
}

func (p *fakeProvider) Type() string { return p.typ }

func (p *fakeProvider) Index(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	p.indexCalls.Add(1)
	if p.indexFn != nil {
		return p.indexFn(ctx, key)
	}
	return spec.SkillRecord{Key: key, Description: "desc:" + key.Type + ":" + key.Name}, nil
}

func (p *fakeProvider) LoadBody(ctx context.Context, key spec.SkillKey) (string, error) {
	p.loadBodyCalls.Add(1)
	if p.loadBodyFn != nil {
		return p.loadBodyFn(ctx, key)
	}
	// Include markup + '&' so we can detect CDATA vs escaping.
	return "BODY<" + key.Name + ">&", nil
}

func (p *fakeProvider) ReadResource(
	ctx context.Context,
	key spec.SkillKey,
	resourcePath string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
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

func mustAddSkill(t *testing.T, rt *Runtime, ctx context.Context, key spec.SkillKey) spec.SkillRecord {
	t.Helper()
	rec, err := rt.AddSkill(ctx, key)
	if err != nil {
		t.Fatalf("AddSkill(%+v): %v", key, err)
	}
	return rec
}

func mustNewSession(t *testing.T, rt *Runtime, ctx context.Context) spec.SessionID {
	t.Helper()
	sid, err := rt.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return sid
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

const availableSkills = "availableSkills"

func mustUnmarshalAvailable(t *testing.T, s string) availableSkillsDoc {
	t.Helper()
	var doc availableSkillsDoc
	if err := xml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("unmarshal availableSkills: %v\nxml=%s", err, s)
	}
	if doc.XMLName.Local != availableSkills {
		t.Fatalf("expected root %s, got %q", availableSkills, doc.XMLName.Local)
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
				WithProvider(pOK),
			},
			wantErr: "",
		},
		{
			name: "WithProviders input is snapshotted (caller mutation after WithProviders does not affect runtime)",
			opts: func() []Option {
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

			// For the snapshot test, ensure providerTypes still contains "ok".
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

func TestRuntime_AddSkill_RemoveSkill_Errors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	p := &fakeProvider{typ: "p"}
	rt := mustNewRuntime(t, WithProvider(p))

	tests := []struct {
		name    string
		do      func() error
		wantErr error
	}{
		{
			name: "AddSkill invalid argument (missing fields)",
			do: func() error {
				_, err := rt.AddSkill(ctx, spec.SkillKey{})
				return err
			},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name: "AddSkill provider not found",
			do: func() error {
				_, err := rt.AddSkill(ctx, spec.SkillKey{
					Type:        "missing",
					SkillHandle: spec.SkillHandle{Name: "s", Location: "/x"},
				})
				return err
			},
			wantErr: spec.ErrProviderNotFound,
		},
		{
			name: "RemoveSkill missing",
			do: func() error {
				_, err := rt.RemoveSkill(ctx, spec.SkillKey{
					Type:        "p",
					SkillHandle: spec.SkillHandle{Name: "nope", Location: "/nope"},
				})
				return err
			},
			wantErr: spec.ErrSkillNotFound,
		},
		{
			name: "AddSkill duplicate",
			do: func() error {
				k := spec.SkillKey{Type: "p", SkillHandle: spec.SkillHandle{Name: "dup", Location: "/d"}}
				if _, err := rt.AddSkill(ctx, k); err != nil {
					return err
				}
				_, err := rt.AddSkill(ctx, k)
				return err
			},
			wantErr: spec.ErrSkillAlreadyExists,
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

func TestRuntime_SessionActivateKeys_CanonicalizesKeyViaProviderIndex(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	p := &fakeProvider{
		typ: "p",
		indexFn: func(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
			key.Location = "NORM:" + key.Location
			return spec.SkillRecord{Key: key, Description: "d:" + key.Name}, nil
		},
		loadBodyFn: func(ctx context.Context, key spec.SkillKey) (string, error) {
			return "BODY<" + key.Name + ">&", nil
		},
	}

	rt := mustNewRuntime(t,
		WithLogger(slog.New(slog.DiscardHandler)),
		WithProvider(p),
	)

	// Add skill (catalog stores normalized key).
	rec := mustAddSkill(t, rt, ctx, spec.SkillKey{
		Type:        "p",
		SkillHandle: spec.SkillHandle{Name: "s1", Location: "/p1"},
	})
	if !strings.HasPrefix(rec.Key.Location, "NORM:") {
		t.Fatalf("expected normalized location, got %q", rec.Key.Location)
	}

	// Activate using the *unnormalized* key - should still work (session canonicalizes via provider.Index).
	sid := mustNewSession(t, rt, ctx)
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	handles, err := rt.SessionActivateKeys(ctx, sid,
		[]spec.SkillKey{{
			Type:        "p",
			SkillHandle: spec.SkillHandle{Name: "s1", Location: "/p1"}, // unnormalized
		}},
		spec.LoadModeReplace,
	)
	if err != nil {
		t.Fatalf("SessionActivateKeys: %v", err)
	}
	if len(handles) != 1 {
		t.Fatalf("expected 1 handle, got %d", len(handles))
	}
	if handles[0].Location != rec.Key.Location {
		t.Fatalf("expected activated handle location %q, got %q", rec.Key.Location, handles[0].Location)
	}
}

func TestRuntime_ListSkills_ActivityAndSessionFilters(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	p := &fakeProvider{typ: "p"}
	rt := mustNewRuntime(t, WithProvider(p))

	rA := mustAddSkill(t, rt, ctx, spec.SkillKey{Type: "p", SkillHandle: spec.SkillHandle{Name: "a", Location: "/a"}})
	rB := mustAddSkill(t, rt, ctx, spec.SkillKey{Type: "p", SkillHandle: spec.SkillHandle{Name: "b", Location: "/b"}})
	rC := mustAddSkill(t, rt, ctx, spec.SkillKey{Type: "p", SkillHandle: spec.SkillHandle{Name: "c", Location: "/c"}})

	sid := mustNewSession(t, rt, ctx)
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	// Activate A and B.
	if _, err := rt.SessionActivateKeys(ctx, sid, []spec.SkillKey{rA.Key, rB.Key}, spec.LoadModeReplace); err != nil {
		t.Fatalf("SessionActivateKeys: %v", err)
	}

	tests := []struct {
		name      string
		filter    *SkillFilter
		wantCount int
		wantErr   error
	}{
		{
			name:      "nil filter => all",
			filter:    nil,
			wantCount: 3,
		},
		{
			name:      "activity any with session => still all records (listing is not prompt)",
			filter:    &SkillFilter{SessionID: sid, Activity: SkillActivityAny},
			wantCount: 3,
		},
		{
			name:      "activity active with session => only active",
			filter:    &SkillFilter{SessionID: sid, Activity: SkillActivityActive},
			wantCount: 2,
		},
		{
			name:      "activity inactive with session => only inactive",
			filter:    &SkillFilter{SessionID: sid, Activity: SkillActivityInactive},
			wantCount: 1,
		},
		{
			name:    "activity active without session => invalid argument",
			filter:  &SkillFilter{Activity: SkillActivityActive},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name:    "invalid activity => invalid argument",
			filter:  &SkillFilter{Activity: SkillActivity("nope")},
			wantErr: spec.ErrInvalidArgument,
		},
		{
			name:    "session missing => ErrSessionNotFound",
			filter:  &SkillFilter{SessionID: spec.SessionID("missing"), Activity: SkillActivityAny},
			wantErr: spec.ErrSessionNotFound,
		},
		{
			name: "allowKeys applies + inactive: allow only A and C, but A is active => only C remains",
			filter: &SkillFilter{
				SessionID: sid,
				Activity:  SkillActivityInactive,
				AllowKeys: []spec.SkillKey{rA.Key, rC.Key},
			},
			wantCount: 1,
		},
		{
			name:      "types filter",
			filter:    &SkillFilter{Types: []string{"p"}},
			wantCount: 3,
		},
		{
			name:      "location prefix filter",
			filter:    &SkillFilter{LocationPrefix: "/b"},
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

func TestRuntime_ListSkills_NamePrefixUsesLLMVisibleName_CollisionAddsTypePrefix(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	pA := &fakeProvider{typ: "a"}
	pB := &fakeProvider{typ: "b"}
	rt := mustNewRuntime(t, WithProvider(pA), WithProvider(pB))

	// Same name+location across types => LLM-visible name should become "a:x" and "b:x".
	_ = mustAddSkill(t, rt, ctx, spec.SkillKey{Type: "a", SkillHandle: spec.SkillHandle{Name: "x", Location: "/same"}})
	_ = mustAddSkill(t, rt, ctx, spec.SkillKey{Type: "b", SkillHandle: spec.SkillHandle{Name: "x", Location: "/same"}})

	recs, err := rt.ListSkills(ctx, &SkillFilter{NamePrefix: "a:"})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record for prefix a:, got %d: %+v", len(recs), recs)
	}
	if recs[0].Key.Type != "a" {
		t.Fatalf("expected type a, got %q", recs[0].Key.Type)
	}
}

func TestRuntime_SkillsPromptXML_RootsSectionsCDATAAndFiltering(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	p := &fakeProvider{
		typ: "p",
		loadBodyFn: func(ctx context.Context, key spec.SkillKey) (string, error) {
			// Ensure body contains markup + '&' so CDATA is detectable.
			return "BODY<" + key.Name + ">&", nil
		},
	}

	rt := mustNewRuntime(t, WithProvider(p))

	// Add out of order to validate availableSkills sorting.
	rB := mustAddSkill(t, rt, ctx, spec.SkillKey{Type: "p", SkillHandle: spec.SkillHandle{Name: "b", Location: "/b"}})
	rA := mustAddSkill(t, rt, ctx, spec.SkillKey{Type: "p", SkillHandle: spec.SkillHandle{Name: "a", Location: "/a"}})
	rC := mustAddSkill(t, rt, ctx, spec.SkillKey{Type: "p", SkillHandle: spec.SkillHandle{Name: "c", Location: "/c"}})

	// No session, default (nil filter) => availableSkills root only, sorted by name.
	xml1, err := rt.SkillsPromptXML(ctx, nil)
	if err != nil {
		t.Fatalf("SkillsPromptXML: %v", err)
	}
	if gotRoot := xmlRootName(t, xml1); gotRoot != availableSkills {
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

	// Create session and activate A then B (activation order should be preserved in active section).
	sid := mustNewSession(t, rt, ctx)
	t.Cleanup(func() { _ = rt.CloseSession(t.Context(), sid) })

	if _, err := rt.SessionActivateKeys(ctx, sid, []spec.SkillKey{rA.Key, rB.Key}, spec.LoadModeReplace); err != nil {
		t.Fatalf("SessionActivateKeys: %v", err)
	}

	// ActivityAny + session => skillsPrompt wrapper with both sections.
	xml2, err := rt.SkillsPromptXML(ctx, &SkillFilter{SessionID: sid, Activity: SkillActivityAny})
	if err != nil {
		t.Fatalf("SkillsPromptXML(any+session): %v", err)
	}
	if gotRoot := xmlRootName(t, xml2); gotRoot != "skillsPrompt" {
		t.Fatalf("expected root skillsPrompt, got %q\nxml=%s", gotRoot, xml2)
	}
	if !strings.Contains(xml2, "<![CDATA[") {
		t.Fatalf("expected CDATA in activeSkills output\nxml=%s", xml2)
	}
	if !strings.Contains(xml2, "BODY<a>&") && !strings.Contains(xml2, "BODY<b>&") {
		t.Fatalf("expected raw (unescaped) body in CDATA\nxml=%s", xml2)
	}

	doc := mustUnmarshalPrompt(t, xml2)
	if doc.Active == nil || doc.Available == nil {
		t.Fatalf("expected both active and available sections present\nxml=%s", xml2)
	}

	// Active should be A then B (activation order).
	if len(doc.Active.Skills) != 2 {
		t.Fatalf("expected 2 active skills, got %d\nxml=%s", len(doc.Active.Skills), xml2)
	}
	if doc.Active.Skills[0].Name != "a" || doc.Active.Skills[1].Name != "b" {
		t.Fatalf("expected active order [a b], got [%s %s]\nxml=%s",
			doc.Active.Skills[0].Name, doc.Active.Skills[1].Name, xml2)
	}
	if doc.Active.Skills[0].Body != "BODY<a>&" {
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
	if gotRoot := xmlRootName(t, xml4); gotRoot != availableSkills {
		t.Fatalf("expected root availableSkills, got %q\nxml=%s", gotRoot, xml4)
	}
	av4 := mustUnmarshalAvailable(t, xml4)
	if len(av4.Skills) != 1 || av4.Skills[0].Name != "c" {
		t.Fatalf("expected only c inactive, got %+v\nxml=%s", av4.Skills, xml4)
	}

	// AllowKeys filter should apply to both active and available sections.
	// Allow only C (inactive). Active should become empty, available should contain only C.
	xml5, err := rt.SkillsPromptXML(ctx, &SkillFilter{
		SessionID: sid,
		Activity:  SkillActivityAny,
		AllowKeys: []spec.SkillKey{rC.Key},
	})
	if err != nil {
		t.Fatalf("SkillsPromptXML(allowKeys): %v", err)
	}
	doc5 := mustUnmarshalPrompt(t, xml5)
	if doc5.Active == nil || doc5.Available == nil {
		t.Fatalf("expected both sections in wrapper\nxml=%s", xml5)
	}
	if len(doc5.Active.Skills) != 0 {
		t.Fatalf("expected 0 active skills after allowKeys restriction, got %d\nxml=%s", len(doc5.Active.Skills), xml5)
	}
	if len(doc5.Available.Skills) != 1 || doc5.Available.Skills[0].Name != "c" {
		t.Fatalf(
			"expected available to contain only c after allowKeys restriction, got %+v\nxml=%s",
			doc5.Available.Skills,
			xml5,
		)
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
		AllowKeys: []spec.SkillKey{
			{}, // dropped
			{Type: "t", SkillHandle: spec.SkillHandle{Name: "n", Location: "l"}},
			{Type: "t", SkillHandle: spec.SkillHandle{Name: "n", Location: "l"}}, // dedupe
			{Type: " ", SkillHandle: spec.SkillHandle{Name: "n", Location: "l"}}, // dropped
		},
		SessionID: "  sid  ",
		Activity:  "",
	}

	got := normalizeSkillsPromptFilter(in)

	// Types are trimmed + deduped.
	if strings.Join(got.Types, ",") != "a,b" && strings.Join(got.Types, ",") != "b,a" {
		t.Fatalf("unexpected types: %+v", got.Types)
	}
	// Stable order is not guaranteed; ensure set membership.
	hasA, hasB := false, false
	for _, tpe := range got.Types {
		if tpe == "a" {
			hasA = true
		}
		if tpe == "b" {
			hasB = true
		}
	}
	if !hasA || !hasB || len(got.Types) != 2 {
		t.Fatalf("unexpected types: %+v", got.Types)
	}

	if got.Activity != SkillActivityAny {
		t.Fatalf("expected default activity any, got %q", got.Activity)
	}
	if got.SessionID != "sid" {
		t.Fatalf("expected trimmed sessionID %q, got %q", "sid", got.SessionID)
	}
	if len(got.AllowKeys) != 1 {
		t.Fatalf("expected 1 allow key, got %d: %+v", len(got.AllowKeys), got.AllowKeys)
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
