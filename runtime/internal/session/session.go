package session

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/flexigpt/agentskills-go/document"
	"github.com/flexigpt/agentskills-go/provider"
	"github.com/flexigpt/agentskills-go/runtime/spec"
)

type ProviderResolver interface {
	Provider(skillType string) (provider.SkillProvider, bool)
}

type Catalog interface {
	ResolveHandle(h spec.SkillHandle) (provider.ProviderSkillKey, bool)
	HandleForKey(key provider.ProviderSkillKey) (spec.SkillHandle, bool)
	EnsureBody(ctx context.Context, key provider.ProviderSkillKey) (string, error)
	GetIndex(key provider.ProviderSkillKey) (provider.ProviderSkillIndexRecord, bool)
}

type SessionConfig struct {
	ID                  string
	Catalog             Catalog
	Providers           ProviderResolver
	MaxActivePerSession int
	Touch               func() // store-provided "touch" to keep TTL/LRU alive

	// Nil means unrestricted. A non-nil empty map means no skill is allowed.
	AllowedKeys       map[provider.ProviderSkillKey]struct{}
	SupportsRunScript bool
}

type Session struct {
	id string

	catalog   Catalog
	providers ProviderResolver

	maxActive   int
	activeOrder []provider.ProviderSkillKey // Active skills are stored as internal keys; order is activation order.
	activeSet   map[provider.ProviderSkillKey]struct{}

	allowedKeys       map[provider.ProviderSkillKey]struct{}
	supportsRunScript bool

	mu           sync.Mutex
	stateVersion uint64 // stateVersion increments on every mutation; used for optimistic concurrency.
	closed       atomic.Bool
	touch        func()
}

func newSession(cfg SessionConfig) *Session {
	var allowedKeys map[provider.ProviderSkillKey]struct{}
	if cfg.AllowedKeys != nil {
		allowedKeys = make(map[provider.ProviderSkillKey]struct{}, len(cfg.AllowedKeys))
		for key := range cfg.AllowedKeys {
			allowedKeys[key] = struct{}{}
		}
	}

	return &Session{
		id:                cfg.ID,
		catalog:           cfg.Catalog,
		providers:         cfg.Providers,
		maxActive:         cfg.MaxActivePerSession,
		activeSet:         map[provider.ProviderSkillKey]struct{}{},
		allowedKeys:       allowedKeys,
		supportsRunScript: cfg.SupportsRunScript,
		touch:             cfg.Touch,
	}
}

func (s *Session) ID() string { return s.id }

// AllowsKey reports whether this session may use key. It is exported only to
// sibling runtime packages through the module's internal boundary.
func (s *Session) AllowsKey(key provider.ProviderSkillKey) bool {
	if s == nil {
		return false
	}
	if s.allowedKeys == nil {
		return true
	}
	_, allowed := s.allowedKeys[key]
	return allowed
}

// ActiveKeys returns the session's active skill keys in activation order.
//
// It also prunes keys that no longer exist in the catalog so callers don't need to handle removed-skills drift.
func (s *Session) ActiveKeys(ctx context.Context) ([]provider.ProviderSkillKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.touchSession()
	if s.isClosed() {
		return nil, spec.ErrSessionNotFound
	}

	s.mu.Lock()
	order := append([]provider.ProviderSkillKey(nil), s.activeOrder...)
	s.mu.Unlock()

	out := make([]provider.ProviderSkillKey, 0, len(order))
	missing := make([]provider.ProviderSkillKey, 0)
	for _, k := range order {
		// If removed from catalog, prune from session.
		if _, ok := s.catalog.HandleForKey(k); !ok {
			missing = append(missing, k)
			continue
		}
		out = append(out, k)
	}

	if len(missing) > 0 {
		s.pruneKeys(missing)
	}
	return out, nil
}

