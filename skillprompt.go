package agentskills

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
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

type skillsPromptXML struct {
	XMLName   xml.Name            `xml:"skillsPrompt"` //nolint:tagliatelle // XML Specific.
	Available *availableSkillsXML `xml:"availableSkills,omitempty"`
	Active    *activeSkillsXML    `xml:"activeSkills,omitempty"`
}

type availableSkillsXML struct {
	Skills []availableSkillItemXML `xml:"skill"` //nolint:tagliatelle // XML Specific.
}

type availableSkillItemXML struct {
	Name        string `xml:"name"`        //nolint:tagliatelle // XML Specific.
	Description string `xml:"description"` //nolint:tagliatelle // XML Specific.
	Location    string `xml:"location"`    //nolint:tagliatelle // XML Specific.
}

type activeSkillsXML struct {
	Skills []activeSkillItemXML `xml:"skill"` //nolint:tagliatelle // XML Specific.
}

// NOTE: We intentionally avoid CDATA. "xml.Marshal" will escape markup safely and avoids the "]]>" CDATA terminator
// edge-case.
type activeSkillItemXML struct {
	Name string `xml:"name,attr"` //nolint:tagliatelle // XML Specific.
	Body string `xml:",chardata"`
}

// SkillsPromptXML is the single prompt API.
//
// Breaking change:
//   - Replaces Runtime.AvailableSkillsPromptXML and Runtime.ActiveSkillsPromptXML.
//   - Produces one XML document containing <availableSkills> and/or <activeSkills>.
func (r *Runtime) SkillsPromptXML(ctx context.Context, f *SkillFilter) (string, error) {
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

	// Build a catalog-filtered record set (this applies Types/NamePrefix/LocationPrefix/AllowKeys).
	records := r.catalog.ListRecords(toCatalogFilter(&SkillFilter{
		Types:          cfg.Types,
		NamePrefix:     cfg.NamePrefix,
		LocationPrefix: cfg.LocationPrefix,
		AllowKeys:      cfg.AllowKeys,
	}))

	// Index by internal key for fast join with session-active keys.
	type recAndHandle struct {
		rec spec.SkillRecord
		h   spec.SkillHandle
	}
	byKey := make(map[spec.SkillKey]recAndHandle, len(records))
	for _, rec := range records {
		h, ok := r.catalog.HandleForKey(rec.Key)
		if !ok {
			// Catalog mutation race: ignore.
			continue
		}
		byKey[rec.Key] = recAndHandle{rec: rec, h: h}
	}

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

	out := skillsPromptXML{}

	// Active section (activation order).
	if includeActive && cfg.SessionID != "" {
		items := make([]activeSkillItemXML, 0, len(activeOrder))
		for _, k := range activeOrder {
			it, ok := byKey[k]
			if !ok {
				// Either filtered out, removed, or not in allowlist.
				continue
			}
			body, err := r.catalog.EnsureBody(ctx, k)
			if err != nil {
				// If the skill disappeared concurrently, skip.
				if errors.Is(err, spec.ErrSkillNotFound) {
					continue
				}
				return "", err
			}
			items = append(items, activeSkillItemXML{
				Name: it.h.Name,
				Body: body,
			})
		}
		out.Active = &activeSkillsXML{Skills: items}
	} else if cfg.Activity == SkillActivityActive {
		// "activity=active" but no session keys (e.g. empty session): still emit the section.
		out.Active = &activeSkillsXML{Skills: nil}
	}

	// Available section (inactive when SessionID is set; otherwise all).
	if includeAvailable {
		items := make([]availableSkillItemXML, 0, len(byKey))
		for k, it := range byKey {
			if cfg.SessionID != "" {
				if _, isActive := activeSet[k]; isActive {
					// When a session is provided and ActivityAny/Inactive, "available" means inactive.
					continue
				}
			}
			items = append(items, availableSkillItemXML{
				Name:        it.h.Name,
				Description: it.rec.Description,
				Location:    it.h.Location,
			})
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Name == items[j].Name {
				return items[i].Location < items[j].Location
			}
			return items[i].Name < items[j].Name
		})
		out.Available = &availableSkillsXML{Skills: items}
	}

	b, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("xml encode: %w", err)
	}
	return string(b), nil
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
