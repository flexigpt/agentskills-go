package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/flexigpt/agentskills-go/document"
	"github.com/flexigpt/agentskills-go/provider"
	"github.com/flexigpt/agentskills-go/runtime/internal/catalog"
	"github.com/flexigpt/agentskills-go/runtime/spec"
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
	AllowSkills []provider.SkillDef

	// SessionID optionally scopes active/inactive filtering.
	SessionID spec.SessionID

	// Activity defaults to SkillActivityAny.
	Activity spec.SkillActivity
}

// SkillListFilter is a HOST/LIFECYCLE listing filter.
// Unlike SkillFilter (prompt), NamePrefix applies to the user-provided skill name (def.name),
// not the LLM-visible computed handle name.
type SkillListFilter struct {
	Types          []string
	NamePrefix     string
	LocationPrefix string
	AllowSkills    []provider.SkillDef

	Inserts []document.SkillInsert

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
		return "", fmt.Errorf("%w: nil context", spec.ErrInvalidRuntimeArgument)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r == nil {
		return "", fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidRuntimeArgument)
	}

	cfg := normalizeSkillsPromptFilter(f)

	// Validate activity/session constraints early.
	switch cfg.Activity {
	case spec.SkillActivityAny, spec.SkillActivityInactive:
		// OK (SessionID optional).
	case spec.SkillActivityActive:
		if cfg.SessionID == "" {
			return "", fmt.Errorf("%w: activity=active requires sessionID", spec.ErrInvalidRuntimeArgument)
		}
	default:
		return "", fmt.Errorf("%w: invalid activity %q", spec.ErrInvalidRuntimeArgument, cfg.Activity)
	}

	// Base catalog filtering (Types/NamePrefix/LocationPrefix/AllowSkills).
	records := r.catalog.ListPromptIndexRecords(toCatalogPromptFilter(&cfg))

	// Resolve session + active set (optional).
	var activeOrder []provider.ProviderSkillKey
	activeSet := map[provider.ProviderSkillKey]struct{}{}
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
		filtered := make(map[provider.ProviderSkillKey]struct{}, len(records))
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
			idx, ok := r.catalog.GetIndex(k)
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
			rendered := document.RenderSkillBody(body, idx.Arguments, nil)
			items = append(items, catalog.ActiveSkillItem{
				Name:      h.Name,
				Body:      rendered.Text,
				Resources: idx.Resources,
			})
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
				Resources:   rec.Resources,
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

type RenderSkillParams struct {
	// Def is the exact host/lifecycle skill definition previously added to the runtime.
	Def provider.SkillDef

	// Arguments are named string values used for $name and {{name}} substitution.
	Arguments map[string]string
}

type RenderSkillOut struct {
	document.RenderSkillDocumentOut

	Resources provider.SkillResourceInfo `json:"resources"`
}

