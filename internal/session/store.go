package session

import (
	"container/list"
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

func (st *Store) NewSessionID() string {
	now := time.Now()

	st.mu.Lock()
	defer st.mu.Unlock()

	st.evictExpiredLocked(now)
	st.evictOverLimitLocked()

	id := uuid.Must(uuid.NewV7()).String()
	s := newSession(SessionConfig{
		ID:                  id,
		Catalog:             st.cfg.Catalog,
		Providers:           st.cfg.Providers,
		MaxActivePerSession: st.cfg.MaxActivePerSession,
		Touch:               func() { st.touch(id) },
	})

	e := st.lru.PushFront(&item{s: s, lastUsed: now})
	st.m[id] = e

	st.evictOverLimitLocked()

	return id
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
func (st *Store) PruneSkill(key spec.SkillKey) {
	ks := key.Type + "\n" + key.Name + "\n" + key.Path

	st.mu.Lock()
	defer st.mu.Unlock()

	for e := st.lru.Front(); e != nil; e = e.Next() {
		it, _ := e.Value.(*item)
		if it == nil || it.s == nil || it.s.closed.Load() {
			continue
		}
		it.s.pruneKey(ks)
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

func (st *Store) deleteElemLocked(e *list.Element) {
	it, _ := e.Value.(*item)
	if it != nil && it.s != nil {
		delete(st.m, it.s.id)
		it.s.closed.Store(true)

	}
	st.lru.Remove(e)
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
