package catalog

import (
	"reflect"
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestPromptFilterAndUserFilter_MatchNilEntry(t *testing.T) {
	t.Parallel()

	if (PromptFilter{}).match(nil) {
		t.Fatalf("expected PromptFilter.match(nil)=false")
	}
	if (UserFilter{}).match(nil) {
		t.Fatalf("expected UserFilter.match(nil)=false")
	}
}

func TestCatalog_ListPromptIndexRecords_FiltersAndSorts(t *testing.T) {
	t.Parallel()

	pa := &testProvider{typ: "a"}
	pb := &testProvider{typ: "b"}
	c := New(mapResolver{"a": pa, "b": pb})

	// Include a collision pair to exercise llmName prefix filtering.
	defs := []spec.SkillDef{
		{Type: "a", Name: "zeta", Location: "/2"},
		{Type: "a", Name: "alpha", Location: "/9"},
		{Type: "a", Name: "alpha", Location: "/1"},
		{Type: "a", Name: "collide", Location: "/c"},
		{Type: "b", Name: "collide", Location: "/c"},
	}
	for _, d := range defs {
		if _, err := c.Add(t.Context(), d); err != nil {
			t.Fatalf("Add(%+v): %v", d, err)
		}
	}

	// Sorting: by canonical key.Name then canonical key.Location.
	all := c.ListPromptIndexRecords(PromptFilter{})
	gotNamesLocs := make([]string, 0, len(all))
	for _, r := range all {
		gotNamesLocs = append(gotNamesLocs, r.Key.Name+"@"+r.Key.Location)
	}

	// Since our providers keep Location canonical == user Location, expected order is:
	// alpha@/1, alpha@/9, collide@/c, zeta@/2
	// (the two collide entries tie on name/location; order between them is not specified).
	if len(all) != len(defs) {
		t.Fatalf("expected %d records, got %d", len(defs), len(all))
	}
	if gotNamesLocs[0] != "alpha@/1" || gotNamesLocs[1] != "alpha@/9" {
		t.Fatalf("unexpected initial sort order: %v", gotNamesLocs)
	}
	if gotNamesLocs[len(gotNamesLocs)-1] != "zeta@/2" {
		t.Fatalf("unexpected final sort order: %v", gotNamesLocs)
	}

	// Types filter.
	onlyB := c.ListPromptIndexRecords(PromptFilter{Types: []string{"b"}})
	if len(onlyB) != 1 {
		t.Fatalf("expected 1 record for type=b, got %d", len(onlyB))
	}
	if onlyB[0].Key.Type != "b" || onlyB[0].Key.Name != "collide" {
		t.Fatalf("unexpected record for type=b: %+v", onlyB[0])
	}

	// LLMNamePrefix filter: ensure "collide" skills have llmName starting with "collide#".
	keyA, ok := c.ResolveDef(spec.SkillDef{Type: "a", Name: "collide", Location: "/c"})
	if !ok {
		t.Fatalf("ResolveDef(a/collide) failed")
	}
	hA, ok := c.HandleForKey(keyA)
	if !ok {
		t.Fatalf("HandleForKey failed")
	}
	if !strings.HasPrefix(hA.Name, "collide#") {
		t.Fatalf("expected disambiguated LLM name for collide, got %+v", hA)
	}

	prefixMatches := c.ListPromptIndexRecords(PromptFilter{LLMNamePrefix: "collide#"})
	if len(prefixMatches) != 2 {
		t.Fatalf("expected 2 records for LLMNamePrefix=collide#, got %d", len(prefixMatches))
	}

	// LocationPrefix filter (uses user def.Location, which matches our canonical in this test).
	locMatches := c.ListPromptIndexRecords(PromptFilter{LocationPrefix: "/c"})
	if len(locMatches) != 2 {
		t.Fatalf("expected 2 records for LocationPrefix=/c, got %d", len(locMatches))
	}

	// AllowDefs filter (exact host/lifecycle defs).
	allow := []spec.SkillDef{
		{Type: "a", Name: "alpha", Location: "/1"},
		{Type: "a", Name: "zeta", Location: "/2"},
	}
	allowed := c.ListPromptIndexRecords(PromptFilter{AllowDefs: allow})
	if len(allowed) != 2 {
		t.Fatalf("expected 2 records for allowlist, got %d", len(allowed))
	}
}

func TestCatalog_ListUserEntries_FiltersAndSorts(t *testing.T) {
	t.Parallel()

	p := &testProvider{typ: "t"}
	c := New(mapResolver{"t": p})

	defs := []spec.SkillDef{
		{Type: "t", Name: "b", Location: "/2"},
		{Type: "t", Name: "a", Location: "/9"},
		{Type: "t", Name: "a", Location: "/1"},
	}
	for _, d := range defs {
		if _, err := c.Add(t.Context(), d); err != nil {
			t.Fatalf("Add(%+v): %v", d, err)
		}
	}

	all := c.ListUserEntries(UserFilter{})
	if len(all) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(all))
	}
	// Sorted by host def.Name then host def.Location.
	got := []string{
		all[0].Record.Def.Name + "@" + all[0].Record.Def.Location,
		all[1].Record.Def.Name + "@" + all[1].Record.Def.Location,
		all[2].Record.Def.Name + "@" + all[2].Record.Def.Location,
	}
	wantPrefix := []string{"a@/1", "a@/9", "b@/2"}
	if !reflect.DeepEqual(got, wantPrefix) {
		t.Fatalf("unexpected sort order: got=%v want=%v", got, wantPrefix)
	}

	// NamePrefix filter.
	onlyA := c.ListUserEntries(UserFilter{NamePrefix: "a"})
	if len(onlyA) != 2 {
		t.Fatalf("expected 2 entries for NamePrefix=a, got %d", len(onlyA))
	}

	// LocationPrefix filter.
	onlySlash9 := c.ListUserEntries(UserFilter{LocationPrefix: "/9"})
	if len(onlySlash9) != 1 {
		t.Fatalf("expected 1 entry for LocationPrefix=/9, got %d", len(onlySlash9))
	}
	if onlySlash9[0].Record.Def.Location != "/9" {
		t.Fatalf("unexpected entry: %+v", onlySlash9[0])
	}

	// AllowDefs filter.
	allow := []spec.SkillDef{{Type: "t", Name: "b", Location: "/2"}}
	allowed := c.ListUserEntries(UserFilter{AllowDefs: allow})
	if len(allowed) != 1 || allowed[0].Record.Def.Name != "b" {
		t.Fatalf("unexpected allowlist result: %+v", allowed)
	}
}
