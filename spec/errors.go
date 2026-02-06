package spec

import "errors"

var (
	ErrSkillNotFound        = errors.New("skill not found")
	ErrSkillAlreadyExists   = errors.New("skill already exists")
	ErrInvalidSkillDir      = errors.New("invalid skill directory")
	ErrNoActiveSkills       = errors.New("no active skills")
	ErrRunScriptUnsupported = errors.New("run_script unsupported")
	ErrSessionNotFound      = errors.New("session not found")
)
