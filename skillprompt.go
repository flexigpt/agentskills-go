package agentskills

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/flexigpt/agentskills-go/internal/catalog"
	"github.com/flexigpt/agentskills-go/spec"
)

const (
	skillsPromptStart = "<<<SKILLS_PROMPT>>>"
	skillsPromptEnd   = "<<<END_SKILLS_PROMPT>>>"
)

// SkillFilter is an optional filter for listing/prompting skills (LLM/prompt-facing).
//
// Semantics:
//   - Types/NamePrefix/LocationPrefix/AllowSkills always apply.
//   - SessionID (optional) allows filtering/annotating by "active in this session".
//   - Activity controls whether to include active, inactive, or both.
//
// Defaults:
//   - Activity defaults to "any".
//   - If SessionID is empty, no active skills exist.
//
// IMPORTANT CONTRACT:
//   - NamePrefix matches the LLM-visible computed handle name (not the host skill def name).
//   - LocationPrefix matches the user-provided location (not provider-canonicalized).
type SkillFilter struct {
	// Types restricts to provider types (e.g. ["fs"]). Empty means "all".
	Types []string

	// NamePrefix restricts to LLM-visible names with this prefix.
	NamePrefix string

	// LocationPrefix restricts to skills whose (user-provided) location starts with this prefix.
	LocationPrefix string

	// AllowSkills restricts to an explicit allowlist of host/lifecycle skill defs. Empty means "all".
	AllowSkills []spec.SkillDef

	// SessionID optionally scopes active/inactive filtering.
	SessionID spec.SessionID

	// Activity defaults to spec.SkillActivityAny.
	Activity spec.SkillActivity
}

// SkillListFilter is a HOST/LIFECYCLE listing filter.
// Unlike SkillFilter (prompt), NamePrefix applies to the user-provided skill name (def.name),
// not the LLM-visible computed handle name.
type SkillListFilter struct {
	Types          []string
	NamePrefix     string
	LocationPrefix string
	AllowSkills    []spec.SkillDef

	SessionID spec.SessionID
	Activity  spec.SkillActivity
}

// SkillsPrompt builds prompt-facing text for available and/or active skills.
//
// Output rules:
//   - If only one section is requested, the return value is exactly that section.
//   - If both sections are requested, the output is wrapped in:
//     <<<SKILLS_PROMPT>>>
//     ...
//     <<<END_SKILLS_PROMPT>>>
func (r *Runtime) SkillsPrompt(ctx context.Context, f *SkillFilter) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r == nil {
		return "", fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
	}

	cfg := normalizeSkillsPromptFilter(f)

	// Validate activity/session constraints early.
	switch cfg.Activity {
	case spec.SkillActivityAny, spec.SkillActivityInactive:
		// OK (SessionID optional).
	case spec.SkillActivityActive:
		if cfg.SessionID == "" {
			return "", fmt.Errorf("%w: activity=active requires sessionID", spec.ErrInvalidArgument)
		}
	default:
		return "", fmt.Errorf("%w: invalid activity %q", spec.ErrInvalidArgument, cfg.Activity)
	}

	// Base catalog filtering (Types/NamePrefix/LocationPrefix/AllowSkills).
	records := r.catalog.ListPromptIndexRecords(toCatalogPromptFilter(&cfg))

	// Resolve session + active set (optional).
	var activeOrder []spec.ProviderSkillKey
	activeSet := map[spec.ProviderSkillKey]struct{}{}
	if cfg.SessionID != "" {
		s, ok := r.sessions.Get(string(cfg.SessionID))
		if !ok {
			return "", spec.ErrSessionNotFound
		}
		keys, err := s.ActiveKeys(ctx)
		if err != nil {
			return "", err
		}
		activeOrder = keys
		for _, k := range keys {
			activeSet[k] = struct{}{}
		}
	}

	includeActive := cfg.Activity == spec.SkillActivityAny || cfg.Activity == spec.SkillActivityActive
	includeAvailable := cfg.Activity == spec.SkillActivityAny || cfg.Activity == spec.SkillActivityInactive

	// Active section: preserve session active order while respecting the filtered catalog view.
	var activePrompt string

	if includeActive && cfg.SessionID != "" {
		// Build membership set for "records" (filtered catalog view), so active section
		// respects the same filters.
		filtered := make(map[spec.ProviderSkillKey]struct{}, len(records))
		for _, rec := range records {
			filtered[rec.Key] = struct{}{}
		}

		items := make([]catalog.ActiveSkillItem, 0, len(activeOrder))
		for _, k := range activeOrder {
			if _, ok := filtered[k]; !ok {
				continue
			}
			h, ok := r.catalog.HandleForKey(k)
			if !ok {
				continue
			}
			body, err := r.catalog.EnsureBody(ctx, k)
			if err != nil {
				if errors.Is(err, spec.ErrSkillNotFound) {
					continue
				}
				return "", err
			}
			items = append(items, catalog.ActiveSkillItem{Name: h.Name, Body: body})
		}

		activePrompt = catalog.ActiveSkillsPrompt(items)
	} else if cfg.Activity == spec.SkillActivityActive {
		activePrompt = catalog.ActiveSkillsPrompt(nil)
	}

	// Available section: prompt-visible metadata only. With SessionID set, "available" means inactive.
	var availablePrompt string
	if includeAvailable {
		items := make([]catalog.AvailableSkillItem, 0, len(records))
		for _, rec := range records {
			if cfg.SessionID != "" {
				if _, isActive := activeSet[rec.Key]; isActive {
					// With session + any/inactive, "available" means inactive.
					continue
				}
			}
			h, ok := r.catalog.HandleForKey(rec.Key)
			if !ok {
				continue
			}
			items = append(items, catalog.AvailableSkillItem{
				Name:        h.Name,
				Description: rec.Description,
				Location:    h.Location,
			})
		}

		availablePrompt = catalog.AvailableSkillsPrompt(items)

	}

	// If only one section is requested, return it as the root (backward-compatible structure).
	if strings.TrimSpace(activePrompt) == "" && strings.TrimSpace(availablePrompt) != "" {
		return availablePrompt, nil
	}
	if strings.TrimSpace(availablePrompt) == "" && strings.TrimSpace(activePrompt) != "" {
		return activePrompt, nil
	}

	// Otherwise wrap both sections into one well-formed document.
	return wrapSkillsPrompt(availablePrompt, activePrompt), nil
}

