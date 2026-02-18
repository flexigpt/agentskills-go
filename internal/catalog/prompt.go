package catalog

import (
	"slices"

	"github.com/flexigpt/agentskills-go/spec"
)

// PromptFilter is used for LLM prompt listing.
type PromptFilter struct {
	Types          []string
	LLMNamePrefix  string
	LocationPrefix string

	// AllowDefs restricts to an explicit allowlist of host/lifecycle definitions. Empty means "all".
	AllowDefs []spec.SkillDef
}

func (f PromptFilter) match(e *entry) bool {
	if e == nil {
		return false
	}
	if len(f.AllowDefs) > 0 && !slices.Contains(f.AllowDefs, e.def) {
		return false
	}

	if len(f.Types) > 0 {
		ok := slices.Contains(f.Types, e.idx.Key.Type)

		if !ok {
			return false
		}
	}
	if f.LLMNamePrefix != "" && len(e.llmName) >= len(f.LLMNamePrefix) {
		if e.llmName[:len(f.LLMNamePrefix)] != f.LLMNamePrefix {
			return false
		}
	} else if f.LLMNamePrefix != "" {
		return false
	}

	if f.LocationPrefix != "" && len(e.def.Location) >= len(f.LocationPrefix) {
		if e.def.Location[:len(f.LocationPrefix)] != f.LocationPrefix {
			return false
		}
	} else if f.LocationPrefix != "" {
		return false
	}

	return true
}

// UserFilter is used for host/lifecycle listing.
type UserFilter struct {
	Types          []string
	NamePrefix     string
	LocationPrefix string
	AllowDefs      []spec.SkillDef
}

func (f UserFilter) match(e *entry) bool {
	if e == nil {
		return false
	}
	if len(f.AllowDefs) > 0 && !slices.Contains(f.AllowDefs, e.def) {
		return false
	}
	if len(f.Types) > 0 && !slices.Contains(f.Types, e.idx.Key.Type) {
		return false
	}
	if f.NamePrefix != "" && len(e.def.Name) >= len(f.NamePrefix) {
		if e.def.Name[:len(f.NamePrefix)] != f.NamePrefix {
			return false
		}
	} else if f.NamePrefix != "" {
		return false
	}
	if f.LocationPrefix != "" && len(e.def.Location) >= len(f.LocationPrefix) {
		if e.def.Location[:len(f.LocationPrefix)] != f.LocationPrefix {
			return false
		}
	} else if f.LocationPrefix != "" {
		return false
	}
	return true
}
