package spec

import "errors"

// Package-level sentinel errors returned by catalog/session/provider operations.
var (
	// ErrInvalidArgument indicates the caller provided an invalid/missing argument.
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrSkillNotFound indicates the requested skill does not exist in the catalog/session.
	ErrSkillNotFound = errors.New("skill not found")

	// ErrSkillAlreadyExists indicates a skill with the same key already exists.
	ErrSkillAlreadyExists = errors.New("skill already exists")

	// ErrProviderNotFound indicates no provider is registered for the requested provider type.
	ErrProviderNotFound = errors.New("provider not found")

	// ErrSkillNotActive indicates the requested skill is not currently active/loaded in the session.
	ErrSkillNotActive = errors.New("skill not active")

	// ErrRunScriptUnsupported indicates the selected provider does not support running scripts.
	ErrRunScriptUnsupported = errors.New("runScript unsupported")

	// ErrSessionNotFound indicates the requested session does not exist.
	ErrSessionNotFound = errors.New("session not found")
)
