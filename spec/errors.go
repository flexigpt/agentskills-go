package spec

import "errors"

var (
	ErrInvalidArgument      = errors.New("invalid argument")
	ErrSkillNotFound        = errors.New("skill not found")
	ErrSkillAlreadyExists   = errors.New("skill already exists")
	ErrProviderNotFound     = errors.New("provider not found")
	ErrSkillNotActive       = errors.New("skill not active")
	ErrRunScriptUnsupported = errors.New("run_script unsupported")
	ErrSessionNotFound      = errors.New("session not found")
)
