package spec

import (
	"context"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

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

// ProviderSkillKey is the INTERNAL canonical identity used by the catalog/session/provider plumbing.
//
// Providers may canonicalize Location (e.g. abs+EvalSymlinks for fs).
// This canonical form MUST NOT be exposed to host/lifecycle APIs.
type ProviderSkillKey struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

// ProviderSkillIndexRecord is the INTERNAL catalog record returned by providers during indexing.
// It carries the canonical ProviderSkillKey used internally.
type ProviderSkillIndexRecord struct {
	Key         ProviderSkillKey `json:"key"`
	Description string           `json:"description"`
	Properties  map[string]any   `json:"properties,omitempty"`
	Digest      string           `json:"digest,omitempty"`
	SkillBody   string           `json:"skillBody,omitempty"` // optional cached body (e.g. SKILL.md without frontmatter)
}

type SkillProvider interface {
	// Type returns the provider type key (e.g. "fs", "s3").
	Type() string

	// Index validates and returns metadata for the skill identified by def.
	// Providers may canonicalize Location in the returned ProviderSkillIndexRecord.Key.
	Index(ctx context.Context, def SkillDef) (ProviderSkillIndexRecord, error)

	// LoadBody returns the prompt-injectable SKILL.md body (frontmatter removed).
	LoadBody(ctx context.Context, key ProviderSkillKey) (string, error)

	// ReadResource reads a resource relative to the skill base location.
	// The meaning/format of resourceLocation is provider-defined (for fs it's typically a relative file path).
	ReadResource(
		ctx context.Context,
		key ProviderSkillKey,
		resourceLocation string,
		encoding ReadResourceEncoding,
	) ([]llmtoolsgoSpec.ToolOutputUnion, error)

	// RunScript executes a script relative to the skill base location.
	// Providers define/enforce constraints (e.g. must be under scripts/).
	RunScript(
		ctx context.Context,
		key ProviderSkillKey,
		scriptLocation string,
		args []string,
		env map[string]string,
		workDir string,
	) (RunScriptOut, error)
}
