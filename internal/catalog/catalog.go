package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/flexigpt/agentskills-go/spec"
)

type ProviderResolver interface {
	Provider(skillType string) (spec.SkillProvider, bool)
}

type entry struct {
	rec        spec.SkillRecord
	bodyLoaded bool
	llmName    string
}

type handleKey struct {
	Name string
	Path string
}

type Catalog struct {
	mu sync.RWMutex

	providers ProviderResolver

	byKey       map[spec.SkillKey]*entry
	handleIndex map[handleKey]spec.SkillKey
}

func New(providers ProviderResolver) *Catalog {
	return &Catalog{
		providers:   providers,
		byKey:       map[spec.SkillKey]*entry{},
		handleIndex: map[handleKey]spec.SkillKey{},
	}
}

func normHandle(h spec.SkillHandle) handleKey {
	return handleKey{
		Name: strings.TrimSpace(h.Name),
		Path: strings.TrimSpace(h.Path),
	}
}

func (c *Catalog) Add(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	if strings.TrimSpace(key.Type) == "" || strings.TrimSpace(key.Name) == "" || strings.TrimSpace(key.Path) == "" {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: key.type, key.name, and key.path are required",
			spec.ErrInvalidArgument,
		)
	}

	p, ok := c.providers.Provider(key.Type)
	if !ok || p == nil {
		return spec.SkillRecord{}, errors.Join(
			spec.ErrProviderNotFound,
			fmt.Errorf("unknown provider type: %q", key.Type),
		)
	}

	rec, err := p.Index(ctx, key)
	if err != nil {
		return spec.SkillRecord{}, err
	}
	if strings.TrimSpace(rec.Key.Type) == "" || strings.TrimSpace(rec.Key.Name) == "" ||
		strings.TrimSpace(rec.Key.Path) == "" {
		return spec.SkillRecord{}, fmt.Errorf("%w: provider returned invalid record key", spec.ErrInvalidArgument)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.byKey[rec.Key]; exists {
		return spec.SkillRecord{}, spec.ErrSkillAlreadyExists
	}

	e := &entry{rec: rec}
	e.bodyLoaded = strings.TrimSpace(rec.SkillMDBody) != ""
	c.byKey[rec.Key] = e

	c.recomputeLLMNamesLocked()

	return e.rec, nil
}

func (c *Catalog) Remove(key spec.SkillKey) (spec.SkillRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.byKey[key]

	if !ok {
		return spec.SkillRecord{}, false
	}

	rec := e.rec
	delete(c.byKey, key)

	c.recomputeLLMNamesLocked()
	return rec, true
}

func (c *Catalog) Get(key spec.SkillKey) (spec.SkillRecord, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.byKey[key]

	if !ok {
		return spec.SkillRecord{}, false
	}
	return e.rec, true
}

func (c *Catalog) ResolveHandle(h spec.SkillHandle) (spec.SkillKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	k, ok := c.handleIndex[normHandle(h)]

	return k, ok
}

func (c *Catalog) HandleForKey(key spec.SkillKey) (spec.SkillHandle, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.byKey[key]

	if !ok {
		return spec.SkillHandle{}, false
	}
	return spec.SkillHandle{Name: e.llmName, Path: e.rec.Key.Path}, true
}

func (c *Catalog) EnsureBody(ctx context.Context, key spec.SkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	c.mu.RLock()
	e, ok := c.byKey[key]

	if !ok {
		c.mu.RUnlock()
		return "", spec.ErrSkillNotFound
	}
	if e.bodyLoaded && strings.TrimSpace(e.rec.SkillMDBody) != "" {
		body := e.rec.SkillMDBody
		c.mu.RUnlock()
		return body, nil
	}
	recKey := e.rec.Key
	c.mu.RUnlock()

	p, ok := c.providers.Provider(recKey.Type)
	if !ok || p == nil {
		return "", spec.ErrProviderNotFound
	}

	body, err := p.LoadBody(ctx, recKey)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Re-check entry still exists and only fill if empty.
	e2, ok := c.byKey[key]

	if !ok {
		return "", spec.ErrSkillNotFound
	}
	if strings.TrimSpace(e2.rec.SkillMDBody) == "" {
		e2.rec.SkillMDBody = body
	}
	e2.bodyLoaded = true
	return e2.rec.SkillMDBody, nil
}

func (c *Catalog) ListRecords(f Filter) []spec.SkillRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]spec.SkillRecord, 0, len(c.byKey))
	for _, e := range c.byKey {
		if !f.match(e) {
			continue
		}
		out = append(out, e.rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.Name == out[j].Key.Name {
			return out[i].Key.Path < out[j].Key.Path
		}
		return out[i].Key.Name < out[j].Key.Name
	})
	return out
}

// Conflict handling / name computation.
// Rule: LLM sees (name + path). Normally "name" is key.Name.
// Only when there is a collision on (Name, Path) across different providers,
// we use "<type>:<name>" for all in that collision group.
// If collision still exists, append "#<shortHash>".
func (c *Catalog) recomputeLLMNamesLocked() {
	// Base groups by (Name, Path) (no type).
	type groupKey struct {
		Name string
		Path string
	}
	groups := map[groupKey][]*entry{}
	for _, e := range c.byKey {
		gk := groupKey{Name: e.rec.Key.Name, Path: e.rec.Key.Path}
		groups[gk] = append(groups[gk], e)
	}

	// First pass: default or type-prefixed within collision groups.
	for _, grp := range groups {
		if len(grp) == 1 {
			grp[0].llmName = grp[0].rec.Key.Name
			continue
		}
		// Collision group on (Name, Path): disambiguate by type for *all* in group.
		for _, e := range grp {
			e.llmName = e.rec.Key.Type + ":" + e.rec.Key.Name
		}
	}

	// Second pass: ensure uniqueness on (llmName, path).
	count := map[string]int{}
	for _, e := range c.byKey {
		count[e.llmName+"\x00"+e.rec.Key.Path]++
	}
	for _, e := range c.byKey {
		k := e.llmName + "\x00" + e.rec.Key.Path

		if count[k] > 1 {
			e.llmName = fmt.Sprintf("%s:%s#%s", e.rec.Key.Type, e.rec.Key.Name, shortHash(e.rec.Key))
		}
	}

	// Rebuild handle index.
	c.handleIndex = map[handleKey]spec.SkillKey{}
	for _, e := range c.byKey {
		c.handleIndex[handleKey{Name: e.llmName, Path: e.rec.Key.Path}] = e.rec.Key
	}
}

func shortHash(k spec.SkillKey) string {
	sum := sha256.Sum256([]byte(k.Type + "\x00" + k.Name + "\x00" + k.Path))
	return hex.EncodeToString(sum[:])[:8]
}
