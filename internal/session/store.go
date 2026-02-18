package session

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/flexigpt/agentskills-go/spec"
)

type StoreConfig struct {
	TTL                 time.Duration
	MaxSessions         int
	MaxActivePerSession int

	Catalog   Catalog
	Providers ProviderResolver
}

type Store struct {
	mu sync.Mutex

	ttl         time.Duration
	maxSessions int

	lru *list.List               // front=MRU
	m   map[string]*list.Element // id -> element(Value=*item)

	cfg StoreConfig
}

type item struct {
	s        *Session
	lastUsed time.Time
}

type NewSessionParams struct {
	// If >0 overrides store default for this session.
	MaxActivePerSession int

	// Optional initial active skill keys (activated with LoadModeReplace).
	ActiveKeys []spec.ProviderSkillKey
}

func NewStore(cfg StoreConfig) *Store {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	maxS := cfg.MaxSessions
	if maxS <= 0 {
		maxS = 4096
	}
	return &Store{
		ttl:         ttl,
		maxSessions: maxS,
		lru:         list.New(),
		m:           map[string]*list.Element{},
		cfg:         cfg,
	}
}

func (st *Store) NewSession(ctx context.Context, p NewSessionParams) (string, []spec.SkillHandle, error) {
	if ctx == nil {
		return "", nil, fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	now := time.Now()

	st.mu.Lock()

	st.evictExpiredLocked(now)
	st.evictOverLimitLocked()

	u, err := uuid.NewV7()
	if err != nil {
		st.mu.Unlock()
		return "", nil, fmt.Errorf("new session id: %w", err)
	}
	id := u.String()
	maxActive := st.cfg.MaxActivePerSession
	if p.MaxActivePerSession > 0 {
		maxActive = p.MaxActivePerSession
	}

	s := newSession(SessionConfig{
		ID:                  id,
		Catalog:             st.cfg.Catalog,
		Providers:           st.cfg.Providers,
		MaxActivePerSession: maxActive,
		Touch:               func() { st.touch(id) },
	})

	e := st.lru.PushFront(&item{s: s, lastUsed: now})
	st.m[id] = e

	st.evictOverLimitLocked()
	st.mu.Unlock()

	if len(p.ActiveKeys) == 0 {
		return id, nil, nil
	}

	handles, err := s.ActivateKeys(ctx, p.ActiveKeys, spec.LoadModeReplace)
	if err != nil {
		st.Delete(id)
		return "", nil, err
	}
	return id, handles, nil
}

func (st *Store) Get(id string) (*Session, bool) {
	now := time.Now()

	st.mu.Lock()
	defer st.mu.Unlock()

	st.evictExpiredLocked(now)

	e := st.m[id]
	if e == nil {
		return nil, false
	}
	it, _ := e.Value.(*item)
	if it == nil || it.s == nil || it.s.closed.Load() {

		st.deleteElemLocked(e)
		return nil, false
	}

	it.lastUsed = now
	st.lru.MoveToFront(e)
	return it.s, true
}

func (st *Store) Delete(id string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if e := st.m[id]; e != nil {
		st.deleteElemLocked(e)
	}
}

// PruneSkill removes the given key from all sessions' active lists.
func (st *Store) PruneSkill(key spec.ProviderSkillKey) {
	// Collect sessions under store lock, then prune outside to avoid holding the
	// global lock while taking per-session locks.
	st.mu.Lock()
	sessions := make([]*Session, 0, st.lru.Len())
	for e := st.lru.Front(); e != nil; e = e.Next() {
		it, _ := e.Value.(*item)
		if it == nil || it.s == nil || it.s.closed.Load() {
			continue
		}
		sessions = append(sessions, it.s)
	}
	st.mu.Unlock()

	for _, s := range sessions {
		if s == nil || s.closed.Load() {
			continue
		}
		s.pruneKey(key)
	}
}

// touch updates lastUsed and MRU position for an existing session.
// Safe to call frequently; does not allocate.
func (st *Store) touch(id string) {
	now := time.Now()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.evictExpiredLocked(now)

	e := st.m[id]
	if e == nil {
		return
	}
	it, _ := e.Value.(*item)
	if it == nil || it.s == nil || it.s.closed.Load() {
		st.deleteElemLocked(e)
		return
	}
	it.lastUsed = now
	st.lru.MoveToFront(e)
}

func (st *Store) evictOverLimitLocked() {
	if st.maxSessions <= 0 {
		return
	}
	for st.lru.Len() > st.maxSessions {
		e := st.lru.Back()
		if e == nil {
			return
		}
		st.deleteElemLocked(e)
	}
}

func (st *Store) evictExpiredLocked(now time.Time) {
	if st.ttl <= 0 {
		return
	}
	for e := st.lru.Back(); e != nil; {
		prev := e.Prev()
		it, ok := e.Value.(*item)
		if !ok || it == nil || it.s == nil {
			st.deleteElemLocked(e)
			e = prev
			continue
		}
		if now.Sub(it.lastUsed) <= st.ttl {
			break
		}
		st.deleteElemLocked(e)
		e = prev
	}
}

func (st *Store) deleteElemLocked(e *list.Element) {
	it, _ := e.Value.(*item)
	if it != nil && it.s != nil {
		delete(st.m, it.s.id)
		it.s.closed.Store(true)

	}
	st.lru.Remove(e)
}
