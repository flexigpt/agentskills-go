package spec

import (
	"errors"

	"github.com/flexigpt/agentskills-go/document"
	"github.com/flexigpt/agentskills-go/provider"
)

var (
	ErrInvalidRuntimeArgument = errors.New("invalid argument")
	ErrSkillNotFound          = errors.New("skill not found")
	ErrSkillAlreadyExists     = errors.New("skill already exists")
	ErrProviderNotFound       = errors.New("provider not found")
	ErrSkillNotActive         = errors.New("skill not active")
	ErrSessionNotFound        = errors.New("session not found")
	ErrSkillNotAllowed        = errors.New("skill not allowed")
)

const (
	skillsRulesCommon = `
Rules:
1) Only use skills that are listed in the provided skills prompt.
2) Prefer reading advertised skill resource locations with skills-readresource before running scripts.
3) After calling skills-load or skills-unload, rely on the updated skills context in subsequent turns.`

	skillsToolsBase = `You have access to "skills" tools:
- skills-load
- skills-unload
- skills-readresource`

	skillsToolsLoadOnly = `You have access to tool "skills-load". After you load at least one skill, more skills tools may be available.`

	skillsToolsAllWithRunScript = skillsToolsBase + "\n" + "- skills-runscript"
)

const (
	SkillsRulesPromptLoadOnly = skillsToolsLoadOnly + "\n" + skillsRulesCommon

	SkillsRulesPromptWithoutRunScript = skillsToolsBase + "\n" + skillsRulesCommon

	SkillsRulesPromptAll = skillsToolsAllWithRunScript + "\n" + skillsRulesCommon
)

const (
	toolTagSkills  = "skills"
	toolVersionOne = "v1.0.0"
)

type (

	// SessionID identifies a runtime session (UUIDv7 string).
	SessionID string

	// SkillActivity controls whether SkillsPrompt includes active, inactive, or both sets.
	SkillActivity string
)

const (
	// SkillActivityAny returns both:
	//   - <activeSkills> (if SessionID is set)
	//   - <availableSkills> (inactive skills if SessionID is set; otherwise all skills)
	SkillActivityAny SkillActivity = "any"

	// SkillActivityActive returns only <activeSkills>. Requires SessionID.
	SkillActivityActive SkillActivity = "active"

	// SkillActivityInactive returns only <availableSkills> for inactive skills. If SessionID
	// is empty, all skills are treated as inactive.
	SkillActivityInactive SkillActivity = "inactive"
)

// SkillHandle is the LLM-facing selector for a skill.
//
// IMPORTANT CONTRACT:
//   - This is ONLY for LLM prompt/tooling APIs (load/read/run/unload).
//   - It MUST NOT be used for host/lifecycle operations (add/remove/list).
//   - It MUST NOT leak internal canonicalization.
//
// Name is computed by the catalog (may include an opaque suffix to disambiguate).
// Location is the user-provided base location string as registered (not canonicalized).
type SkillHandle struct {
	// Name is the catalog-computed LLM-visible name (usually the real name;
	// may be disambiguated with an opaque suffix like "my-skill#1a2b3c4d").
	Name string `json:"name"`

	// Location is the user-provided and provider-interpreted base location for the skill.
	Location string `json:"location"`
}

// SkillRecord is the catalog record for a skill.
type SkillRecord struct {
	Def provider.SkillDef `json:"def"`

	Name        string `json:"name"`
	Description string `json:"description"`
	DisplayName string `json:"displayName,omitempty"`

	// Insert is the FlexiGPT insertion hint parsed from SKILL.md.
	// Defaults to "instructions".
	Insert document.SkillInsert `json:"insert"`

	Arguments []document.SkillArgument `json:"arguments,omitempty"`

	Tags []string `json:"tags,omitempty"`

	Resources provider.SkillResourceInfo `json:"resources"`

	// RawFrontmatter preserves the parsed SKILL.md YAML frontmatter for callers that want
	// compatibility metadata that this runtime does not interpret.
	RawFrontmatter map[string]any `json:"rawFrontmatter,omitempty"`

	Warnings []string `json:"warnings,omitempty"`

	Digest string `json:"digest,omitempty"`
}

// LoadMode controls how skills-load updates the active list.
type LoadMode string

const (
	LoadModeReplace LoadMode = "replace"
	LoadModeAdd     LoadMode = "add"
)

type LoadArgs struct {
	Skills []SkillHandle `json:"skills"`
	Mode   LoadMode      `json:"mode,omitempty"` // default: replace
}

type LoadOut struct {
	ActiveSkills []SkillHandle `json:"activeSkills"`
}

type UnloadArgs struct {
	Skills []SkillHandle `json:"skills,omitempty"`
	All    bool          `json:"all,omitempty"`
}

type UnloadOut struct {
	ActiveSkills []SkillHandle `json:"activeSkills"`
}