func (s *Session) ActivateKeys(
	ctx context.Context,
	keys []provider.ProviderSkillKey,
	mode spec.LoadMode,
) ([]spec.SkillHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.touchSession()
	if s.isClosed() {
		return nil, spec.ErrSessionNotFound
	}

	m := mode
	if strings.TrimSpace(string(m)) == "" {
		m = spec.LoadModeReplace
	}
	if m != spec.LoadModeReplace && m != spec.LoadModeAdd {
		return nil, fmt.Errorf("%w: mode must be 'replace' or 'add'", spec.ErrInvalidRuntimeArgument)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: keys is required", spec.ErrInvalidRuntimeArgument)
	}

	// Validate keys exist in catalog (and dedupe).

	req := make([]provider.ProviderSkillKey, 0, len(keys))
	seen := map[provider.ProviderSkillKey]struct{}{}

	for _, k := range keys {
		if _, ok := seen[k]; ok {
			continue
		}

		idx, ok := s.catalog.GetIndex(k)
		if !ok {
			return nil, fmt.Errorf("%w: unknown skill key: %+v", spec.ErrSkillNotFound, k)
		}
		if !s.AllowsKey(k) {
			return nil, spec.ErrSkillNotAllowed
		}
		insert, _ := document.NormalizeSkillInsert(idx.Insert)
		if insert != document.SkillInsertInstructions {
			return nil, fmt.Errorf(
				"%w: only insert=instructions skills can be activated in a session",
				spec.ErrInvalidRuntimeArgument,
			)
		}
		seen[k] = struct{}{}
		req = append(req, k)
	}
	// Progressive disclosure: ensure bodies loadable BEFORE committing state.
	// Also handle concurrent mutations safely via a small retry loop.
	for range 5 {
		s.mu.Lock()
		if s.isClosed() {
			s.mu.Unlock()
			return nil, spec.ErrSessionNotFound
		}

		snapVer := s.stateVersion
		currentOrder := append([]provider.ProviderSkillKey(nil), s.activeOrder...)
		currentSet := make(map[provider.ProviderSkillKey]struct{}, len(s.activeSet))
		for k := range s.activeSet {
			currentSet[k] = struct{}{}
		}
		s.mu.Unlock()

		// Compute next state without holding lock.
		nextSet := map[provider.ProviderSkillKey]struct{}{}
		nextOrder := make([]provider.ProviderSkillKey, 0, len(currentOrder)+len(req))

		switch m {
		case spec.LoadModeReplace:
			for _, k := range req {

				nextSet[k] = struct{}{}
				nextOrder = append(nextOrder, k)
			}
		case spec.LoadModeAdd:
			reqSet := map[provider.ProviderSkillKey]struct{}{}

			for _, k := range req {
				reqSet[k] = struct{}{}
			}
			for _, k := range currentOrder {
				if _, isReq := reqSet[k]; isReq {
					continue
				}
				if _, ok := currentSet[k]; !ok {
					continue
				}
				nextSet[k] = struct{}{}
				nextOrder = append(nextOrder, k)
			}
			for _, k := range req {
				nextSet[k] = struct{}{}
				nextOrder = append(nextOrder, k)
			}
		}

		if s.maxActive > 0 && len(nextOrder) > s.maxActive {
			return nil, fmt.Errorf(
				"%w: too many active skills (%d > %d)",
				spec.ErrInvalidRuntimeArgument,
				len(nextOrder),
				s.maxActive,
			)
		}

		// Ensure bodies are loadable (IO) without lock.
		for _, k := range nextOrder {
			if _, err := s.catalog.EnsureBody(ctx, k); err != nil {
				return nil, err
			}
		}

		// Re-check existence just before commit (skills could have been removed concurrently).
		for _, k := range nextOrder {
			if _, ok := s.catalog.GetIndex(k); !ok {
				return nil, spec.ErrSkillNotFound
			}
		}

		// Commit.
		s.mu.Lock()
		if s.isClosed() {
			s.mu.Unlock()
			return nil, spec.ErrSessionNotFound
		}
		if s.stateVersion != snapVer {
			// Concurrent modification detected; retry with a fresh snapshot.
			s.mu.Unlock()
			continue
		}
		s.activeSet = nextSet

		s.activeOrder = nextOrder
		s.stateVersion++

		handles, err := s.activeHandlesLocked()
		s.mu.Unlock()
		return handles, err
	}

	return nil, fmt.Errorf("%w: concurrent session modification; please retry", spec.ErrInvalidRuntimeArgument)
}

func (s *Session) activeHandlesLocked() ([]spec.SkillHandle, error) {
	out := make([]spec.SkillHandle, 0, len(s.activeOrder))
	var missing map[provider.ProviderSkillKey]struct{}

	for _, k := range s.activeOrder {
		h, ok := s.catalog.HandleForKey(k)
		if !ok {
			if missing == nil {
				missing = map[provider.ProviderSkillKey]struct{}{}
			}
			missing[k] = struct{}{}
			continue
		}
		out = append(out, h)
	}
	if len(missing) > 0 {
		for k := range missing {
			delete(s.activeSet, k)
		}
		s.activeOrder = slices.DeleteFunc(s.activeOrder, func(v provider.ProviderSkillKey) bool {
			_, ok := missing[v]
			return ok
		})
		s.stateVersion++
	}
	return out, nil
}

func (s *Session) isActiveLocked(k provider.ProviderSkillKey) bool {
	_, ok := s.activeSet[k]
	return ok
}

func (s *Session) touchSession() {
	if s.touch != nil {
		s.touch()
	}
}

func (s *Session) pruneKeys(keys []provider.ProviderSkillKey) {
	if len(keys) == 0 {
		return
	}
	rm := make(map[provider.ProviderSkillKey]struct{}, len(keys))
	for _, k := range keys {
		rm[k] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed() {
		return
	}

	changed := false
	for k := range rm {
		if _, ok := s.activeSet[k]; ok {
			delete(s.activeSet, k)
			changed = true
		}
	}
	if !changed {
		return
	}

	s.activeOrder = slices.DeleteFunc(s.activeOrder, func(v provider.ProviderSkillKey) bool {
		_, ok := rm[v]
		return ok
	})
	s.stateVersion++
}

func (s *Session) pruneKey(k provider.ProviderSkillKey) {
	if s.closed.Load() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.activeSet[k]; !ok {
		return
	}
	delete(s.activeSet, k)
	s.stateVersion++

	// Remove from order slice.
	s.activeOrder = slices.DeleteFunc(s.activeOrder, func(v provider.ProviderSkillKey) bool { return v == k })
}

func (s *Session) isClosed() bool { return s.closed.Load() }
