package session

import (
	"errors"
	"testing"
	"time"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestNewStore_Defaults(t *testing.T) {
	t.Parallel()

	st := NewStore(StoreConfig{})
	if st.ttl != 24*time.Hour {
		t.Fatalf("expected default ttl 24h, got %v", st.ttl)
	}
	if st.maxSessions != 4096 {
		t.Fatalf("expected default maxSessions 4096, got %d", st.maxSessions)
	}
	if st.lru == nil || st.m == nil {
		t.Fatalf("expected store internals to be initialized: %+v", st)
	}
}

func TestSession_ActiveKeys_ClosedSession(t *testing.T) {
	t.Parallel()

	s := newSession(SessionConfig{
		ID:                  "closed-session",
		Catalog:             newMemCatalog(),
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})
	s.closed.Store(true)

	if _, err := s.ActiveKeys(t.Context()); !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSession_IDAndActiveHandlesLockedPrunesMissingHandles(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "p2"}
	h1 := spec.SkillHandle{Name: "a", Location: "p1"}
	h2 := spec.SkillHandle{Name: "b", Location: "p2"}
	cat.addWithHandle(k1, h1, "body-a")
	cat.addWithHandle(k2, h2, "body-b")

	s := newSession(SessionConfig{
		ID:                  "session-123",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	if got := s.ID(); got != "session-123" {
		t.Fatalf("ID() = %q, want %q", got, "session-123")
	}

	if _, err := s.ActivateKeys(t.Context(), []spec.ProviderSkillKey{k1, k2}, spec.LoadModeReplace); err != nil {
		t.Fatalf("ActivateKeys: %v", err)
	}

	// Make k2 lose its prompt handle but keep the catalog key around.
	cat.mu.Lock()
	delete(cat.handles, k2)
	cat.mu.Unlock()

	handles, err := s.activeHandlesLocked()
	if err != nil {
		t.Fatalf("activeHandlesLocked: %v", err)
	}
	if len(handles) != 1 || handles[0] != h1 {
		t.Fatalf("unexpected handles: %+v", handles)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeOrder) != 1 || s.activeOrder[0] != k1 {
		t.Fatalf("expected activeOrder pruned to [k1], got %+v", s.activeOrder)
	}
	if len(s.activeSet) != 1 {
		t.Fatalf("expected activeSet pruned to 1 entry, got %+v", s.activeSet)
	}
	if !s.isActiveLocked(k1) {
		t.Fatalf("expected k1 to remain active")
	}
}

func TestSession_PruneKey_RemovesKeyAndNoopsWhenClosed(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "p2"}
	cat.add(k1, "body-a")
	cat.add(k2, "body-b")

	s := newSession(SessionConfig{
		ID:                  "session-456",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	if _, err := s.ActivateKeys(
		t.Context(),
		[]spec.ProviderSkillKey{k1, k2},
		spec.LoadModeReplace,
	); err != nil {
		t.Fatalf("ActivateKeys: %v", err)
	}

	s.pruneKey(k1)
	s.mu.Lock()
	if len(s.activeOrder) != 1 || s.activeOrder[0] != k2 {
		t.Fatalf("expected activeOrder to contain only k2 after pruneKey, got %+v", s.activeOrder)
	}
	if s.isActiveLocked(k1) {
		t.Fatalf("expected k1 to be removed from activeSet")
	}
	s.mu.Unlock()

	// Closed sessions should ignore prune requests.
	s.closed.Store(true)
	s.pruneKey(k2)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeOrder) != 1 || s.activeOrder[0] != k2 {
		t.Fatalf("expected pruneKey to no-op on closed session, got %+v", s.activeOrder)
	}
	if !s.isActiveLocked(k2) {
		t.Fatalf("expected k2 to remain active when closed")
	}
}

func TestStore_Get_RemovesClosedSession(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	st := NewStore(StoreConfig{
		TTL:                 10 * time.Second,
		MaxSessions:         100,
		MaxActivePerSession: 8,
		Catalog:             cat,
		Providers:           mapResolver{},
	})

	id, _, err := st.NewSession(t.Context(), NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	s, ok := st.Get(id)
	if !ok {
		t.Fatalf("expected session to exist")
	}
	s.closed.Store(true)

	if _, ok := st.Get(id); ok {
		t.Fatalf("expected closed session to be removed on Get")
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if _, exists := st.m[id]; exists {
		t.Fatalf("expected store map entry to be removed")
	}
	if st.lru.Len() != 0 {
		t.Fatalf("expected LRU to be empty after removal, got %d", st.lru.Len())
	}
}
