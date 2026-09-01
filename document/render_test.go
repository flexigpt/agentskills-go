package document

import (
	"reflect"
	"testing"
)

func TestNormalizeSkillInsert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		in     SkillInsert
		want   SkillInsert
		wantOK bool
	}{
		{
			name:   "empty defaults to instructions",
			in:     SkillInsert(""),
			want:   SkillInsertInstructions,
			wantOK: true,
		},
		{
			name:   "trimmed instructions",
			in:     SkillInsert(" instructions "),
			want:   SkillInsertInstructions,
			wantOK: true,
		},
		{
			name:   "trimmed user message",
			in:     SkillInsert(" USER-MESSAGE \n"),
			want:   SkillInsertUserMessage,
			wantOK: true,
		},
		{name: "invalid value", in: SkillInsert("template"), want: SkillInsertInstructions, wantOK: false},
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
			if got := isValidSkillArgumentName(tt.in); got != tt.want {
				t.Fatalf("isValidSkillArgumentName(%q) = %v, want %v", tt.in, got, tt.want)
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
	args := []SkillArgument{
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
