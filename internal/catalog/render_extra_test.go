package catalog

import (
	"reflect"
	"sort"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestNormalizeSkillInsert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		in     spec.SkillInsert
		want   spec.SkillInsert
		wantOK bool
	}{
		{
			name:   "empty defaults to instructions",
			in:     spec.SkillInsert(""),
			want:   spec.SkillInsertInstructions,
			wantOK: true,
		},
		{
			name:   "trimmed instructions",
			in:     spec.SkillInsert(" instructions "),
			want:   spec.SkillInsertInstructions,
			wantOK: true,
		},
		{
			name:   "trimmed user message",
			in:     spec.SkillInsert(" USER-MESSAGE \n"),
			want:   spec.SkillInsertUserMessage,
			wantOK: true,
		},
		{name: "invalid value", in: spec.SkillInsert("template"), want: spec.SkillInsertInstructions, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := NormalizeSkillInsert(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("NormalizeSkillInsert(%q) = (%q,%v), want (%q,%v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIsValidSkillArgumentName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "underscore ok", in: "_ok", want: true},
		{name: "alnum ok", in: "a1_b2", want: true},
		{name: "empty invalid", in: "", want: false},
		{name: "leading digit invalid", in: "1abc", want: false},
		{name: "dash invalid", in: "bad-name", want: false},
		{name: "space invalid", in: "bad name", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsValidSkillArgumentName(tt.in); got != tt.want {
				t.Fatalf("IsValidSkillArgumentName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestScanIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		wantName string
		wantN    int
	}{
		{name: "empty", in: "", wantName: "", wantN: 0},
		{name: "leading digit rejected", in: "1abc", wantName: "", wantN: 0},
		{name: "valid identifier", in: "abc123", wantName: "abc123", wantN: 6},
		{name: "stops at punctuation", in: "abc-123", wantName: "abc", wantN: 3},
		{name: "underscore allowed", in: "_name9", wantName: "_name9", wantN: 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotName, gotN := scanIdentifier(tt.in)
			if gotName != tt.wantName || gotN != tt.wantN {
				t.Fatalf("scanIdentifier(%q) = (%q,%d), want (%q,%d)", tt.in, gotName, gotN, tt.wantName, tt.wantN)
			}
		})
	}
}

func TestRenderSkillBody_SubstitutionWarningsAndEscapes(t *testing.T) {
	t.Parallel()

	body := "Hello $name, greet {{ title }}.\nEscaped \\$name and $1bad and {{ unknown }}.\nRepeat $name."
	args := []spec.SkillArgument{
		{Name: "name", Default: "World"},
		{Name: "title", Default: "Commander"},
		{Name: "name", Default: "ignored"},
		{Name: "bad-name", Default: "x"},
	}
	values := map[string]string{"name": "Alice"}

	got := RenderSkillBody(body, args, values)

	wantText := "Hello Alice, greet Commander.\nEscaped $name and $1bad and {{ unknown }}.\nRepeat Alice."
	if got.Text != wantText {
		t.Fatalf("RenderSkillBody text mismatch\n\ngot:\n%s\n\nwant:\n%s", got.Text, wantText)
	}

	wantArgs := map[string]string{"name": "Alice", "title": "Commander"}
	if !reflect.DeepEqual(got.AppliedArguments, wantArgs) {
		t.Fatalf("AppliedArguments mismatch: got=%#v want=%#v", got.AppliedArguments, wantArgs)
	}

	if !reflect.DeepEqual(got.UnknownPlaceholders, []string{"unknown"}) {
		t.Fatalf("UnknownPlaceholders mismatch: got=%#v", got.UnknownPlaceholders)
	}

	wantWarnings := []string{
		"duplicate argument ignored: name",
		"invalid argument name ignored: bad-name",
		"unknown placeholder left unchanged: unknown",
	}
	if !reflect.DeepEqual(got.Warnings, wantWarnings) {
		t.Fatalf("Warnings mismatch: got=%#v want=%#v", got.Warnings, wantWarnings)
	}
}

func TestRenderSkillBody_EmptyBodyAndNoArgs(t *testing.T) {
	t.Parallel()

	got := RenderSkillBody("", nil, nil)
	if got.Text != "" {
		t.Fatalf("expected empty text, got %q", got.Text)
	}
	if len(got.AppliedArguments) != 0 {
		t.Fatalf("expected no applied arguments, got %#v", got.AppliedArguments)
	}
	if len(got.UnknownPlaceholders) != 0 {
		t.Fatalf("expected no unknown placeholders, got %#v", got.UnknownPlaceholders)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", got.Warnings)
	}
}

func TestUniqueSortedStrings(t *testing.T) {
	t.Parallel()

	got := uniqueSortedStrings([]string{" b ", "a", "", "a", "b", "c", " c"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueSortedStrings mismatch: got=%#v want=%#v", got, want)
	}

	if out := uniqueSortedStrings(nil); out != nil {
		t.Fatalf("expected nil for empty input, got %#v", out)
	}
}

func TestPromptFilterAndUserFilterMatchWithPrefixesAndInserts(t *testing.T) {
	t.Parallel()

	e := &entry{
		def: spec.SkillDef{Type: "t", Name: "alpha", Location: "/user/location"},
		idx: spec.ProviderSkillIndexRecord{
			Key:    spec.ProviderSkillKey{Type: "t", Name: "alpha", Location: "/canon/location"},
			Insert: spec.SkillInsertUserMessage,
		},
		llmName: "alpha#1234",
	}

	if !(PromptFilter{LLMNamePrefix: "alpha#", Inserts: []spec.SkillInsert{spec.SkillInsertUserMessage}}).match(e) {
		t.Fatalf("expected prompt filter to match")
	}
	if (PromptFilter{LLMNamePrefix: "alpha#123456789"}).match(e) {
		t.Fatalf("expected too-long LLMNamePrefix to fail")
	}
	if (PromptFilter{LLMNamePrefix: "beta"}).match(e) {
		t.Fatalf("expected mismatched LLMNamePrefix to fail")
	}
	if (PromptFilter{LocationPrefix: "/user/location/extra"}).match(e) {
		t.Fatalf("expected too-long LocationPrefix to fail")
	}
	if (PromptFilter{LocationPrefix: "/other"}).match(e) {
		t.Fatalf("expected mismatched LocationPrefix to fail")
	}
	if (PromptFilter{Types: []string{"other"}}).match(e) {
		t.Fatalf("expected mismatched type to fail")
	}
	if (PromptFilter{Inserts: []spec.SkillInsert{spec.SkillInsertInstructions}}).match(e) {
		t.Fatalf("expected mismatched insert to fail")
	}
	if !(UserFilter{NamePrefix: "alp", LocationPrefix: "/user", Inserts: []spec.SkillInsert{spec.SkillInsertUserMessage}}).match(
		e,
	) {
		t.Fatalf("expected user filter to match")
	}
	if (UserFilter{NamePrefix: "alphabet"}).match(e) {
		t.Fatalf("expected too-long user name prefix to fail")
	}
	if (UserFilter{NamePrefix: "bet"}).match(e) {
		t.Fatalf("expected user name prefix mismatch to fail")
	}
	if (UserFilter{LocationPrefix: "/user/location/extra"}).match(e) {
		t.Fatalf("expected too-long user location prefix to fail")
	}
	if (UserFilter{LocationPrefix: "/nope"}).match(e) {
		t.Fatalf("expected user location prefix mismatch to fail")
	}
	if (UserFilter{Types: []string{"other"}}).match(e) {
		t.Fatalf("expected user type mismatch to fail")
	}
	if (UserFilter{Inserts: []spec.SkillInsert{spec.SkillInsertInstructions}}).match(e) {
		t.Fatalf("expected user insert mismatch to fail")
	}
}

func TestListPromptAndUserEntries_InsertFilteringAndSorting(t *testing.T) {
	t.Parallel()

	p := &testProvider{typ: "t"}
	c := New(mapResolver{"t": p})

	defs := []spec.SkillDef{
		{Type: "t", Name: "beta", Location: "/2"},
		{Type: "t", Name: "alpha", Location: "/9"},
		{Type: "t", Name: "alpha", Location: "/1"},
	}
	for i, d := range defs {
		if _, err := c.Add(t.Context(), d); err != nil {
			t.Fatalf("Add[%d](%+v): %v", i, d, err)
		}
	}

	key, ok := c.ResolveDef(spec.SkillDef{Type: "t", Name: "alpha", Location: "/1"})
	if !ok {
		t.Fatalf("ResolveDef failed")
	}
	idx, ok := c.GetIndex(key)
	if !ok {
		t.Fatalf("GetIndex failed")
	}
	idx.Insert = spec.SkillInsertUserMessage
	c.mu.Lock()
	if e := c.byKey[key]; e != nil {
		e.idx = idx
	}
	c.mu.Unlock()

	promptRecords := c.ListPromptIndexRecords(PromptFilter{Inserts: []spec.SkillInsert{spec.SkillInsertUserMessage}})
	if len(promptRecords) != 1 || promptRecords[0].Key.Name != "alpha" || promptRecords[0].Key.Location != "/1" {
		t.Fatalf("unexpected prompt insert filter result: %+v", promptRecords)
	}

	userRecords := c.ListUserEntries(UserFilter{Inserts: []spec.SkillInsert{spec.SkillInsertUserMessage}})
	if len(userRecords) != 1 || userRecords[0].Record.Def.Name != "alpha" ||
		userRecords[0].Record.Def.Location != "/1" {
		t.Fatalf("unexpected user insert filter result: %+v", userRecords)
	}

	allPrompt := c.ListPromptIndexRecords(PromptFilter{})
	if len(allPrompt) != len(defs) {
		t.Fatalf("expected %d prompt records, got %d", len(defs), len(allPrompt))
	}
	gotNames := make([]string, 0, len(allPrompt))
	for _, rec := range allPrompt {
		gotNames = append(gotNames, rec.Key.Name+"@"+rec.Key.Location)
	}
	wantNames := append([]string(nil), gotNames...)
	sort.Strings(wantNames)
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("expected prompt records to be sorted, got=%v want=%v", gotNames, wantNames)
	}
}
