package catalog

import (
	"reflect"
	"testing"
)

func TestAvailableSkillsPrompt(t *testing.T) {
	tests := []struct {
		name string
		in   []AvailableSkillItem
		want string
	}{
		{
			name: "empty",
			in:   nil,
			want: `<<<AVAILABLE_SKILLS>>>
(none)
<<<END_AVAILABLE_SKILLS>>>`,
		},
		{
			name: "sorts by name then location and preserves raw text",
			in: []AvailableSkillItem{
				{Name: "b", Description: "2 & 3", Location: "/2"},
				{Name: "a", Description: "<x>", Location: "/9"},
				{Name: "a", Description: "ok", Location: "/1"},
			},
			want: `<<<AVAILABLE_SKILLS>>>
name: a
location: /1
description: ok
---
name: a
location: /9
description: <x>
---
name: b
location: /2
description: 2 & 3
<<<END_AVAILABLE_SKILLS>>>`,
		},
		{
			name: "trims and flattens inline fields and omits empty optional fields",
			in: []AvailableSkillItem{
				{Name: "  skill  ", Description: " line one\nline two ", Location: "  local  "},
				{Name: "name-only"},
			},
			want: `<<<AVAILABLE_SKILLS>>>
name: skill
location: local
description: line one line two
---
name: name-only
<<<END_AVAILABLE_SKILLS>>>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := cloneAvailableSkillItems(tt.in)

			got := AvailableSkillsPrompt(tt.in)
			if got != tt.want {
				t.Fatalf("AvailableSkillsPrompt() mismatch\n\ngot:\n%s\n\nwant:\n%s", got, tt.want)
			}

			if !reflect.DeepEqual(tt.in, orig) {
				t.Fatalf("AvailableSkillsPrompt() mutated input\n\ngot:  %#v\nwant: %#v", tt.in, orig)
			}
		})
	}
}

func TestActiveSkillsPrompt(t *testing.T) {
	tests := []struct {
		name string
		in   []ActiveSkillItem
		want string
	}{
		{
			name: "empty",
			in:   nil,
			want: `<<<ACTIVE_SKILLS>>>
(none)
<<<END_ACTIVE_SKILLS>>>`,
		},
		{
			name: "preserves input order and raw body text",
			in: []ActiveSkillItem{
				{Name: "s1", Body: "use <tag> & keep raw"},
				{Name: "s2", Body: "second"},
			},
			want: `<<<ACTIVE_SKILLS>>>
name: s1
body:
use <tag> & keep raw
<!-- SKILL SEPARATOR -->
name: s2
body:
second
<<<END_ACTIVE_SKILLS>>>`,
		},
		{
			name: "trims trailing newlines from body but preserves internal blank lines",
			in: []ActiveSkillItem{
				{Name: "planner", Body: "line one\n\nline three\n\n"},
			},
			want: `<<<ACTIVE_SKILLS>>>
name: planner
body:
line one

line three
<<<END_ACTIVE_SKILLS>>>`,
		},
		{
			name: "empty body still renders body field",
			in: []ActiveSkillItem{
				{Name: "planner", Body: ""},
			},
			want: `<<<ACTIVE_SKILLS>>>
name: planner
body:
<<<END_ACTIVE_SKILLS>>>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := cloneActiveSkillItems(tt.in)

			got := ActiveSkillsPrompt(tt.in)
			if got != tt.want {
				t.Fatalf("ActiveSkillsPrompt() mismatch\n\ngot:\n%s\n\nwant:\n%s", got, tt.want)
			}

			if !reflect.DeepEqual(tt.in, orig) {
				t.Fatalf("ActiveSkillsPrompt() mutated input\n\ngot:  %#v\nwant: %#v", tt.in, orig)
			}
		})
	}
}

func cloneAvailableSkillItems(in []AvailableSkillItem) []AvailableSkillItem {
	if in == nil {
		return nil
	}
	out := make([]AvailableSkillItem, len(in))
	copy(out, in)
	return out
}

func cloneActiveSkillItems(in []ActiveSkillItem) []ActiveSkillItem {
	if in == nil {
		return nil
	}
	out := make([]ActiveSkillItem, len(in))
	copy(out, in)
	return out
}
