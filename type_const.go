// Package agentskills provides a runtime and data types for working with
// agent skills described by SKILL.md files. Skills are discovered from
// directories that are explicitly added to a Runtime at run time.
//
// The package is stateless with respect to per-conversation skills state:
// callers are responsible for storing SessionState between turns.

package agentskills

import (
	"errors"
)

var (
	// ErrSkillNotFound is returned when a skill with the given name does
	// not exist in the runtime.
	ErrSkillNotFound = errors.New("skill not found")

	// ErrSkillAlreadyExists is returned when attempting to add a skill
	// whose Name is already present in the runtime.
	ErrSkillAlreadyExists = errors.New("skill already exists")

	// ErrInvalidSkillDir is returned when a directory does not contain a
	// valid SKILL.md or fails validation.
	ErrInvalidSkillDir = errors.New("invalid skill directory")

	// ErrNoActiveSkills is returned by operations that require at least
	// one active skill when none are present in the SessionState.
	ErrNoActiveSkills = errors.New("no active skills")

	// ErrRunScriptUnsupported is returned if RunScript is called on a
	// Runtime that was not configured with shell execution support.
	ErrRunScriptUnsupported = errors.New("run_script unsupported")
)

// Skill represents a single skill as loaded from a SKILL.md file.
// It is a pure data object; Runtime methods operate on these values
// but Skill itself has no behavior.
type Skill struct {
	// Name is the unique identifier for the skill within a Runtime.
	// It typically comes from the SKILL.md frontmatter.
	Name string `json:"name"`

	// Description is a short human-readable summary of the skill.
	Description string `json:"description"`

	// RootDir is the absolute path to the directory that contains the
	// SKILL.md and all resources/scripts for this skill.
	RootDir string `json:"root_dir"`

	// Location is the absolute path to the SKILL.md file.
	Location string `json:"location"`

	// Digest is an optional hash (e.g. SHA-256 hex) of the SKILL.md
	// contents. It can be used for cache invalidation/integrity checks.
	Digest string `json:"digest,omitempty"`

	// Properties contains parsed frontmatter and additional metadata
	// for the skill. Keys/value types are skill-convention specific
	// (license, version, resources, scripts, etc.).
	Properties map[string]any `json:"properties,omitempty"`

	// Body is the markdown body of SKILL.md after frontmatter (the
	// natural-language instructions for the skill).
	Body string `json:"body"`
}

// SessionState represents the skills-related state for one conversation
// or agent session. Callers must persist it between turns and pass it
// back into Runtime methods.
//
// The runtime never stores SessionState internally; all methods that
// change it return a new value.
type SessionState struct {
	// Active holds the ordered list of active skill names, with the
	// most recently activated skill at the end of the slice.
	Active []string `json:"active"`

	// Digests optionally records the SKILL.md digest for each active
	// skill at the time it was loaded. Callers may use this to detect
	// when a skill definition has changed.
	Digests map[string]string `json:"digests,omitempty"`
}

// LoadMode controls how Load applies the requested names
// to the existing SessionState.
type LoadMode string

const (
	// LoadModeAdd appends the requested skills to the existing Active
	// list, moving any duplicates to the end.
	LoadModeAdd LoadMode = "add"

	// LoadModeReplace replaces the Active list entirely with exactly
	// the requested skills (in the given order).
	LoadModeReplace LoadMode = "replace"
)

// LoadArgs describes a request to activate one or more skills.
type LoadArgs struct {
	// Names are the skill names to activate. Each must exist in the
	// Runtime; otherwise Load returns an error.
	Names []string `json:"names"`

	// Mode controls whether to add to or replace the current Active list.
	// If empty, LoadModeAdd is used.
	Mode LoadMode `json:"mode,omitempty"`
}

// SkillRef summarizes a skill in a Load/Unload result.
type SkillRef struct {
	Name       string         `json:"name"`
	RootDir    string         `json:"root_dir"`
	Location   string         `json:"location"`
	Digest     string         `json:"digest,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

// LoadResult is returned by Load when applying a LoadArgs to a SessionState.
type LoadResult struct {
	Active []SkillRef `json:"active_skills"`
}

// UnloadArgs describes a request to deactivate one or more skills.
type UnloadArgs struct {
	// Names are the skill names to remove from the Active list.
	// They are ignored if All is true.
	Names []string `json:"names,omitempty"`

	// All, if true, clears the Active list regardless of Names.
	All bool `json:"all,omitempty"`
}

// UnloadResult is returned by Unload after applying an UnloadArgs.
type UnloadResult struct {
	Active []SkillRef `json:"active_skills"`
}

// ReadEncoding mirrors the encoding choices of fstool.ReadFile.
type ReadEncoding string

const (
	ReadEncodingText   ReadEncoding = "text"
	ReadEncodingBinary ReadEncoding = "binary"
)

// ReadArgs describes a request to read a resource from a skill.
type ReadArgs struct {
	// SkillName is the name of the skill to read from. If empty, the
	// most recently active skill in sess.Active is used.
	SkillName string `json:"skill,omitempty"`

	// Path is a relative path under the skill's RootDir pointing to
	// the resource to read.
	Path string `json:"path"`

	// Encoding controls how the resource is interpreted. If empty,
	// ReadEncodingText is used.
	Encoding ReadEncoding `json:"encoding,omitempty"`
}

// RunScriptArgs describes a request to execute a script belonging to a skill.
type RunScriptArgs struct {
	// SkillName is the name of the skill whose script should be executed.
	// If empty, the most recently active skill in sess.Active is used.
	SkillName string `json:"skill,omitempty"`

	// Script is the relative path to the script within the skill's
	// RootDir (e.g. "scripts/cleanup.sh").
	Script string `json:"script"`

	// Args are the command-line arguments passed to the script.
	Args []string `json:"args,omitempty"`

	// Env contains environment variable overrides for the script process.
	Env map[string]string `json:"env,omitempty"`

	// Workdir, if non-empty, overrides the working directory for the
	// script. If empty, a default derived from the skill's RootDir
	// will be used.
	Workdir string `json:"workdir,omitempty"`
}
