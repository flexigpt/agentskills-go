package spec

import (
	"strings"
	"testing"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

func TestSkillRulesPrompts(t *testing.T) {
	t.Parallel()

	if !strings.Contains(SkillsRulesPromptLoadOnly, "skills-load") ||
		!strings.Contains(SkillsRulesPromptLoadOnly, "Only use skills") {
		t.Fatalf("unexpected load-only prompt: %q", SkillsRulesPromptLoadOnly)
	}
	if !strings.Contains(SkillsRulesPromptWithoutRunScript, "skills-readresource") ||
		strings.Contains(SkillsRulesPromptWithoutRunScript, "skills-runscript") {
		t.Fatalf("unexpected without-run-script prompt: %q", SkillsRulesPromptWithoutRunScript)
	}
	if !strings.Contains(SkillsRulesPromptAll, "skills-runscript") ||
		!strings.Contains(SkillsRulesPromptAll, "skills-load") {
		t.Fatalf("unexpected all prompt: %q", SkillsRulesPromptAll)
	}
}

func TestSkillToolConstructors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		got      llmtoolsgoSpec.Tool
		wantSlug string
		wantID   llmtoolsgoSpec.FuncID
		wantTags []string
	}{
		{
			name:     "load",
			got:      SkillsLoadTool(),
			wantSlug: "skills-load",
			wantID:   FuncIDSkillsLoad,
			wantTags: []string{toolTagSkills},
		},
		{
			name:     "readresource",
			got:      SkillsReadResourceTool(),
			wantSlug: "skills-readresource",
			wantID:   FuncIDSkillsReadResource,
			wantTags: []string{toolTagSkills},
		},
		{
			name:     "runscript",
			got:      SkillsRunScriptTool(),
			wantSlug: "skills-runscript",
			wantID:   FuncIDSkillsRunScript,
			wantTags: []string{toolTagSkills, "exec"},
		},
		{
			name:     "unload",
			got:      SkillsUnloadTool(),
			wantSlug: "skills-unload",
			wantID:   FuncIDSkillsUnload,
			wantTags: []string{toolTagSkills},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got.SchemaVersion != llmtoolsgoSpec.SchemaVersion {
				t.Fatalf("schema version mismatch: got=%q want=%q", tt.got.SchemaVersion, llmtoolsgoSpec.SchemaVersion)
			}
			if tt.got.Slug != tt.wantSlug {
				t.Fatalf("slug mismatch: got=%q want=%q", tt.got.Slug, tt.wantSlug)
			}
			if tt.got.GoImpl.FuncID != tt.wantID {
				t.Fatalf("funcID mismatch: got=%q want=%q", tt.got.GoImpl.FuncID, tt.wantID)
			}
			if tt.got.ID == "" || tt.got.DisplayName == "" || tt.got.Description == "" {
				t.Fatalf("expected populated metadata: %+v", tt.got)
			}
			if string(tt.got.ArgSchema) == "" {
				t.Fatalf("expected arg schema for %s", tt.name)
			}
			if tt.got.Version != toolVersionOne {
				t.Fatalf("version mismatch: got=%q want=%q", tt.got.Version, toolVersionOne)
			}
			if tt.got.CreatedAt != llmtoolsgoSpec.SchemaStartTime ||
				tt.got.ModifiedAt != llmtoolsgoSpec.SchemaStartTime {
				t.Fatalf(
					"expected schema timestamps to use SchemaStartTime, got=%v/%v",
					tt.got.CreatedAt,
					tt.got.ModifiedAt,
				)
			}
			if len(tt.got.Tags) != len(tt.wantTags) {
				t.Fatalf("tags length mismatch: got=%v want=%v", tt.got.Tags, tt.wantTags)
			}
			for i := range tt.wantTags {
				if tt.got.Tags[i] != tt.wantTags[i] {
					t.Fatalf("tags mismatch: got=%v want=%v", tt.got.Tags, tt.wantTags)
				}
			}
		})
	}
}
