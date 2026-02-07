package session

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

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

	closed bool
}

func newSession(cfg SessionConfig) *Session {
	return &Session{
		id:          cfg.ID,
		catalog:     cfg.Catalog,
		providers:   cfg.Providers,
		maxActive:   cfg.MaxActivePerSession,
		activeByKey: map[string]spec.SkillKey{},
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
	req := make([]spec.SkillKey, 0, len(keys))
	seen := map[string]struct{}{}
	for _, k := range keys {
		ks := keyStr(k)
		if _, ok := seen[ks]; ok {
			continue
		}
		if _, ok := s.catalog.Get(k); !ok {
			return nil, fmt.Errorf("%w: unknown skill key: %+v", spec.ErrSkillNotFound, k)
		}
		seen[ks] = struct{}{}
		req = append(req, k)
	}

	// Build next active set.
	s.mu.Lock()
	defer s.mu.Unlock()

	var nextOrder []string
	var nextByKey map[string]spec.SkillKey

	switch m {
	case spec.LoadModeReplace:
		nextByKey = map[string]spec.SkillKey{}
		nextOrder = make([]string, 0, len(req))
		for _, k := range req {
			ks := keyStr(k)
			nextByKey[ks] = k
			nextOrder = append(nextOrder, ks)
		}

	case spec.LoadModeAdd:
		reqSet := map[string]spec.SkillKey{}
		for _, k := range req {
			reqSet[keyStr(k)] = k
		}

		nextByKey = map[string]spec.SkillKey{}
		nextOrder = make([]string, 0, len(s.activeOrder)+len(req))

		// Keep existing order except those being re-added (dedupe).
		for _, ks := range s.activeOrder {
			if _, isReq := reqSet[ks]; isReq {
				continue
			}
			if k, ok := s.activeByKey[ks]; ok {
				nextByKey[ks] = k
				nextOrder = append(nextOrder, ks)
			}
		}
		// Append requested in request order.
		for _, k := range req {
			ks := keyStr(k)
			nextByKey[ks] = k
			nextOrder = append(nextOrder, ks)
		}
	}

	if s.maxActive > 0 && len(nextOrder) > s.maxActive {
		return nil, fmt.Errorf("too many active skills (%d > %d)", len(nextOrder), s.maxActive)
	}

	// Progressive disclosure: ensure bodies loadable now (fail early).
	// Do it outside session lock to avoid blocking other calls.
	s.activeByKey = nextByKey
	s.activeOrder = nextOrder

	s.mu.Unlock()
	for _, ks := range nextOrder {
		k := nextByKey[ks]
		if _, err := s.catalog.EnsureBody(ctx, k); err != nil {
			s.mu.Lock()
			return nil, err
		}
	}
	s.mu.Lock()

	return s.activeHandlesLocked()
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
