package catalog

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

func TestAvailableSkillsXML_SortsAndEscapes(t *testing.T) {
	t.Parallel()

	in := []AvailableSkillItem{
		{Name: "b", Description: "2 & 3", Location: "/2"},
		{Name: "a", Description: "<x>", Location: "/9"},
		{Name: "a", Description: "ok", Location: "/1"},
	}

	orig := append([]AvailableSkillItem(nil), in...)

	s, err := AvailableSkillsXML(in)
	if err != nil {
		t.Fatalf("AvailableSkillsXML: %v", err)
	}
	if !strings.Contains(s, "<availableSkills") {
		t.Fatalf("unexpected xml: %s", s)
	}

	// Ensure input slice was not mutated (function sorts a copy).
	if !reflect.DeepEqual(in, orig) {
		t.Fatalf("expected input not to be mutated; got=%v want=%v", in, orig)
	}

	var decoded availableSkills
	if err := xml.Unmarshal([]byte(s), &decoded); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if len(decoded.Skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(decoded.Skills))
	}

	// Sorted by name then Location.
	if decoded.Skills[0].Name != "a" || decoded.Skills[0].Location != "/1" {
		t.Fatalf("unexpected first: %+v", decoded.Skills[0])
	}
	if decoded.Skills[1].Name != "a" || decoded.Skills[1].Location != "/9" {
		t.Fatalf("unexpected second: %+v", decoded.Skills[1])
	}
	if decoded.Skills[2].Name != "b" {
		t.Fatalf("unexpected third: %+v", decoded.Skills[2])
	}

	// Ensure escaping happened on marshal: the raw string should not contain "<x>" inside description tags.
	if strings.Contains(s, "<description><x></description>") {
		t.Fatalf("expected description to be escaped, got: %s", s)
	}
}

func TestAvailableSkillsXML_Empty(t *testing.T) {
	t.Parallel()

	s, err := AvailableSkillsXML(nil)
	if err != nil {
		t.Fatalf("AvailableSkillsXML: %v", err)
	}
	if !strings.Contains(s, "<availableSkills") {
		t.Fatalf("unexpected xml: %s", s)
	}

	var decoded availableSkills
	if err := xml.Unmarshal([]byte(s), &decoded); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if len(decoded.Skills) != 0 {
		t.Fatalf("expected 0 skills, got %d", len(decoded.Skills))
	}
}

func TestActiveSkillsXML_UsesCDATA_AndPreservesOrder(t *testing.T) {
	t.Parallel()

	in := []ActiveSkillItem{
		{Name: "s1", Body: "use <tag> & keep raw"},
		{Name: "s2", Body: "second"},
	}
	s, err := ActiveSkillsXML(in)
	if err != nil {
		t.Fatalf("ActiveSkillsXML: %v", err)
	}
	if !strings.Contains(s, "<activeSkills") {
		t.Fatalf("unexpected xml: %s", s)
	}
	// CDATA marker should appear.
	if !strings.Contains(s, "<![CDATA[") {
		t.Fatalf("expected CDATA, got: %s", s)
	}

	var decoded activeSkills
	if err := xml.Unmarshal([]byte(s), &decoded); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if len(decoded.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(decoded.Skills))
	}
	if decoded.Skills[0].Name != "s1" || decoded.Skills[1].Name != "s2" {
		t.Fatalf("expected order preserved, got: %+v", decoded.Skills)
	}
	if decoded.Skills[0].Body != in[0].Body || decoded.Skills[1].Body != in[1].Body {
		t.Fatalf("body mismatch: got=%q/%q want=%q/%q",
			decoded.Skills[0].Body, decoded.Skills[1].Body,
			in[0].Body, in[1].Body,
		)
	}
}
