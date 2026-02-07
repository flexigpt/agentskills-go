package catalog

import "slices"

// Filter is used for listing/prompting skills.
type Filter struct {
	Types      []string
	NamePrefix string
	PathPrefix string
}

func (f Filter) match(e *entry) bool {
	if e == nil {
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

	if f.PathPrefix != "" && len(e.rec.Key.Path) >= len(f.PathPrefix) {
		if e.rec.Key.Path[:len(f.PathPrefix)] != f.PathPrefix {
			return false
		}
	} else if f.PathPrefix != "" {
		return false
	}

	return true
}

// AvailableSkillsPromptXML builds <available_skills> XML for system prompts.
func (c *Catalog) AvailableSkillsPromptXML(f Filter) (string, error) {
	entries := c.listEntriesForPrompt(f)

	items := make([]AvailableSkillItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, AvailableSkillItem{
			Name:        e.llmName,
			Description: e.rec.Description,
			Path:        e.rec.Key.Path,
		})
	}

	return AvailableSkillsXML(items)
}