func normalizeSkillListFilter(f *SkillListFilter) SkillListFilter {
	if f == nil {
		return SkillListFilter{Activity: spec.SkillActivityAny}
	}
	types := make([]string, 0, len(f.Types))
	seenT := map[string]struct{}{}
	for _, t := range f.Types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seenT[t]; ok {
			continue
		}
		seenT[t] = struct{}{}
		types = append(types, t)
	}
	allow := make([]spec.SkillDef, 0, len(f.AllowSkills))
	seenD := map[spec.SkillDef]struct{}{}
	for _, d := range f.AllowSkills {
		if strings.TrimSpace(d.Type) == "" || strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Location) == "" {
			continue
		}
		if _, ok := seenD[d]; ok {
			continue
		}
		seenD[d] = struct{}{}
		allow = append(allow, d)
	}
	act := spec.SkillActivity(strings.TrimSpace(string(f.Activity)))
	if act == "" {
		act = spec.SkillActivityAny
	}
	return SkillListFilter{
		Types:          types,
		NamePrefix:     f.NamePrefix,
		LocationPrefix: f.LocationPrefix,
		AllowSkills:    allow,
		SessionID:      spec.SessionID(strings.TrimSpace(string(f.SessionID))),
		Activity:       act,
	}
}

func toCatalogUserFilter(f *SkillListFilter) catalog.UserFilter {
	if f == nil {
		return catalog.UserFilter{}
	}
	return catalog.UserFilter{
		Types:          append([]string(nil), f.Types...),
		NamePrefix:     f.NamePrefix,
		LocationPrefix: f.LocationPrefix,
		AllowDefs:      append([]spec.SkillDef(nil), f.AllowSkills...),
	}
}

func normalizeSkillsPromptFilter(f *SkillFilter) SkillFilter {
	if f == nil {
		return SkillFilter{Activity: spec.SkillActivityAny}
	}

	types := make([]string, 0, len(f.Types))
	seen := map[string]struct{}{}
	for _, t := range f.Types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		types = append(types, t)
	}

	allow := make([]spec.SkillDef, 0, len(f.AllowSkills))
	seenD := map[spec.SkillDef]struct{}{}
	for _, d := range f.AllowSkills {
		// Keep strict equality semantics; just drop obviously empty entries.
		if strings.TrimSpace(d.Type) == "" || strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Location) == "" {
			continue
		}
		if _, ok := seenD[d]; ok {
			continue
		}
		seenD[d] = struct{}{}
		allow = append(allow, d)
	}

	act := spec.SkillActivity(strings.TrimSpace(string(f.Activity)))
	if act == "" {
		act = spec.SkillActivityAny
	}

	return SkillFilter{
		Types:          types,
		NamePrefix:     f.NamePrefix,
		LocationPrefix: f.LocationPrefix,
		AllowSkills:    allow,
		SessionID:      spec.SessionID(strings.TrimSpace(string(f.SessionID))),
		Activity:       act,
	}
}

func wrapSkillsPrompt(parts ...string) string {
	var b strings.Builder
	b.WriteString(skillsPromptStart)
	wrote := false
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		wrote = true
		b.WriteByte('\n')
		b.WriteString(p)
	}
	if wrote {
		b.WriteByte('\n')
	}
	b.WriteString(skillsPromptEnd)
	return b.String()
}

func toCatalogPromptFilter(f *SkillFilter) catalog.PromptFilter {
	if f == nil {
		return catalog.PromptFilter{}
	}
	return catalog.PromptFilter{
		Types:          append([]string(nil), f.Types...),
		LLMNamePrefix:  f.NamePrefix,
		LocationPrefix: f.LocationPrefix,
		AllowDefs:      append([]spec.SkillDef(nil), f.AllowSkills...),
	}
}
