package agentskills

import (
	"context"
	"errors"
	"log/slog"
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
