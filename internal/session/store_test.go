package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestStore_TTLEviction(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	st := NewStore(StoreConfig{
		TTL:                 20 * time.Millisecond,
		MaxSessions:         100,
		MaxActivePerSession: 8,
		Catalog:             cat,
		Providers:           mapResolver{},
	})

	id, _, err := st.NewSession(t.Context(), NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, ok := st.Get(id); !ok {
		t.Fatalf("expected session to exist immediately")
	}

	time.Sleep(35 * time.Millisecond)

	// Eventually should be evicted.
	if _, ok := st.Get(id); ok {
		t.Fatalf("expected session to be expired/evicted")
	}
}

func TestStore_TTLTouchExtendsLifetime(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	ttl := 80 * time.Millisecond

	st := NewStore(StoreConfig{
		TTL:                 ttl,
		MaxSessions:         100,
		MaxActivePerSession: 8,
		Catalog:             cat,
		Providers:           mapResolver{},
	})

	id, _, err := st.NewSession(t.Context(), NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if _, ok := st.Get(id); !ok {
		t.Fatalf("expected session to exist before touch")
	}

	// Touch happened via Get(). Sleep less than ttl since touch.
	time.Sleep(50 * time.Millisecond)
	if _, ok := st.Get(id); !ok {
		t.Fatalf("expected session to still exist due to touch extending TTL")
	}
}

func TestStore_MaxSessionsAndLRU(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	st := NewStore(StoreConfig{
		TTL:                 10 * time.Second,
		MaxSessions:         2,
		MaxActivePerSession: 8,
		Catalog:             cat,
		Providers:           mapResolver{},
	})

	s1, _, err := st.NewSession(t.Context(), NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession s1: %v", err)
	}
	s2, _, err := st.NewSession(t.Context(), NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession s2: %v", err)
	}

	// Touch s1 to make it MRU; s2 becomes LRU.
	if _, ok := st.Get(s1); !ok {
		t.Fatalf("expected s1 to exist")
	}

	s3, _, err := st.NewSession(t.Context(), NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession s3: %v", err)
	}

	if _, ok := st.Get(s2); ok {
		t.Fatalf("expected s2 evicted as LRU")
	}
	if _, ok := st.Get(s1); !ok {
		t.Fatalf("expected s1 retained as MRU")
	}
	if _, ok := st.Get(s3); !ok {
		t.Fatalf("expected s3 exists")
	}
}

func TestStore_PruneSkill(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	k := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	cat.add(k, "ok")

	st := NewStore(StoreConfig{
		TTL:                 10 * time.Second,
		MaxSessions:         100,
		MaxActivePerSession: 8,
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
	})

	id, _, err := st.NewSession(t.Context(), NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s, ok := st.Get(id)
	if !ok {
		t.Fatalf("Get: missing session")
	}

	_, err = s.ActivateKeys(t.Context(), []spec.ProviderSkillKey{k}, spec.LoadModeReplace)
	if err != nil {
		t.Fatalf("ActivateKeys: %v", err)
	}

	st.PruneSkill(k)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeOrder) != 0 {
		t.Fatalf("expected activeOrder pruned to empty, got %+v", s.activeOrder)
	}
}

func TestStore_NewSession_InitialActiveKeys_Activated(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	cat := newMemCatalog()
	k := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	cat.add(k, "ok")

	st := NewStore(StoreConfig{
		TTL:                 10 * time.Second,
		MaxSessions:         100,
		MaxActivePerSession: 8,
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
	})

	id, active, err := st.NewSession(ctx, NewSessionParams{ActiveKeys: []spec.ProviderSkillKey{k}})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(active) != 1 || active[0].Name != "a" {
		t.Fatalf("expected one active handle a, got %+v", active)
	}

	s, ok := st.Get(id)
	if !ok {
		t.Fatalf("expected session exists")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeOrder) != 1 || s.activeOrder[0] != k {
		t.Fatalf("expected activeOrder to contain key, got %+v", s.activeOrder)
	}
}

func TestStore_NewSession_InitialActiveKeys_ErrorDeletesSession(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	// Empty catalog; activation should fail with ErrSkillNotFound.
	cat := newMemCatalog()

	st := NewStore(StoreConfig{
		TTL:                 10 * time.Second,
		MaxSessions:         100,
		MaxActivePerSession: 8,
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
	})

	_, _, err := st.NewSession(ctx, NewSessionParams{
		ActiveKeys: []spec.ProviderSkillKey{
			{Type: "t", Name: "missing", Location: "p1"},
		},
	})
	if !errors.Is(err, spec.ErrSkillNotFound) {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}

	// Store should not retain a session after activation failure.
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.m) != 0 || st.lru.Len() != 0 {
		t.Fatalf("expected store empty after failed NewSession, got m=%d lru=%d", len(st.m), st.lru.Len())
	}
}

func TestStore_NewSession_MaxActiveOverride_AppliesPerSession(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	cat := newMemCatalog()
	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "p2"}
	cat.add(k1, "ok")
	cat.add(k2, "ok")

	st := NewStore(StoreConfig{
		TTL:                 10 * time.Second,
		MaxSessions:         100,
		MaxActivePerSession: 1, // default would reject 2 active keys
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
	})

	id, active, err := st.NewSession(ctx, NewSessionParams{
		MaxActivePerSession: 2,
		ActiveKeys:          []spec.ProviderSkillKey{k1, k2},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active handles, got %+v", active)
	}
	if _, ok := st.Get(id); !ok {
		t.Fatalf("expected session exists")
	}
}

func TestStore_NewSession_ContextValidation(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	st := NewStore(StoreConfig{
		TTL:                 10 * time.Second,
		MaxSessions:         100,
		MaxActivePerSession: 8,
		Catalog:             cat,
		Providers:           mapResolver{},
	})

	cases := []struct {
		name  string
		ctx   context.Context
		isErr func(error) bool
	}{
		{
			name: "nil_context",
			ctx:  nil,
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "canceled_context",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			}(),
			isErr: func(err error) bool {
				return errors.Is(err, context.Canceled)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := st.NewSession(tc.ctx, NewSessionParams{})
			if err == nil || !tc.isErr(err) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestStore_DeleteClosesSession(t *testing.T) {
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

	st.Delete(id)

	if _, ok := st.Get(id); ok {
		t.Fatalf("expected session to be deleted")
	}
}
