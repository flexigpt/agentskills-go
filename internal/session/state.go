package session

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
