package spec

// SessionID identifies a runtime session (UUIDv7 string).
type SessionID string

// MaxSkillResourceLocations is the maximum number of provider-defined resource locations
// included in skill metadata. TotalCount still reports the full provider-reported count.
const MaxSkillResourceLocations = 1024

// SkillActivity controls whether SkillsPrompt includes active, inactive, or both sets.
type SkillActivity string

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

// SkillDef is the host/lifecycle-facing skill definition.
//
// This is the ONLY type that should be used in lifecycle events:
// add/remove/list skills and session creation configuration.
//
// Location is the exact user-provided base location string (not canonicalized).
type SkillDef struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

// SkillInsert describes where a rendered SKILL.md body should be inserted by the consumer.
//
// The default is SkillInsertInstructions. This keeps normal Agent Skills behavior:
// a skill body is instruction/context material unless it explicitly opts into user insertion.
type SkillInsert string

const (
	// SkillInsertInstructions means the rendered body is instruction/context material.
	SkillInsertInstructions SkillInsert = "instructions"
	// SkillInsertUserMessage means the rendered body should be placed in the user-message body.
	SkillInsertUserMessage SkillInsert = "user-message"
)

// ParseSkillDocumentOptions controls provider-independent SKILL.md parsing.
type ParseSkillDocumentOptions struct {
	// ExpectedName is an optional source-derived name, such as the containing
	// filesystem directory name. A mismatch triggers a error.
	ExpectedName string `json:"expectedName,omitempty"`
}

// SkillArgument is a named string argument supported by the FlexiGPT skill extension.
//
// Values are intentionally string-only. Consumers may build richer UI validation on top,
// but the runtime only renders strings into the skill body.
type SkillArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
}

// SkillDocument is a materialized, provider-independent SKILL.md document.
//
// RawFrontmatter preserves fields that the runtime does not interpret.
type SkillDocument struct {
	Name         string          `json:"name"`
	DisplayName  string          `json:"displayName,omitempty"`
	Description  string          `json:"description"`
	Insert       SkillInsert     `json:"insert"`
	Arguments    []SkillArgument `json:"arguments,omitempty"`
	Tags         []string        `json:"tags,omitempty"`
	MarkdownBody string          `json:"markdownBody"`

	RawFrontmatter map[string]any `json:"rawFrontmatter,omitempty"`
}

// SkillResourceInfo describes additional provider-defined resources associated with a skill.
//
// Locations are provider-defined values intended to be passed back as resourceLocation
// to skills-readresource. For an fs provider they are typically slash-separated relative
// file paths, but non-fs providers may use repository paths, embedded resource names,
// object IDs, VM resource handles, or any other provider-defined location format.
type SkillResourceInfo struct {
	// HasResources is true when the provider advertises at least one additional resource.
	HasResources bool `json:"hasResources"`

	// TotalCount is the total number of additional resources found, even when Locations is truncated.
	TotalCount int `json:"totalCount"`

	// Locations contains up to MaxSkillResourceLocations provider-defined resource locations.
	Locations []string `json:"locations,omitempty"`

	// MoreLocations is true when more locations exist than are included in Locations.
	MoreLocations bool `json:"moreLocations"`
}

// RenderSkillOut is returned by Runtime.RenderSkill.
type RenderSkillOut struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	DisplayName string      `json:"displayName,omitempty"`
	Insert      SkillInsert `json:"insert"`

	Tags []string `json:"tags,omitempty"`

	Resources SkillResourceInfo `json:"resources"`

	Text string `json:"text"`

	Arguments        []SkillArgument   `json:"arguments,omitempty"`
	AppliedArguments map[string]string `json:"appliedArguments,omitempty"`

	RawFrontmatter map[string]any `json:"rawFrontmatter,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
}

// RenderSkillBodyResult is the low-level result of rendering declared arguments into a skill body.
type RenderSkillBodyResult struct {
	Text string `json:"text"`

	AppliedArguments    map[string]string `json:"appliedArguments,omitempty"`
	UnknownPlaceholders []string          `json:"unknownPlaceholders,omitempty"`
	Warnings            []string          `json:"warnings,omitempty"`
}

// SkillRecord is the catalog record for a skill.
type SkillRecord struct {
	Def SkillDef `json:"def"`

	Name        string `json:"name"`
	Description string `json:"description"`
	DisplayName string `json:"displayName,omitempty"`

	// Insert is the FlexiGPT insertion hint parsed from SKILL.md.
	// Defaults to "instructions".
	Insert SkillInsert `json:"insert"`

	Arguments []SkillArgument `json:"arguments,omitempty"`

	Tags []string `json:"tags,omitempty"`

	Resources SkillResourceInfo `json:"resources"`

	// RawFrontmatter preserves the parsed SKILL.md YAML frontmatter for callers that want
	// compatibility metadata that this runtime does not interpret.
	RawFrontmatter map[string]any `json:"rawFrontmatter,omitempty"`

	Warnings []string `json:"warnings,omitempty"`

	Digest string `json:"digest,omitempty"`
}