// RenderSkill renders a skill body using the FlexiGPT skill extensions.
//
// This is a HOST/LIFECYCLE API intended for app wrappers and chat UIs. It does not activate
// the skill in a session and it never executes commands from SKILL.md.
func (r *Runtime) RenderSkill(ctx context.Context, p RenderSkillParams) (RenderSkillOut, error) {
	if ctx == nil {
		return RenderSkillOut{}, fmt.Errorf("%w: nil context", spec.ErrInvalidRuntimeArgument)
	}
	if err := ctx.Err(); err != nil {
		return RenderSkillOut{}, err
	}
	if r == nil {
		return RenderSkillOut{}, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidRuntimeArgument)
	}

	def := p.Def
	if strings.TrimSpace(def.Type) != def.Type ||
		strings.TrimSpace(def.Name) != def.Name ||
		strings.TrimSpace(def.Location) != def.Location {
		return RenderSkillOut{}, fmt.Errorf(
			"%w: def fields must not contain leading/trailing whitespace",
			spec.ErrInvalidRuntimeArgument,
		)
	}
	if strings.TrimSpace(def.Type) == "" ||
		strings.TrimSpace(def.Name) == "" ||
		strings.TrimSpace(def.Location) == "" {
		return RenderSkillOut{}, fmt.Errorf(
			"%w: def.type, def.name, and def.location are required",
			spec.ErrInvalidRuntimeArgument,
		)
	}

	key, ok := r.catalog.ResolveDef(def)
	if !ok {
		return RenderSkillOut{}, fmt.Errorf("%w: unknown skill def: %+v", spec.ErrSkillNotFound, def)
	}

	idx, ok := r.catalog.GetIndex(key)
	if !ok {
		return RenderSkillOut{}, spec.ErrSkillNotFound
	}

	body, err := r.catalog.EnsureBody(ctx, key)
	if err != nil {
		return RenderSkillOut{}, err
	}

	rendered := document.RenderSkillBody(body, idx.Arguments, p.Arguments)
	insert, _ := document.NormalizeSkillInsert(idx.Insert)

	warnings := make([]string, 0, len(idx.Warnings)+len(rendered.Warnings))
	warnings = append(warnings, idx.Warnings...)
	warnings = append(warnings, rendered.Warnings...)

	name := idx.Name
	if name == "" {
		name = idx.Key.Name
	}

	return RenderSkillOut{
		RenderSkillDocumentOut: document.RenderSkillDocumentOut{
			Name:             name,
			Description:      idx.Description,
			DisplayName:      idx.DisplayName,
			Insert:           insert,
			Tags:             append([]string(nil), idx.Tags...),
			Text:             rendered.Text,
			Arguments:        append([]document.SkillArgument(nil), idx.Arguments...),
			AppliedArguments: rendered.AppliedArguments,
			RawFrontmatter:   idx.RawFrontmatter,
			Warnings:         warnings,
		},

		Resources: cloneSkillResourceInfo(idx.Resources),
	}, nil
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
	allow := make([]provider.SkillDef, 0, len(f.AllowSkills))
	seenD := map[provider.SkillDef]struct{}{}
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
	inserts := normalizeInsertFilter(f.Inserts)
	act := spec.SkillActivity(strings.TrimSpace(string(f.Activity)))
	if act == "" {
		act = spec.SkillActivityAny
	}
	return SkillListFilter{
		Types:          types,
		NamePrefix:     f.NamePrefix,
		LocationPrefix: f.LocationPrefix,
		AllowSkills:    allow,
		Inserts:        inserts,
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
		AllowDefs:      append([]provider.SkillDef(nil), f.AllowSkills...),
		Inserts:        append([]document.SkillInsert(nil), f.Inserts...),
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

	allow := make([]provider.SkillDef, 0, len(f.AllowSkills))
	seenD := map[provider.SkillDef]struct{}{}
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

func normalizeInsertFilter(in []document.SkillInsert) []document.SkillInsert {
	out := make([]document.SkillInsert, 0, len(in))
	seen := map[document.SkillInsert]struct{}{}
	for _, raw := range in {
		insert, ok := document.NormalizeSkillInsert(raw)
		if !ok {
			continue
		}
		if _, exists := seen[insert]; exists {
			continue
		}
		seen[insert] = struct{}{}
		out = append(out, insert)
	}
	return out
}

func cloneSkillResourceInfo(in provider.SkillResourceInfo) provider.SkillResourceInfo {
	in.Locations = append([]string(nil), in.Locations...)
	return in
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
	// SkillsPrompt is LLM-facing progressive disclosure. Only instruction skills are advertised/loadable here.
	// "insert=user-message" skills are rendered through Runtime.RenderSkill by the host/chat UI instead.
	inserts := []document.SkillInsert{document.SkillInsertInstructions}

	return catalog.PromptFilter{
		Types:          append([]string(nil), f.Types...),
		LLMNamePrefix:  f.NamePrefix,
		LocationPrefix: f.LocationPrefix,
		AllowDefs:      append([]provider.SkillDef(nil), f.AllowSkills...),
		Inserts:        inserts,
	}
}
