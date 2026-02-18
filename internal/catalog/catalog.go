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
	// Host/lifecycle definition (exact user input).
	def spec.SkillDef

	// Internal/provider-canonicalized record.
	idx spec.ProviderSkillIndexRecord

	bodyLoaded bool
	llmName    string

	// "bodyWait" is non-nil while a LoadBody call is in-flight for this entry. It is closed when loading finishes
	// (success or failure).
	bodyWait chan struct{}
	bodyErr  error
}

type handleKey struct {
	Name     string
	Location string
}

type Catalog struct {
	mu sync.RWMutex

	providers ProviderResolver

	// ByKey is keyed by INTERNAL canonical key.
	byKey map[spec.ProviderSkillKey]*entry

	// ByDef maps EXACT user-provided defs to the canonical internal key.
	byDef map[spec.SkillDef]spec.ProviderSkillKey

	// HandleIndex maps LLM-facing handles (computed name + user location) to canonical internal keys.
	handleIndex map[handleKey]spec.ProviderSkillKey
}

func New(providers ProviderResolver) *Catalog {
	return &Catalog{
		providers:   providers,
		byKey:       map[spec.ProviderSkillKey]*entry{},
		byDef:       map[spec.SkillDef]spec.ProviderSkillKey{},
		handleIndex: map[handleKey]spec.ProviderSkillKey{},
	}
}

// Add registers a skill by its host/lifecycle definition (user input).
// It stores provider-canonicalized identity internally, but returns only the original def to callers.
func (c *Catalog) Add(ctx context.Context, def spec.SkillDef) (spec.SkillRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}

	if strings.TrimSpace(def.Type) == "" || strings.TrimSpace(def.Name) == "" || strings.TrimSpace(def.Location) == "" {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: def.type, def.name, and def.location are required",
			spec.ErrInvalidArgument,
		)
	}

	p, ok := c.providers.Provider(def.Type)
	if !ok || p == nil {
		return spec.SkillRecord{}, errors.Join(
			spec.ErrProviderNotFound,
			fmt.Errorf("unknown provider type: %q", def.Type),
		)
	}

	idx, err := p.Index(ctx, def)
	if err != nil {
		return spec.SkillRecord{}, err
	}

	// Provider is allowed to canonicalize Location, but must not change Type/Name identity.
	if idx.Key.Type != def.Type {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: provider changed type from %q to %q",
			spec.ErrInvalidArgument,
			def.Type,
			idx.Key.Type,
		)
	}
	if idx.Key.Name != def.Name {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: provider changed name from %q to %q",
			spec.ErrInvalidArgument,
			def.Name,
			idx.Key.Name,
		)
	}

	if strings.TrimSpace(idx.Key.Type) == "" ||
		strings.TrimSpace(idx.Key.Name) == "" ||
		strings.TrimSpace(idx.Key.Location) == "" {
		return spec.SkillRecord{}, fmt.Errorf("%w: provider returned invalid record key", spec.ErrInvalidArgument)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.byKey[idx.Key]; exists {
		return spec.SkillRecord{}, spec.ErrSkillAlreadyExists
	}
	if _, exists := c.byDef[def]; exists {
		// Same exact user def already registered.
		return spec.SkillRecord{}, spec.ErrSkillAlreadyExists
	}

	e := &entry{def: def, idx: idx}

	// If a provider pre-populates SkillBody we treat it as already loaded
	// only when non-empty. (With the current data model, empty-but-loaded
	// cannot be distinguished from not-yet-loaded.)
	e.bodyLoaded = idx.SkillBody != ""

	c.byKey[idx.Key] = e
	c.byDef[def] = idx.Key

	c.recomputeLLMNamesLocked()

	return spec.SkillRecord{
		Def:         def,
		Description: idx.Description,
		Properties:  idx.Properties,
		Digest:      idx.Digest,
	}, nil
}

