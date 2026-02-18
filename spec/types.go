package spec

// SessionID identifies a runtime session (UUIDv7 string).
type SessionID string

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

// SkillRecord is the catalog record for a skill.
type SkillRecord struct {
	Def         SkillDef       `json:"def"`
	Description string         `json:"description"`
	Properties  map[string]any `json:"properties,omitempty"`
	Digest      string         `json:"digest,omitempty"`
}
