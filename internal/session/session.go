package session

import (
	"context"
	"errors"
	"fmt"
	"maps"
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

	mu sync.Mutex

	catalog   Catalog
	providers ProviderResolver

	maxActive int

	// Active skills are stored as internal keys; order is activation order.
	activeOrder []string                 // []keyStr
	activeByKey map[string]spec.SkillKey // keyStr -> key

	closed atomic.Bool
	touch  func()
}

func newSession(cfg SessionConfig) *Session {
	return &Session{
		id:          cfg.ID,
		catalog:     cfg.Catalog,
		providers:   cfg.Providers,
		maxActive:   cfg.MaxActivePerSession,
		activeByKey: map[string]spec.SkillKey{},
		touch:       cfg.Touch,
	}
}

func (s *Session) ID() string { return s.id }

func keyStr(k spec.SkillKey) string {
	return k.Type + "\n" + k.Name + "\n" + k.Path
}

func (s *Session) ActiveSkillsPromptXML(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	s.touchSession()
	if s.isClosed() {
		return "", spec.ErrSessionNotFound
	}

	s.mu.Lock()
	order := append([]string(nil), s.activeOrder...)
	byKey := make(map[string]spec.SkillKey, len(s.activeByKey))
	maps.Copy(byKey, s.activeByKey)
	s.mu.Unlock()

	items := make([]catalog.ActiveSkillItem, 0, len(order))
	for _, ks := range order {
		k := byKey[ks]

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
		return nil, errors.New("mode must be 'replace' or 'add'")
	}
	if len(keys) == 0 {
		return nil, errors.New("keys is required")
	}

	// Validate keys exist in catalog (and dedupe).
	// Robustness: if key.Path is not canonical, attempt to canonicalize via provider.Index and
	// then match against catalog using the normalized key.
	req := make([]spec.SkillKey, 0, len(keys))
	seen := map[string]struct{}{}
	for _, k := range keys {
		ks := keyStr(k)
		if _, ok := seen[ks]; ok {
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
				return nil, fmt.Errorf("%w: unknown skill key: %+v", spec.ErrSkillNotFound, k)
			}
			k = rec.Key
			ks = keyStr(k)
			if _, ok2 := s.catalog.Get(k); !ok2 {
				return nil, fmt.Errorf("%w: unknown skill key: %+v", spec.ErrSkillNotFound, k)
			}
		}
		seen[ks] = struct{}{}
		req = append(req, k)
	}
	// Progressive disclosure: ensure bodies loadable BEFORE committing state.
	// Also handle concurrent mutations safely via a small retry loop.
	for range 3 {
		s.mu.Lock()
		if s.isClosed() {
			s.mu.Unlock()
			return nil, spec.ErrSessionNotFound
		}

		// Snapshot current state for "add" mode.
		currentOrder := append([]string(nil), s.activeOrder...)
		currentByKey := make(map[string]spec.SkillKey, len(s.activeByKey))
		maps.Copy(currentByKey, s.activeByKey)
		s.mu.Unlock()

		// Compute next state without holding lock.
		nextByKey := map[string]spec.SkillKey{}
		nextOrder := make([]string, 0, len(currentOrder)+len(req))

		switch m {
		case spec.LoadModeReplace:
			for _, k := range req {
				ks := keyStr(k)
				nextByKey[ks] = k
				nextOrder = append(nextOrder, ks)
			}
		case spec.LoadModeAdd:
			reqSet := map[string]struct{}{}
			for _, k := range req {
				reqSet[keyStr(k)] = struct{}{}
			}
			for _, ks := range currentOrder {
				if _, isReq := reqSet[ks]; isReq {
					continue
				}
				if k, ok := currentByKey[ks]; ok {
					nextByKey[ks] = k
					nextOrder = append(nextOrder, ks)
				}
			}
			for _, k := range req {
				ks := keyStr(k)
				nextByKey[ks] = k
				nextOrder = append(nextOrder, ks)
			}
		}

		if s.maxActive > 0 && len(nextOrder) > s.maxActive {
			return nil, fmt.Errorf("too many active skills (%d > %d)", len(nextOrder), s.maxActive)
		}

		// Ensure bodies are loadable (IO) without lock.
		for _, ks := range nextOrder {
			k := nextByKey[ks]
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
		s.activeByKey = nextByKey
		s.activeOrder = nextOrder
		handles, err := s.activeHandlesLocked()
		s.mu.Unlock()
		return handles, err
	}

	return nil, errors.New("concurrent session modification; please retry")
}

func (s *Session) activeHandlesLocked() ([]spec.SkillHandle, error) {
	out := make([]spec.SkillHandle, 0, len(s.activeOrder))
	for _, ks := range s.activeOrder {
		k, ok := s.activeByKey[ks]
		if !ok {
			continue
		}
		h, ok := s.catalog.HandleForKey(k)
		if !ok {
			return nil, spec.ErrSkillNotFound
		}
		out = append(out, h)
	}
	return out, nil
}

func (s *Session) isActiveLocked(ks string) bool {
	_, ok := s.activeByKey[ks]
	return ok
}

func (s *Session) touchSession() {
	if s.touch != nil {
		s.touch()
	}
}

func (s *Session) isClosed() bool { return s.closed.Load() }

func (s *Session) pruneKey(ks string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.activeByKey[ks]; !ok {
		return
	}
	delete(s.activeByKey, ks)

	// Remove from order slice.
	s.activeOrder = slices.DeleteFunc(s.activeOrder, func(v string) bool { return v == ks })
}
