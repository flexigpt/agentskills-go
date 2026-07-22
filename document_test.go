package agentskills

import (
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestParseSkillDocumentToleratesOptionalMetadata(t *testing.T) {
	t.Parallel()

	document, warnings, err := ParseSkillDocument(
		[]byte(`---
name: example-skill
description: " Example description "
insert: unsupported-placement
arguments:
  - name: topic
    description: Topic to discuss.
  - 42
  - topic
tags: useful
custom-field:
  retained: true
---

`),
		spec.ParseSkillDocumentOptions{
			ExpectedName: "example-skill",
		},
	)
	if err != nil {
		t.Fatalf("ParseSkillDocument() error = %v", err)
	}

	if document.Name != "example-skill" {
		t.Fatalf("Name = %q", document.Name)
	}
	if document.Description != "Example description" {
		t.Fatalf("Description = %q", document.Description)
	}
	if document.Insert != spec.SkillInsertInstructions {
		t.Fatalf("Insert = %q", document.Insert)
	}
	if len(document.Arguments) != 1 ||
		document.Arguments[0].Name != "topic" {
		t.Fatalf("Arguments = %+v", document.Arguments)
	}
	if len(document.Tags) != 1 || document.Tags[0] != "useful" {
		t.Fatalf("Tags = %v", document.Tags)
	}
	if document.RawFrontmatter["custom-field"] == nil {
		t.Fatal("expected custom-field to be retained")
	}

	allWarnings := strings.Join(warnings, "\n")
	for _, expected := range []string{
		"whitespace removed",
		"unsupported frontmatter.insert",
		"arguments[1]",
		"duplicate argument",
		"tags string",
		"body is empty",
	} {
		if !strings.Contains(allWarnings, expected) {
			t.Fatalf(
				"warnings %q do not contain %q",
				allWarnings,
				expected,
			)
		}
	}
}

func TestRenderSkillDocument(t *testing.T) {
	t.Parallel()

	output, err := RenderSkillDocument(
		spec.SkillDocument{
			Name:        "example-skill",
			DisplayName: "Example Skill",
			Description: "An example skill.",
			Insert:      spec.SkillInsertUserMessage,
			Arguments: []spec.SkillArgument{
				{Name: "topic", Default: "Go"},
			},
			Tags:         []string{"example"},
			MarkdownBody: "Explain $topic and {{ missing }}.",
		},
		map[string]string{"topic": "Agent Skills"},
	)
	if err != nil {
		t.Fatalf("RenderSkillDocument() error = %v", err)
	}

	if output.Text != "Explain Agent Skills and {{ missing }}." {
		t.Fatalf("Text = %q", output.Text)
	}
	if output.Insert != spec.SkillInsertUserMessage {
		t.Fatalf("Insert = %q", output.Insert)
	}
	if len(output.Tags) != 1 || output.Tags[0] != "example" {
		t.Fatalf("Tags = %v", output.Tags)
	}
	if output.AppliedArguments["topic"] != "Agent Skills" {
		t.Fatalf("AppliedArguments = %v", output.AppliedArguments)
	}
	if len(output.Warnings) == 0 {
		t.Fatal("expected warning for unknown placeholder")
	}
}

func TestMarshalSkillDocumentRoundTrip(t *testing.T) {
	t.Parallel()

	input := spec.SkillDocument{
		Name:        "example-skill",
		DisplayName: "Example Skill",
		Description: "An example skill.",
		Insert:      spec.SkillInsertInstructions,
		Arguments: []spec.SkillArgument{
			{Name: "topic", Default: "Go"},
		},
		Tags:         []string{"example", "writing"},
		MarkdownBody: "Discuss $topic.",
		RawFrontmatter: map[string]any{
			"custom-field": "retained",
		},
	}

	raw, err := MarshalSkillDocument(input)
	if err != nil {
		t.Fatalf("MarshalSkillDocument() error = %v", err)
	}

	output, _, err := ParseSkillDocument(
		raw,
		spec.ParseSkillDocumentOptions{},
	)
	if err != nil {
		t.Fatalf("ParseSkillDocument() error = %v", err)
	}

	if output.Name != input.Name ||
		output.Description != input.Description ||
		output.DisplayName != input.DisplayName {
		t.Fatalf("round-trip output = %+v", output)
	}
	if output.RawFrontmatter["custom-field"] != "retained" {
		t.Fatalf(
			"custom field = %#v",
			output.RawFrontmatter["custom-field"],
		)
	}
}
