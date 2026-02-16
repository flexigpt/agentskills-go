package catalog

import (
	"slices"

	"github.com/flexigpt/agentskills-go/spec"
)

// Filter is used for listing/prompting skills.
type Filter struct {
	Types          []string
	NamePrefix     string
	LocationPrefix string
	// AllowKeys restricts to an explicit allowlist of skill keys. Empty means "all".
	AllowKeys []spec.SkillKey
}

func (f Filter) match(e *entry) bool {
	if e == nil {
		return false
	}
	if len(f.AllowKeys) > 0 && !slices.Contains(f.AllowKeys, e.rec.Key) {
		return false
	}

	if len(f.Types) > 0 {
		ok := slices.Contains(f.Types, e.rec.Key.Type)
		if !ok {
			return false
		}
	}
	if f.NamePrefix != "" && len(e.llmName) >= len(f.NamePrefix) {
		if e.llmName[:len(f.NamePrefix)] != f.NamePrefix {
			return false
		}
	} else if f.NamePrefix != "" {
		return false
	}

	if f.LocationPrefix != "" && len(e.rec.Key.Location) >= len(f.LocationPrefix) {
		if e.rec.Key.Location[:len(f.LocationPrefix)] != f.LocationPrefix {
			return false
		}
	} else if f.LocationPrefix != "" {
		return false
	}

	return true
}
