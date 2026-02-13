package spec

import (
	"context"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

// SessionID identifies a runtime session (UUIDv7 string).
type SessionID string

// SkillHandle is the LLM-facing selector for a skill.
// It is intentionally small and stable: (name + location).
//
// Location is provider-interpreted. Examples:
//   - fs provider: absolute or normalized base directory path
//   - s3 provider: s3://bucket/prefix
type SkillHandle struct {
	// Name is the catalog-computed LLM-visible name (usually the real name;
	// may be disambiguated like "fs:my-skill" when collisions require it).
	Name string `json:"name"`

	// Location is the provider-interpreted base location for the skill.
	Location string `json:"location"`
}

// SkillKey is the internal/host-facing stable identity for a skill.
//
// Type: provider type key (e.g. "fs", "s3").
// Name: canonical skill name.
// Location: provider-interpreted base location (e.g. base dir for fs).
type SkillKey struct {
	SkillHandle

	Type string `json:"type"`
}

// SkillRecord is the catalog record for a skill.
type SkillRecord struct {
	Key         SkillKey       `json:"key"`
	Description string         `json:"description"`
	Properties  map[string]any `json:"properties,omitempty"`
	Digest      string         `json:"digest,omitempty"`
	SkillBody   string         `json:"skillBody,omitempty"` // optional cached body (e.g. SKILL.md without frontmatter)
}

// LoadMode controls how skills.load updates the active list.
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

type ReadResourceEncoding string

const (
	ReadResourceEncodingText   ReadResourceEncoding = "text"
	ReadResourceEncodingBinary ReadResourceEncoding = "binary"
)

type ReadResourceArgs struct {
	SkillName     string `json:"skillName"`
	SkillLocation string `json:"skillLocation"`

	// ResourceLocation is provider-defined; for fs providers this is typically a relative file path.
	ResourceLocation string `json:"resourceLocation"`

	Encoding ReadResourceEncoding `json:"encoding,omitempty"` // default: text
}

type RunScriptArgs struct {
	SkillName     string `json:"skillName"`
	SkillLocation string `json:"skillLocation"`

	// ScriptLocation is provider-defined; for fs providers this is typically a relative script file path.
	ScriptLocation string `json:"scriptLocation"`

	Args []string          `json:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty"`

	// WorkDir is provider-defined; for fs providers this is typically a relative directory under the skill base.
	WorkDir string `json:"workDir,omitempty"`
}

type RunScriptOut struct {
	Location   string `json:"location"`
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	TimedOut   bool   `json:"timedOut,omitempty"`
	DurationMS int64  `json:"durationMS,omitempty"`
}

type SkillProvider interface {
	// Type returns the provider type key (e.g. "fs", "s3").
	Type() string

	// Index validates and returns metadata for the skill identified by key.
	// Providers may normalize key.Location (e.g. abs + eval symlinks for fs) and return it in SkillRecord.Key.
	Index(ctx context.Context, key SkillKey) (SkillRecord, error)

	// LoadBody returns the prompt-injectable SKILL.md body (frontmatter removed).
	LoadBody(ctx context.Context, key SkillKey) (string, error)

	// ReadResource reads a resource relative to the skill base location.
	// The meaning/format of resourceLocation is provider-defined (for fs it's typically a relative file path).
	ReadResource(
		ctx context.Context,
		key SkillKey,
		resourceLocation string,
		encoding ReadResourceEncoding,
	) ([]llmtoolsgoSpec.ToolOutputUnion, error)

	// RunScript executes a script relative to the skill base location.
	// Providers define/enforce constraints (e.g. must be under scripts/).
	RunScript(
		ctx context.Context,
		key SkillKey,
		scriptLocation string,
		args []string,
		env map[string]string,
		workDir string,
	) (RunScriptOut, error)
}
