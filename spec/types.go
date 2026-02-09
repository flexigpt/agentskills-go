package spec

import (
	"context"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

// SessionID identifies a runtime session (UUIDv7 string).
type SessionID string

// SkillKey is the internal/host-facing stable identity for a skill.
//
// Type: provider type key (e.g. "fs", "s3").
// Name: canonical skill name.
// Path: provider-interpreted base path (basePath).
type SkillKey struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// SkillHandle is the LLM-facing selector.
// IMPORTANT: No provider type is included.
type SkillHandle struct {
	// Name is the catalog-computed LLM-visible name (usually the real name;
	// may be disambiguated like "fs:my-skill" when collisions require it).
	Name string `json:"name"`
	Path string `json:"path"`
}

// SkillRecord is the catalog record for a skill.
type SkillRecord struct {
	Key         SkillKey       `json:"key"`
	Description string         `json:"description"`
	Properties  map[string]any `json:"properties,omitempty"`
	Digest      string         `json:"digest,omitempty"`
	SkillMDBody string         `json:"skill_md_body,omitempty"` // optional cached body
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
	ActiveSkills []SkillHandle `json:"active_skills"`
}

type UnloadArgs struct {
	Skills []SkillHandle `json:"skills,omitempty"`
	All    bool          `json:"all,omitempty"`
}

type UnloadOut struct {
	ActiveSkills []SkillHandle `json:"active_skills"`
}

type ReadEncoding string

const (
	ReadEncodingText   ReadEncoding = "text"
	ReadEncodingBinary ReadEncoding = "binary"
)

type ReadArgs struct {
	Skill    SkillHandle  `json:"skill"`
	Path     string       `json:"path"`
	Encoding ReadEncoding `json:"encoding,omitempty"` // default: text
}

type RunScriptArgs struct {
	Skill   SkillHandle       `json:"skill"`
	Path    string            `json:"path"` // relative; provider enforces constraints (e.g. scripts/)
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Workdir string            `json:"workdir,omitempty"` // relative; default provider-specific (fs: base root)
}

type RunScriptOut struct {
	Path       string `json:"path"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

type SkillProvider interface {
	// Type returns the provider type key (e.g. "fs", "s3").
	Type() string

	// Index validates and returns metadata for the skill identified by key.
	// Providers may normalize key.Path (e.g. abs + eval symlinks) and return it in SkillRecord.Key.
	Index(ctx context.Context, key SkillKey) (SkillRecord, error)

	// LoadBody returns the prompt-injectable SKILL.md body (frontmatter removed).
	LoadBody(ctx context.Context, key SkillKey) (string, error)

	// ReadResource reads a resource relative to the skill base path.
	ReadResource(
		ctx context.Context,
		key SkillKey,
		resourcePath string,
		encoding ReadEncoding,
	) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error)

	// RunScript executes a script relative to the skill base path.
	// Providers define/enforce constraints (e.g. must be under scripts/).
	RunScript(
		ctx context.Context,
		key SkillKey,
		scriptPath string,
		args []string,
		env map[string]string,
		workdir string,
	) (RunScriptOut, error)
}