// ResolveDef resolves an EXACT user-provided skill def (as originally added) to the internal canonical key.
func (c *Catalog) ResolveDef(def spec.SkillDef) (spec.ProviderSkillKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, ok := c.byDef[def]
	return k, ok
}

// Remove removes a skill by its EXACT host/lifecycle definition.
// Returns the host-facing record, the canonical internal key, and ok=false if not found.
func (c *Catalog) Remove(def spec.SkillDef) (spec.SkillRecord, spec.ProviderSkillKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	canon, ok := c.byDef[def]
	if !ok {
		return spec.SkillRecord{}, spec.ProviderSkillKey{}, false
	}

	e, ok := c.byKey[canon]
	if !ok || e == nil {
		// Repair drift.
		delete(c.byDef, def)
		return spec.SkillRecord{}, spec.ProviderSkillKey{}, false
	}

	// Wake any waiters to avoid deadlocks if Remove races EnsureBody.
	if ch := e.bodyWait; ch != nil {
		e.bodyWait = nil
		close(ch)
	}

	rec := spec.SkillRecord{
		Def:         e.def,
		Description: e.idx.Description,
		Properties:  e.idx.Properties,
		Digest:      e.idx.Digest,
	}

	delete(c.byKey, canon)
	delete(c.byDef, e.def)

	c.recomputeLLMNamesLocked()
	return rec, canon, true
}

func (c *Catalog) GetIndex(key spec.ProviderSkillKey) (spec.ProviderSkillIndexRecord, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.byKey[key]
	if !ok {
		return spec.ProviderSkillIndexRecord{}, false
	}
	return e.idx, true
}

func (c *Catalog) ResolveHandle(h spec.SkillHandle) (spec.ProviderSkillKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	k, ok := c.handleIndex[normHandle(h)]
	return k, ok
}

func (c *Catalog) HandleForKey(key spec.ProviderSkillKey) (spec.SkillHandle, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.byKey[key]
	if !ok {
		return spec.SkillHandle{}, false
	}

	// IMPORTANT: Location returned to LLM is the user-provided location (no canonicalization leakage).
	return spec.SkillHandle{Name: e.llmName, Location: e.def.Location}, true
}

func (c *Catalog) EnsureBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	for range 5 {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		c.mu.Lock()
		e, ok := c.byKey[key]
		if !ok {
			c.mu.Unlock()
			return "", spec.ErrSkillNotFound
		}
		if e.bodyLoaded {
			body := e.idx.SkillBody
			c.mu.Unlock()
			return body, nil
		}
		if e.bodyErr != nil {
			err := e.bodyErr
			c.mu.Unlock()
			return "", err
		}
		if ch := e.bodyWait; ch != nil {
			// Someone else is loading.
			c.mu.Unlock()
			select {
			case <-ch:
				// Continue.
			case <-ctx.Done():
				return "", ctx.Err()
			}
			continue
		}

		// Become the loader.
		ch := make(chan struct{})
		e.bodyWait = ch
		recKey := e.idx.Key
		c.mu.Unlock()

		p, ok := c.providers.Provider(recKey.Type)
		if !ok || p == nil {
			c.finishBodyLoad(key, ch, "", spec.ErrProviderNotFound)
			return "", spec.ErrProviderNotFound
		}

		body, err := p.LoadBody(ctx, recKey)
		c.finishBodyLoad(key, ch, body, err)
		if err != nil {
			return "", err
		}
		return body, nil
	}

	return "", errors.New("could not ensure skill body")
}

// ListPromptIndexRecords lists INTERNAL records for prompt assembly (returns canonical keys).
func (c *Catalog) ListPromptIndexRecords(f PromptFilter) []spec.ProviderSkillIndexRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]spec.ProviderSkillIndexRecord, 0, len(c.byKey))
	for _, e := range c.byKey {
		if !f.match(e) {
			continue
		}
		out = append(out, e.idx)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.Name == out[j].Key.Name {
			return out[i].Key.Location < out[j].Key.Location
		}
		return out[i].Key.Name < out[j].Key.Name
	})
	return out
}

