package spec

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

const skillToolSchemaDraft07URI = "http://json-schema.org/draft-07/schema#"

func TestSkillToolsExposeInputAndOutputSchemas(t *testing.T) {
	tests := []struct {
		name                string
		tool                llmtoolsgoSpec.Tool
		outputType          string
		outputRequired      []string
		outputRequiresItems bool
		scopedDescription   bool
	}{
		{
			name:           "skills-load",
			tool:           SkillsLoadTool(),
			outputType:     "object",
			outputRequired: []string{"activeSkills"},
		},
		{
			name:           "skills-unload",
			tool:           SkillsUnloadTool(),
			outputType:     "object",
			outputRequired: []string{"activeSkills"},
		},
		{
			name:                "skills-readresource",
			tool:                SkillsReadResourceTool(),
			outputType:          "array",
			outputRequiresItems: true,
			scopedDescription:   true,
		},
		{
			name:              "skills-runscript",
			tool:              SkillsRunScriptTool(),
			outputType:        "object",
			outputRequired:    []string{"location", "exitCode"},
			scopedDescription: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertSkillToolSchema(t, "argSchema", tc.tool.ArgSchema)
			output := assertSkillToolSchema(
				t,
				"outputSchema",
				tc.tool.OutputSchema,
			)

			assertSkillToolOutputShape(
				t,
				output,
				tc.outputType,
				tc.outputRequired,
				tc.outputRequiresItems,
			)

			if tc.scopedDescription &&
				!strings.Contains(
					tc.tool.Description,
					"inside the selected active skill directory",
				) {
				t.Fatalf(
					"tool description does not describe skill-directory scope: %q",
					tc.tool.Description,
				)
			}
		})
	}
}

func assertSkillToolSchema(
	t *testing.T,
	name string,
	raw llmtoolsgoSpec.JSONSchema,
) map[string]json.RawMessage {
	t.Helper()

	if len(bytes.TrimSpace(raw)) == 0 {
		t.Fatalf("%s is empty", name)
	}
	if !json.Valid(raw) {
		t.Fatalf("%s is not valid JSON: %s", name, raw)
	}

	var schema map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}

	var schemaURI string
	if err := json.Unmarshal(schema["$schema"], &schemaURI); err != nil {
		t.Fatalf("decode %s $schema: %v", name, err)
	}
	if schemaURI != skillToolSchemaDraft07URI {
		t.Fatalf(
			"%s $schema = %q, want %q",
			name,
			schemaURI,
			skillToolSchemaDraft07URI,
		)
	}

	if len(schema["type"]) == 0 {
		t.Fatalf("%s has no root type", name)
	}
	return schema
}

func assertSkillToolOutputShape(
	t *testing.T,
	schema map[string]json.RawMessage,
	wantType string,
	wantRequired []string,
	requireItems bool,
) {
	t.Helper()

	var gotType string
	if err := json.Unmarshal(schema["type"], &gotType); err != nil {
		t.Fatalf("decode output schema type: %v", err)
	}
	if gotType != wantType {
		t.Fatalf("output schema type = %q, want %q", gotType, wantType)
	}

	if requireItems && len(schema["items"]) == 0 {
		t.Fatal("output schema has no items definition")
	}

	if len(wantRequired) == 0 {
		return
	}

	var properties map[string]json.RawMessage
	if err := json.Unmarshal(schema["properties"], &properties); err != nil {
		t.Fatalf("decode output schema properties: %v", err)
	}
	for _, name := range wantRequired {
		if len(properties[name]) == 0 {
			t.Fatalf("output schema has no %q property", name)
		}
	}

	var required []string
	if err := json.Unmarshal(schema["required"], &required); err != nil {
		t.Fatalf("decode output schema required fields: %v", err)
	}
	for _, name := range wantRequired {
		if !containsSchemaString(required, name) {
			t.Fatalf("output schema does not require %q", name)
		}
	}
}

func containsSchemaString(values []string, wanted string) bool {
	return slices.Contains(values, wanted)
}
