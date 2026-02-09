package session

import (
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

	id, err := st.NewSession()
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

	s1, err := st.NewSession()
	if err != nil {
		t.Fatalf("NewSession s1: %v", err)
	}
	s2, err := st.NewSession()
	if err != nil {
		t.Fatalf("NewSession s2: %v", err)
	}

	// Touch s1 to make it MRU; s2 becomes LRU.
	if _, ok := st.Get(s1); !ok {
		t.Fatalf("expected s1 to exist")
	}

	s3, err := st.NewSession()
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
	k := spec.SkillKey{Type: "t", Name: "a", Path: "p1"}
	cat.add(k, "ok")

	st := NewStore(StoreConfig{
		TTL:                 10 * time.Second,
		MaxSessions:         100,
		MaxActivePerSession: 8,
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
	})

	id, err := st.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s, ok := st.Get(id)
	if !ok {
		t.Fatalf("Get: missing session")
	}

	_, err = s.ActivateKeys(t.Context(), []spec.SkillKey{k}, spec.LoadModeReplace)
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