type UserEntry struct {
	Key    spec.ProviderSkillKey // canonical/internal key
	Record spec.SkillRecord      // host-facing record (user def)
}

// ListUserEntries lists host-facing records, while preserving the canonical key for internal consumers.
func (c *Catalog) ListUserEntries(f UserFilter) []UserEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]UserEntry, 0, len(c.byKey))
	for k, e := range c.byKey {
		if !f.match(e) {
			continue
		}
		out = append(out, UserEntry{
			Key: k,
			Record: spec.SkillRecord{
				Def:         e.def,
				Description: e.idx.Description,
				Properties:  e.idx.Properties,
				Digest:      e.idx.Digest,
			},
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Record.Def.Name == out[j].Record.Def.Name {
			return out[i].Record.Def.Location < out[j].Record.Def.Location
		}
		return out[i].Record.Def.Name < out[j].Record.Def.Name
	})
	return out
}

// Conflict handling / name computation.
// Rule: LLM sees (name + location). Normally "name" is idx.Key.Name.
// Only when there is a collision on (Name, Location), we disambiguate WITHOUT leaking provider type:
//
//	"<name>#<shortHash>"
//
// IMPORTANT CONTRACT:
//   - The shortHash MUST be derived from host/lifecycle-visible inputs (spec.SkillDef),
//     not from provider-canonical/internal keys, so canonicalization never becomes LLM-visible.
func (c *Catalog) recomputeLLMNamesLocked() {
	// Base groups by (Name, Location) (no type).
	type groupKey struct {
		Name     string
		Location string
	}
	groups := map[groupKey][]*entry{}
	for _, e := range c.byKey {
		gk := groupKey{Name: e.idx.Key.Name, Location: e.def.Location}
		groups[gk] = append(groups[gk], e)
	}

	// First pass: default or opaque-hash suffix within collision groups.
	for _, grp := range groups {
		if len(grp) == 1 {
			grp[0].llmName = grp[0].idx.Key.Name
			continue
		}
		for _, e := range grp {
			e.llmName = fmt.Sprintf("%s#%s", e.idx.Key.Name, shortHashDef(e.def))
		}
	}

	// Second pass: ensure uniqueness on (llmName, location).
	count := map[string]int{}
	for _, e := range c.byKey {
		count[e.llmName+"\x00"+e.def.Location]++
	}
	for _, e := range c.byKey {
		k := e.llmName + "\x00" + e.def.Location
		if count[k] > 1 {
			e.llmName = fmt.Sprintf("%s#%s", e.idx.Key.Name, shortHashDef(e.def))
		}
	}

	// Rebuild handle index.
	c.handleIndex = map[handleKey]spec.ProviderSkillKey{}
	for _, e := range c.byKey {
		c.handleIndex[handleKey{Name: e.llmName, Location: e.def.Location}] = e.idx.Key
	}
}

func (c *Catalog) finishBodyLoad(key spec.ProviderSkillKey, ch chan struct{}, body string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byKey[key]
	if !ok {
		// Entry removed: Remove() is responsible for closing/waking waiters.
		return
	}
	// Only publish if this completion corresponds to the currently in-flight load.
	if e.bodyWait != ch {
		return
	}

	e.bodyWait = nil
	close(ch)

	if err != nil {
		// Don't permanently cache context cancellation/deadline errors.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		e.bodyErr = err
		return
	}

	e.idx.SkillBody = body
	e.bodyLoaded = true
	e.bodyErr = nil
}

func normHandle(h spec.SkillHandle) handleKey {
	return handleKey{
		Name:     strings.TrimSpace(h.Name),
		Location: strings.TrimSpace(h.Location),
	}
}

func shortHashDef(d spec.SkillDef) string {
	sum := sha256.Sum256([]byte(d.Type + "\x00" + d.Name + "\x00" + d.Location))
	return hex.EncodeToString(sum[:])[:8]
}
