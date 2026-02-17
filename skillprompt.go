package agentskills

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/flexigpt/agentskills-go/internal/catalog"
	"github.com/flexigpt/agentskills-go/spec"
)

// SkillActivity controls whether SkillsPromptXML includes active, inactive, or both sets.
type SkillActivity string

const (
	// SkillActivityAny returns both:
	//   - <activeSkills> (if SessionID is set)
	//   - <availableSkills> (inactive skills if SessionID is set; otherwise all skills)
	SkillActivityAny SkillActivity = "any"

	// SkillActivityActive returns only <activeSkills>. Requires SessionID.
	SkillActivityActive SkillActivity = "active"

	// SkillActivityInactive returns only <availableSkills> for inactive skills. If SessionID
	// is empty, all skills are treated as inactive.
	SkillActivityInactive SkillActivity = "inactive"
)

// SkillFilter is an optional filter for listing/prompting skills.
//
// Semantics:
//   - Types/NamePrefix/LocationPrefix/AllowKeys always apply.
//   - SessionID (optional) allows filtering/annotating by "active in this session".
//   - Activity controls whether to include active, inactive, or both.
//
// Defaults:
//   - Activity defaults to "any".
//   - If SessionID is empty, no active skills exist.
type SkillFilter struct {
	// Types restricts to provider types (e.g. ["fs"]). Empty means "all".
	Types []string

	// NamePrefix restricts to LLM-visible names with this prefix.
	NamePrefix string

	// LocationPrefix restricts to skills whose base location starts with this prefix.
	LocationPrefix string

	// AllowKeys restricts to an explicit allowlist of skill keys. Empty means "all".
	AllowKeys []spec.SkillKey

	// SessionID optionally scopes active/inactive filtering.
	SessionID spec.SessionID

	// Activity defaults to SkillActivityAny.
	Activity SkillActivity
}

// SkillsPromptXML is the single prompt API.
//
// Output compatibility rules (to preserve previous XML “standards” as much as possible):
//   - If only one section is requested, the root is exactly one of:
//   - <availableSkills>...</availableSkills>
//   - <activeSkills>...</activeSkills>
//     (matching the historical outputs from internal/catalog XML builders).
//   - If both sections are requested simultaneously, the output is wrapped in:
//     <skillsPrompt> ... </skillsPrompt>
func (r *Runtime) SkillsPromptXML(ctx context.Context, f *SkillFilter) (string, error) {
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
	case SkillActivityAny, SkillActivityInactive:
		// OK (SessionID optional).
	case SkillActivityActive:
		if cfg.SessionID == "" {
			return "", fmt.Errorf("%w: activity=active requires sessionID", spec.ErrInvalidArgument)
		}
	default:
		return "", fmt.Errorf("%w: invalid activity %q", spec.ErrInvalidArgument, cfg.Activity)
	}

	// Base catalog filtering (Types/NamePrefix/LocationPrefix/AllowKeys).
	records := r.catalog.ListRecords(toCatalogFilter(&cfg))

	// Resolve session + active set (optional).
	var activeOrder []spec.SkillKey
	activeSet := map[spec.SkillKey]struct{}{}
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

	includeActive := cfg.Activity == SkillActivityAny || cfg.Activity == SkillActivityActive
	includeAvailable := cfg.Activity == SkillActivityAny || cfg.Activity == SkillActivityInactive

	// Active section: preserve historical CDATA encoding by reusing catalog.ActiveSkillsXML.
	var activeXML string

	if includeActive && cfg.SessionID != "" {
		// Build membership set for "records" (filtered catalog view), so active section
		// respects the same filters.
		filtered := make(map[spec.SkillKey]struct{}, len(records))
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
		var err error
		activeXML, err = catalog.ActiveSkillsXML(items)
		if err != nil {
			return "", err
		}
	} else if cfg.Activity == SkillActivityActive {
		// Emit an empty <activeSkills>...</activeSkills> section for "active" queries.
		var err error
		activeXML, err = catalog.ActiveSkillsXML(nil)
		if err != nil {
			return "", err
		}
	}

	// Available section: reuse catalog.AvailableSkillsXML to preserve tag structure + ordering.
	var availableXML string
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
		var err error
		availableXML, err = catalog.AvailableSkillsXML(items)
		if err != nil {
			return "", err
		}
	}

	// If only one section is requested, return it as the root (backward-compatible structure).
	if strings.TrimSpace(activeXML) == "" && strings.TrimSpace(availableXML) != "" {
		return availableXML, nil
	}
	if strings.TrimSpace(availableXML) == "" && strings.TrimSpace(activeXML) != "" {
		return activeXML, nil
	}

	// Otherwise wrap both sections into one well-formed document.
	return wrapSkillsPromptXML(availableXML, activeXML), nil
}

func normalizeSkillsPromptFilter(f *SkillFilter) SkillFilter {
	if f == nil {
		return SkillFilter{Activity: SkillActivityAny}
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

	allow := make([]spec.SkillKey, 0, len(f.AllowKeys))
	seenK := map[spec.SkillKey]struct{}{}
	for _, k := range f.AllowKeys {
		// Keep strict equality semantics; just drop obviously empty entries.
		if strings.TrimSpace(k.Type) == "" || strings.TrimSpace(k.Name) == "" || strings.TrimSpace(k.Location) == "" {
			continue
		}
		if _, ok := seenK[k]; ok {
			continue
		}
		seenK[k] = struct{}{}
		allow = append(allow, k)
	}

	act := SkillActivity(strings.TrimSpace(string(f.Activity)))
	if act == "" {
		act = SkillActivityAny
	}

	return SkillFilter{
		Types:          types,
		NamePrefix:     f.NamePrefix,
		LocationPrefix: f.LocationPrefix,
		AllowKeys:      allow,
		SessionID:      spec.SessionID(strings.TrimSpace(string(f.SessionID))),
		Activity:       act,
	}
}

func wrapSkillsPromptXML(parts ...string) string {
	var b strings.Builder
	b.WriteString("<skillsPrompt>")
	wrote := false
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		wrote = true
		b.WriteByte('\n')
		b.WriteString(indentLines(p, "  "))
	}
	if wrote {
		b.WriteByte('\n')
	}
	b.WriteString("</skillsPrompt>")
	return b.String()
}

func indentLines(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func toCatalogFilter(f *SkillFilter) catalog.Filter {
	if f == nil {
		return catalog.Filter{}
	}
	return catalog.Filter{
		Types:          append([]string(nil), f.Types...),
		NamePrefix:     f.NamePrefix,
		LocationPrefix: f.LocationPrefix,
		AllowKeys:      append([]spec.SkillKey(nil), f.AllowKeys...),
	}
}
