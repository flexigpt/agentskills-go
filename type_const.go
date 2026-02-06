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
