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

func TestParseSkillDocument_ToleratesAndNormalizesOptionalMetadata(t *testing.T) {
	t.Parallel()

	document, warnings, err := ParseSkillDocument(
		[]byte(
			"\ufeff---\r\nname: tolerant-skill\r\ndescription: \" A tolerant skill. \"\r\ninsert: unsupported\r\narguments:\r\n  - name: topic\r\n    description: 42\r\n    default: Go\r\n  - invalid name\r\n  - topic\r\ntags:\r\n  - writing\r\n  - \" \"\r\n  - 42\r\n  - writing\r\ncustom:\r\n  nested: retained\r\n...\r\n\r\n# Tolerant Skill\r\n\r\nDiscuss $topic and {{ unknown }}.\r\n",
		),
		spec.ParseSkillDocumentOptions{ExpectedName: "tolerant-skill"},
	)
	if err != nil {
		t.Fatalf("ParseSkillDocument() error = %v", err)
	}

	if document.DisplayName != "Tolerant Skill" {
		t.Fatalf("DisplayName = %q", document.DisplayName)
	}
	if document.Description != "A tolerant skill." {
		t.Fatalf("Description = %q", document.Description)
	}
	if document.Insert != spec.SkillInsertInstructions {
		t.Fatalf("Insert = %q", document.Insert)
	}
	if len(document.Arguments) != 1 || document.Arguments[0].Name != "topic" {
		t.Fatalf("Arguments = %+v", document.Arguments)
	}
	if document.Arguments[0].Description != "" || document.Arguments[0].Default != "Go" {
		t.Fatalf("Argument = %+v", document.Arguments[0])
	}
	if len(document.Tags) != 1 || document.Tags[0] != "writing" {
		t.Fatalf("Tags = %v", document.Tags)
	}
	if document.MarkdownBody != "# Tolerant Skill\n\nDiscuss $topic and {{ unknown }}.\n" {
		t.Fatalf("MarkdownBody = %q", document.MarkdownBody)
	}
	custom, ok := document.RawFrontmatter["custom"].(map[string]any)
	if !ok || custom["nested"] != "retained" {
		t.Fatalf("RawFrontmatter custom = %#v", document.RawFrontmatter["custom"])
	}

	allWarnings := strings.Join(warnings, "\n")
	for _, expected := range []string{
		"UTF-8 BOM was removed",
		"whitespace removed",
		"unsupported frontmatter.insert",
		"arguments[0].description",
		"arguments[1].name is invalid",
		"duplicate argument",
		"tags[1] was ignored because it is empty",
		"tags[2] was ignored because it is not a string",
	} {
		if !strings.Contains(allWarnings, expected) {
			t.Fatalf("warnings %q do not contain %q", allWarnings, expected)
		}
	}
}

func TestParseSkillDocument_RejectsRequiredOrUnsafeInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
		options spec.ParseSkillDocumentOptions
		want    string
	}{
		{
			name:    "missing frontmatter",
			content: []byte("# No frontmatter\n"),
			want:    "requires YAML frontmatter",
		},
		{
			name:    "unterminated frontmatter",
			content: []byte("---\nname: valid-skill\ndescription: x\n"),
			want:    "unterminated YAML frontmatter",
		},
		{
			name:    "missing name",
			content: []byte("---\ndescription: x\n---\n"),
			want:    "frontmatter.name is required",
		},
		{
			name:    "non-string description",
			content: []byte("---\nname: valid-skill\ndescription: 42\n---\n"),
			want:    "frontmatter.description must be a string",
		},
		{
			name:    "invalid name",
			content: []byte("---\nname: Invalid--Skill\ndescription: x\n---\n"),
			want:    "lowercase hyphenated",
		},
		{
			name:    "expected name mismatch",
			content: []byte("---\nname: valid-skill\ndescription: x\n---\n"),
			options: spec.ParseSkillDocumentOptions{ExpectedName: "other-skill"},
			want:    "must match expected name",
		},
		{
			name:    "invalid utf8",
			content: []byte{'-', '-', '-', '\n', 0xff},
			want:    "valid UTF-8",
		},
		{
			name:    "nul byte",
			content: []byte("---\nname: valid-skill\ndescription: x\n---\n\x00"),
			want:    "NUL byte",
		},
		{
			name:    "oversized document",
			content: []byte(strings.Repeat("x", MaxSkillDocumentBytes+1)),
			want:    "exceeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := ParseSkillDocument(tt.content, tt.options)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestRenderAndMarshalSkillDocument_RejectInvalidDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document spec.SkillDocument
	}{
		{
			name: "unsupported insert",
			document: spec.SkillDocument{
				Name: "valid-skill", Description: "x", Insert: "elsewhere",
			},
		},
		{
			name: "duplicate arguments",
			document: spec.SkillDocument{
				Name:        "valid-skill",
				Description: "x",
				Insert:      spec.SkillInsertInstructions,
				Arguments: []spec.SkillArgument{
					{Name: "topic"}, {Name: "topic"},
				},
			},
		},
		{
			name: "untrimmed tag",
			document: spec.SkillDocument{
				Name:        "valid-skill",
				Description: "x",
				Insert:      spec.SkillInsertInstructions,
				Tags:        []string{" writing "},
			},
		},
		{
			name: "invalid body utf8",
			document: spec.SkillDocument{
				Name:         "valid-skill",
				Description:  "x",
				Insert:       spec.SkillInsertInstructions,
				MarkdownBody: string([]byte{0xff}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := RenderSkillDocument(tt.document, nil); err == nil ||
				!strings.Contains(err.Error(), "invalid Skill document") {
				t.Fatalf("RenderSkillDocument() error = %v", err)
			}
			if _, err := MarshalSkillDocument(tt.document); err == nil ||
				!strings.Contains(err.Error(), "invalid Skill document") {
				t.Fatalf("MarshalSkillDocument() error = %v", err)
			}
		})
	}
}
