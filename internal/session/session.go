package session

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/flexigpt/agentskills-go/internal/catalog"
	"github.com/flexigpt/agentskills-go/spec"
)

type ProviderResolver interface {
	Provider(skillType string) (spec.SkillProvider, bool)
}

type Catalog interface {
	ResolveHandle(h spec.SkillHandle) (spec.SkillKey, bool)
	HandleForKey(key spec.SkillKey) (spec.SkillHandle, bool)
	EnsureBody(ctx context.Context, key spec.SkillKey) (string, error)
	Get(key spec.SkillKey) (spec.SkillRecord, bool)
}

type SessionConfig struct {
	ID                  string
	Catalog             Catalog
	Providers           ProviderResolver
	MaxActivePerSession int
	Touch               func() // store-provided "touch" to keep TTL/LRU alive
}

type Session struct {
	id string

	catalog   Catalog
	providers ProviderResolver

	maxActive   int
	activeOrder []spec.SkillKey // Active skills are stored as internal keys; order is activation order.
	activeSet   map[spec.SkillKey]struct{}

	mu           sync.Mutex
	stateVersion uint64 // stateVersion increments on every mutation; used for optimistic concurrency.
	closed       atomic.Bool
	touch        func()
}

func newSession(cfg SessionConfig) *Session {
	return &Session{
		id:        cfg.ID,
		catalog:   cfg.Catalog,
		providers: cfg.Providers,
		maxActive: cfg.MaxActivePerSession,
		activeSet: map[spec.SkillKey]struct{}{},
		touch:     cfg.Touch,
	}
}

func (s *Session) ID() string { return s.id }

func (s *Session) ActiveSkillsPromptXML(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	s.touchSession()
	if s.isClosed() {
		return "", spec.ErrSessionNotFound
	}

	s.mu.Lock()
	order := append([]spec.SkillKey(nil), s.activeOrder...)
	s.mu.Unlock()

	items := make([]catalog.ActiveSkillItem, 0, len(order))
	for _, k := range order {

		h, ok := s.catalog.HandleForKey(k)
		if !ok {
			return "", spec.ErrSkillNotFound
		}
		body, err := s.catalog.EnsureBody(ctx, k)
		if err != nil {
			return "", err
		}
		items = append(items, catalog.ActiveSkillItem{
			Name: h.Name,
			Body: body,
		})
	}

	return catalog.ActiveSkillsXML(items)
}

func (s *Session) ActivateKeys(
	ctx context.Context,
	keys []spec.SkillKey,
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
		return nil, fmt.Errorf("%w: mode must be 'replace' or 'add'", spec.ErrInvalidArgument)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: keys is required", spec.ErrInvalidArgument)
	}

	// Validate keys exist in catalog (and dedupe).
	// Robustness: if key.Path is not canonical, attempt to canonicalize via provider.Index and
	// then match against catalog using the normalized key.
	req := make([]spec.SkillKey, 0, len(keys))
	seen := map[spec.SkillKey]struct{}{}

	for _, k := range keys {
		if _, ok := seen[k]; ok {
			continue
		}

		if _, ok := s.catalog.Get(k); !ok {
			// Try provider-based canonicalization.
			p, okp := s.providers.Provider(k.Type)
			if !okp || p == nil {
				return nil, spec.ErrProviderNotFound
			}
			rec, err := p.Index(ctx, k)
			if err != nil {
				return nil, fmt.Errorf("%w: unknown skill key: %+v: %w", spec.ErrSkillNotFound, k, err)
			}
			k = rec.Key
			if _, ok2 := s.catalog.Get(k); !ok2 {
				return nil, fmt.Errorf("%w: unknown skill key: %+v", spec.ErrSkillNotFound, k)
			}
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
		currentOrder := append([]spec.SkillKey(nil), s.activeOrder...)
		currentSet := make(map[spec.SkillKey]struct{}, len(s.activeSet))
		for k := range s.activeSet {
			currentSet[k] = struct{}{}
		}
		s.mu.Unlock()

		// Compute next state without holding lock.
		nextSet := map[spec.SkillKey]struct{}{}
		nextOrder := make([]spec.SkillKey, 0, len(currentOrder)+len(req))

		switch m {
		case spec.LoadModeReplace:
			for _, k := range req {

				nextSet[k] = struct{}{}
				nextOrder = append(nextOrder, k)
			}
		case spec.LoadModeAdd:
			reqSet := map[spec.SkillKey]struct{}{}

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
			return nil, fmt.Errorf("too many active skills (%d > %d)", len(nextOrder), s.maxActive)
		}

		// Ensure bodies are loadable (IO) without lock.
		for _, k := range nextOrder {
			if _, err := s.catalog.EnsureBody(ctx, k); err != nil {
				return nil, err
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

	return nil, fmt.Errorf("%w: concurrent session modification; please retry", spec.ErrInvalidArgument)
}

func (s *Session) activeHandlesLocked() ([]spec.SkillHandle, error) {
	out := make([]spec.SkillHandle, 0, len(s.activeOrder))
	for _, k := range s.activeOrder {
		h, ok := s.catalog.HandleForKey(k)
		if !ok {
			return nil, spec.ErrSkillNotFound
		}
		out = append(out, h)
	}
	return out, nil
}

func (s *Session) isActiveLocked(k spec.SkillKey) bool { _, ok := s.activeSet[k]; return ok }

func (s *Session) touchSession() {
	if s.touch != nil {
		s.touch()
	}
}

func (s *Session) isClosed() bool { return s.closed.Load() }

func (s *Session) pruneKey(k spec.SkillKey) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.activeSet[k]; !ok {
		return
	}
	delete(s.activeSet, k)
	s.stateVersion++

	// Remove from order slice.
	s.activeOrder = slices.DeleteFunc(s.activeOrder, func(v spec.SkillKey) bool { return v == k })
}
