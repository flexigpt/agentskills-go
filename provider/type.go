package provider

import (
	"context"
	"errors"

	"github.com/flexigpt/agentskills-go/document"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

var (
	// ErrInvalidProviderArgument indicates the caller provided an invalid/missing argument.
	ErrInvalidProviderArgument = errors.New("invalid provider argument")

	// ErrRunScriptUnsupported indicates the selected provider does not support running scripts.
	ErrRunScriptUnsupported = errors.New("runScript unsupported")
)

const (
	MaxSkillResourceLocations = 1024
)

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

type ReadResourceEncoding string

const (
	ReadResourceEncodingText   ReadResourceEncoding = "text"
	ReadResourceEncodingBinary ReadResourceEncoding = "binary"
)

type RunScriptOut struct {
	Location   string `json:"location"`
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	TimedOut   bool   `json:"timedOut,omitempty"`
	DurationMS int64  `json:"durationMS,omitempty"`
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

// ProviderSkillKey is the provider/runtime canonical identity.
//
// It is public because custom provider implementations must create and consume
// it. It is not a host identity or an LLM-facing skill handle.
type ProviderSkillKey struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

// ProviderSkillIndexRecord is the provider-to-runtime indexing result.
//
// Name must equal the input SkillDef.Name. Key may canonicalize Location but
// must preserve the input SkillDef.Type and SkillDef.Name.
type ProviderSkillIndexRecord struct {
	Key ProviderSkillKey `json:"key"`

	Name        string `json:"name"`
	Description string `json:"description"`
	DisplayName string `json:"displayName,omitempty"`

	// Insert is parsed from SKILL.md frontmatter field "insert".
	// Missing/empty defaults to "instructions".
	Insert document.SkillInsert `json:"insert"`

	Arguments []document.SkillArgument `json:"arguments,omitempty"`

	Tags []string `json:"tags,omitempty"`

	Resources SkillResourceInfo `json:"resources"`

	RawFrontmatter map[string]any `json:"rawFrontmatter,omitempty"`

	Warnings []string `json:"warnings,omitempty"`

	Digest    string `json:"digest,omitempty"`
	SkillBody string `json:"skillBody,omitempty"` // optional cached body (e.g. SKILL.md without frontmatter)
}

type SkillProvider interface {
	// Type returns the provider type key (e.g. "fs", "s3").
	Type() string

	// Index validates and returns metadata for the skill identified by def.
	// Providers may canonicalize Location in the returned ProviderSkillIndexRecord.Key.
	Index(ctx context.Context, def SkillDef) (ProviderSkillIndexRecord, error)

	// SupportsRunScript reports whether this provider permits script execution
	// under its current immutable configuration.
	SupportsRunScript() bool

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

	// RunScript executes a provider-scoped script identified by scriptLocation.
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
