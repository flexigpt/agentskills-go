package sessionstore

import (
	"container/list"
	"sync"
	"time"

	"github.com/flexigpt/llmtools-go/shelltool"
	"github.com/google/uuid"
)

type ShellBinding struct {
	Tool           *shelltool.ShellTool
	ShellSessionID string
}

type Session struct {
	ID string

	Mu sync.Mutex

	ActiveSkills []string

	// One shell binding per skill name (per spec request: per session/skill shell tool).
	shellBySkill map[string]*ShellBinding

	closed bool
}

func (s *Session) ShellBindingForSkill(name string) *ShellBinding {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.shellBySkill == nil {
		s.shellBySkill = map[string]*ShellBinding{}
	}
	b := s.shellBySkill[name]
	if b == nil {
		b = &ShellBinding{}
		s.shellBySkill[name] = b
	}
	return b
}

type Store struct {
	mu sync.Mutex

	ttl         time.Duration
	maxSessions int

	lru *list.List               // front=MRU
	m   map[string]*list.Element // id -> element(Value=*item)
}

type item struct {
	s        *Session
	lastUsed time.Time
}

const (
	defaultTTL = 24 * time.Hour
	defaultMax = 4096
)

func New() *Store {
	return &Store{
		ttl:         defaultTTL,
		maxSessions: defaultMax,
		lru:         list.New(),
		m:           map[string]*list.Element{},
	}
}

func (st *Store) SetTTL(ttl time.Duration) {
	if ttl < 0 {
		ttl = 0
	}
	st.mu.Lock()
	st.ttl = ttl
	st.evictExpiredLocked(time.Now())
	st.mu.Unlock()
}

func (st *Store) SetMaxSessions(maxSessions int) {
	if maxSessions < 0 {
		maxSessions = 0
	}
	st.mu.Lock()
	st.maxSessions = maxSessions
	st.evictOverLimitLocked()
	st.mu.Unlock()
}

func (st *Store) NewSession() *Session {
	now := time.Now()
	st.mu.Lock()
	defer st.mu.Unlock()

	st.evictExpiredLocked(now)
	st.evictOverLimitLocked()

	id := uuid.Must(uuid.NewV7()).String()
	s := &Session{ID: id}
	e := st.lru.PushFront(&item{s: s, lastUsed: now})
	st.m[id] = e

	st.evictOverLimitLocked()
	return s
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
	if it == nil || it.s == nil || it.s.closed {
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
		delete(st.m, it.s.ID)
		it.s.closed = true
	}
	st.lru.Remove(e)
}
