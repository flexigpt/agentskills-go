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

func (c *Catalog) Add(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}

	if strings.TrimSpace(key.Type) == "" || strings.TrimSpace(key.Name) == "" || strings.TrimSpace(key.Location) == "" {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: key.type, key.name, and key.location are required",
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

	// Provider is allowed to normalize Location, but must not change Type/Name identity.
	if rec.Key.Type != key.Type {
		return spec.SkillRecord{}, fmt.Errorf("%w: provider changed key.type from %q to %q",
			spec.ErrInvalidArgument, key.Type, rec.Key.Type)
	}
	if rec.Key.Name != key.Name {
		return spec.SkillRecord{}, fmt.Errorf("%w: provider changed key.name from %q to %q",
			spec.ErrInvalidArgument, key.Name, rec.Key.Name)
	}

	if strings.TrimSpace(rec.Key.Type) == "" || strings.TrimSpace(rec.Key.Name) == "" ||
		strings.TrimSpace(rec.Key.Location) == "" {
		return spec.SkillRecord{}, fmt.Errorf("%w: provider returned invalid record key", spec.ErrInvalidArgument)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.byKey[rec.Key]; exists {
		return spec.SkillRecord{}, spec.ErrSkillAlreadyExists
	}

	e := &entry{rec: rec}
	// If a provider pre-populates SkillMDBody we treat it as already loaded
	// only when non-empty. (With the current data model, empty-but-loaded
	// cannot be distinguished from not-yet-loaded.)
	e.bodyLoaded = rec.SkillBody != ""
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

	// Wake any waiters to avoid deadlocks if Remove races EnsureBody.
	if ch := e.bodyWait; ch != nil {
		e.bodyWait = nil
		close(ch)
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
	return spec.SkillHandle{Name: e.llmName, Location: e.rec.Key.Location}, true
}

func (c *Catalog) EnsureBody(ctx context.Context, key spec.SkillKey) (string, error) {
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
			body := e.rec.SkillBody
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
		recKey := e.rec.Key
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
			return out[i].Key.Location < out[j].Key.Location
		}
		return out[i].Key.Name < out[j].Key.Name
	})
	return out
}

// Conflict handling / name computation.
// Rule: LLM sees (name + location). Normally "name" is key.Name.
// Only when there is a collision on (Name, Location) across different providers,
// we use "<type>:<name>" for all in that collision group.
// If collision still exists, append "#<shortHash>".
func (c *Catalog) recomputeLLMNamesLocked() {
	// Base groups by (Name, Location) (no type).
	type groupKey struct {
		Name     string
		Location string
	}
	groups := map[groupKey][]*entry{}
	for _, e := range c.byKey {
		gk := groupKey{Name: e.rec.Key.Name, Location: e.rec.Key.Location}
		groups[gk] = append(groups[gk], e)
	}

	// First pass: default or type-prefixed within collision groups.
	for _, grp := range groups {
		if len(grp) == 1 {
			grp[0].llmName = grp[0].rec.Key.Name
			continue
		}
		// Collision group on (Name, Location): disambiguate by type for *all* in group.
		for _, e := range grp {
			e.llmName = e.rec.Key.Type + ":" + e.rec.Key.Name
		}
	}

	// Second pass: ensure uniqueness on (llmName, location).
	count := map[string]int{}
	for _, e := range c.byKey {
		count[e.llmName+"\x00"+e.rec.Key.Location]++
	}
	for _, e := range c.byKey {
		k := e.llmName + "\x00" + e.rec.Key.Location

		if count[k] > 1 {
			e.llmName = fmt.Sprintf("%s:%s#%s", e.rec.Key.Type, e.rec.Key.Name, shortHash(e.rec.Key))
		}
	}

	// Rebuild handle index.
	c.handleIndex = map[handleKey]spec.SkillKey{}
	for _, e := range c.byKey {
		c.handleIndex[handleKey{Name: e.llmName, Location: e.rec.Key.Location}] = e.rec.Key
	}
}

func (c *Catalog) finishBodyLoad(key spec.SkillKey, ch chan struct{}, body string, err error) {
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

	e.rec.SkillBody = body
	e.bodyLoaded = true
	e.bodyErr = nil
}

func normHandle(h spec.SkillHandle) handleKey {
	return handleKey{
		Name:     strings.TrimSpace(h.Name),
		Location: strings.TrimSpace(h.Location),
	}
}

func shortHash(k spec.SkillKey) string {
	sum := sha256.Sum256([]byte(k.Type + "\x00" + k.Name + "\x00" + k.Location))
	return hex.EncodeToString(sum[:])[:8]
}
