package integration

import (
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/document"
)

func TestReadmeDocumentWorkflow(t *testing.T) {
	raw := []byte(`---
name: summarize-text
description: Summarizes supplied text.
insert: user-message
arguments:
  - name: text
    description: Text to summarize.
  - name: tone
    description: Desired summary tone.
    default: concise
tags:
  - writing
source: editor
---

# Summarize Text

Summarize $text in a $tone tone.
`)

	parsed, warnings, err := document.ParseSkillDocument(
		raw,
		document.ParseSkillDocumentOptions{
			ExpectedName: "summarize-text",
		},
	)
	if err != nil {
		t.Fatalf("ParseSkillDocument() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("ParseSkillDocument() warnings = %v, want none", warnings)
	}
	if parsed.DisplayName != "Summarize Text" {
		t.Fatalf("DisplayName = %q, want %q", parsed.DisplayName, "Summarize Text")
	}
	if parsed.Insert != document.SkillInsertUserMessage {
		t.Fatalf("Insert = %q, want %q", parsed.Insert, document.SkillInsertUserMessage)
	}
	if parsed.RawFrontmatter["source"] != "editor" {
		t.Fatalf("RawFrontmatter[source] = %#v, want %q", parsed.RawFrontmatter["source"], "editor")
	}

	rendered, err := document.RenderSkillDocument(
		parsed,
		map[string]string{
			"text": "Design notes",
		},
	)
	if err != nil {
		t.Fatalf("RenderSkillDocument() error = %v", err)
	}
	if !strings.Contains(
		rendered.Text,
		"Summarize Design notes in a concise tone.",
	) {
		t.Fatalf("rendered text = %q", rendered.Text)
	}
	if rendered.AppliedArguments["text"] != "Design notes" {
		t.Fatalf("applied text argument = %q", rendered.AppliedArguments["text"])
	}
	if rendered.AppliedArguments["tone"] != "concise" {
		t.Fatalf("applied tone argument = %q", rendered.AppliedArguments["tone"])
	}

	marshaled, err := document.MarshalSkillDocument(parsed)
	if err != nil {
		t.Fatalf("MarshalSkillDocument() error = %v", err)
	}

	roundTripped, roundTripWarnings, err := document.ParseSkillDocument(
		marshaled,
		document.ParseSkillDocumentOptions{
			ExpectedName: "summarize-text",
		},
	)
	if err != nil {
		t.Fatalf("round-trip ParseSkillDocument() error = %v", err)
	}
	if len(roundTripWarnings) != 0 {
		t.Fatalf("round-trip warnings = %v, want none", roundTripWarnings)
	}
	if roundTripped.Insert != document.SkillInsertUserMessage {
		t.Fatalf("round-trip insert = %q", roundTripped.Insert)
	}
	if roundTripped.RawFrontmatter["source"] != "editor" {
		t.Fatalf(
			"round-trip RawFrontmatter[source] = %#v, want %q",
			roundTripped.RawFrontmatter["source"],
			"editor",
		)
	}
}
