package agentskills

import (
	"context"
	"errors"
	"log/slog"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

// SkillsRuntime holds an in-memory registry of skills loaded from directories.
// It is safe for concurrent use by multiple goroutines.
type SkillsRuntime struct {
	maxActivePerSession int
	logger              *slog.Logger
}

// Option is a functional option for configuring a Runtime.
type Option func(*SkillsRuntime) error

// WithMaxActivePerSession sets a soft upper bound on concurrently active
// skills per SessionState. Zero or negative means "no explicit limit".
func WithMaxActivePerSession(n int) Option {
	return func(r *SkillsRuntime) error {
		r.maxActivePerSession = n
		return nil
	}
}

// WithLogger sets the logger used by the Runtime.
func WithLogger(l *slog.Logger) Option {
	return func(r *SkillsRuntime) error {
		r.logger = l
		return nil
	}
}

// NewSkillsRuntime constructs an empty Runtime with the given options.
// No skills are loaded initially; callers add/remove skills explicitly
// via AddSkillDir/RemoveSkill.
func NewSkillsRuntime(opts ...Option) (*SkillsRuntime, error) {
	r := &SkillsRuntime{}
	for _, o := range opts {
		if o != nil {
			e := o(r)
			if e != nil {
				return nil, errors.New("invalid skills runtime options")
			}
		}
	}
	return r, nil
}

// AddSkillDir loads a single skill from the given directory.
//
// The directory must contain exactly one SKILL.md file at its root;
// nested scanning is not performed. Callers that want to discover
// multiple skills under a tree should locate individual skill dirs
// themselves and call AddSkillDir once per dir.
//
// On success it returns the loaded Skill. If a skill with the same Name
// already exists, ErrSkillAlreadyExists is returned.
func (s *SkillsRuntime) AddSkillDir(ctx context.Context, dir string) (Skill, error) {
	return Skill{}, nil
}

// RemoveSkill removes a skill by name from the runtime.
//
// If the skill does not exist, ErrSkillNotFound is returned.
// The removed Skill (if any) is returned for convenience.
func (s *SkillsRuntime) RemoveSkill(name string) (Skill, error) {
	return Skill{}, nil
}

// ListSkills returns a snapshot of all skills currently registered in
// the runtime, sorted by Name.
func (s *SkillsRuntime) ListSkills() ([]Skill, error) {
	return []Skill{}, nil
}

// GetSkill returns the skill with the given name, if present.
func (s *SkillsRuntime) GetSkill(name string) (Skill, error) {
	return Skill{}, nil
}

// Load applies a LoadArgs to the given SessionState and returns both the
// resulting state and a structured description of the active skills.
//
// It enforces Runtime-level constraints such as MaxActivePerSession.
// If any requested skill name does not exist in the Runtime, an error
// is returned and the SessionState is not changed.
func (s *SkillsRuntime) Load(
	ctx context.Context,
	sess SessionState,
	args LoadArgs,
) (result LoadResult, newState SessionState, err error) {
	return LoadResult{}, SessionState{}, nil
}

// Unload applies an UnloadArgs to the given SessionState and returns
// both the resulting state and an updated description of the active skills.
//
// If All is true, all skills are deactivated. If All is false and Names
// is empty, Unload returns an error.
func (s *SkillsRuntime) Unload(
	ctx context.Context,
	sess SessionState,
	args UnloadArgs,
) (result UnloadResult, newState SessionState, err error) {
	return UnloadResult{}, SessionState{}, nil
}

// ReadFile reads a file or resource from a skill's directory and returns one
// or more llmspec.ToolStoreOutputUnion values describing the content.
//
// The exact behavior (text vs binary, PDF extraction, etc.) is intended
// to mirror the semantics of fstool.ReadFile.
//
// If SkillName is empty, ReadFile requires that sess.Active is non-empty;
// otherwise ErrNoActiveSkills is returned. If the named skill does not
// exist in the runtime, or if the path escapes the skill's RootDir,
// an error is returned.
func (s *SkillsRuntime) ReadFile(
	ctx context.Context,
	sess SessionState,
	args ReadArgs,
) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	return []llmtoolsgoSpec.ToolStoreOutputUnion{}, nil
}

// RunScript executes a script associated with a skill and returns its
// outputs as llmspec.ToolStoreOutputUnion values (e.g. a text block with
// stdout/stderr, or a file/image if the script creates artifacts).
//
// If the Runtime was not configured with shell execution support,
// ErrRunScriptUnsupported is returned.
//
// As with Read, if SkillName is empty, the most recently active skill
// is used; ErrNoActiveSkills is returned if there is none. If the named
// skill does not exist or the script path escapes its RootDir, an error
// is returned.
func (s *SkillsRuntime) RunScript(
	ctx context.Context,
	sess SessionState,
	args RunScriptArgs,
) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	return []llmtoolsgoSpec.ToolStoreOutputUnion{}, nil
}

// AvailableSkillsPrompt builds a prompt snippet describing the given
// skills for use in system messages.
//
// If names is nil or empty, all skills in the runtime are included.
// If any name is unknown, an error is returned.
//
// The returned string is intended to be embedded directly into a prompt.
// The exact format (e.g. XML) is defined by this package and is kept
// stable across versions.
func (s *SkillsRuntime) AvailableSkillsPrompt(names []string) (string, error) {
	return "", nil
}

// ActiveSkillsPrompt builds a prompt snippet describing the active skills
// in the given SessionState. Only skills that are currently registered
// in the runtime are included; missing names are ignored.
//
// The returned string is intended to be embedded directly into a prompt.
func (s *SkillsRuntime) ActiveSkillsPrompt(sess SessionState) (string, error) {
	return "", nil
}
