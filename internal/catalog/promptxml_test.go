package catalog

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestAvailableSkillsXML_SortsAndEscapes(t *testing.T) {
	t.Parallel()

	in := []AvailableSkillItem{
		{Name: "b", Description: "2 & 3", Path: "/2"},
		{Name: "a", Description: "<x>", Path: "/9"},
		{Name: "a", Description: "ok", Path: "/1"},
	}

	s, err := AvailableSkillsXML(in)
	if err != nil {
		t.Fatalf("AvailableSkillsXML: %v", err)
	}
	if !strings.Contains(s, "<available_skills") {
		t.Fatalf("unexpected xml: %s", s)
	}

	var decoded availableSkills
	if err := xml.Unmarshal([]byte(s), &decoded); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if len(decoded.Skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(decoded.Skills))
	}

	// Sorted by name then path.
	if decoded.Skills[0].Name != "a" || decoded.Skills[0].Path != "/1" {
		t.Fatalf("unexpected first: %+v", decoded.Skills[0])
	}
	if decoded.Skills[1].Name != "a" || decoded.Skills[1].Path != "/9" {
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

func TestActiveSkillsXML_UsesCDATA(t *testing.T) {
	t.Parallel()

	in := []ActiveSkillItem{
		{Name: "s", Body: "use <tag> & keep raw"},
	}
	s, err := ActiveSkillsXML(in)
	if err != nil {
		t.Fatalf("ActiveSkillsXML: %v", err)
	}
	if !strings.Contains(s, "<active_skills") {
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
	if len(decoded.Skills) != 1 || decoded.Skills[0].Name != "s" {
		t.Fatalf("unexpected decoded: %+v", decoded)
	}
	if decoded.Skills[0].Body != in[0].Body {
		t.Fatalf("body mismatch: got=%q want=%q", decoded.Skills[0].Body, in[0].Body)
	}
}
