package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestSession_ActivateKeys_ReplaceAdd_DedupeAndOrdering(t *testing.T) {
	cat := newMemCatalog()
	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "p2"}
	cat.add(k1, "B1")
	cat.add(k2, "B2")

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	steps := []struct {
		name string
		do   func(ctx context.Context) ([]spec.SkillHandle, error)
		want []spec.SkillHandle
	}{
		{
			name: "replace",
			do: func(ctx context.Context) ([]spec.SkillHandle, error) {
				return s.ActivateKeys(ctx, []spec.ProviderSkillKey{k1}, spec.LoadModeReplace)
			},
			want: []spec.SkillHandle{{Name: "a", Location: "p1"}},
		},
		{
			name: "add_dedupes",
			do: func(ctx context.Context) ([]spec.SkillHandle, error) {
				return s.ActivateKeys(ctx, []spec.ProviderSkillKey{k1, k2, k2}, spec.LoadModeAdd)
			},
			want: []spec.SkillHandle{
				{Name: "a", Location: "p1"},
				{Name: "b", Location: "p2"},
			},
		},
		{
			name: "add_moves_requested_to_end",
			do: func(ctx context.Context) ([]spec.SkillHandle, error) {
				// Current order: [a,b]. Add [a] should become [b,a].
				return s.ActivateKeys(ctx, []spec.ProviderSkillKey{k1}, spec.LoadModeAdd)
			},
			want: []spec.SkillHandle{
				{Name: "b", Location: "p2"},
				{Name: "a", Location: "p1"},
			},
		},
		{
			name: "empty_mode_defaults_to_replace",
			do: func(ctx context.Context) ([]spec.SkillHandle, error) {
				return s.ActivateKeys(ctx, []spec.ProviderSkillKey{k2}, spec.LoadMode(""))
			},
			want: []spec.SkillHandle{{Name: "b", Location: "p2"}},
		},
	}

	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			hs, err := st.do(t.Context())
			if err != nil {
				t.Fatalf("ActivateKeys: %v", err)
			}
			if len(hs) != len(st.want) {
				t.Fatalf("unexpected handle count: got=%d want=%d handles=%+v", len(hs), len(st.want), hs)
			}
			for i := range hs {
				if hs[i] != st.want[i] {
					t.Fatalf("unexpected handles[%d]: got=%+v want=%+v all=%+v", i, hs[i], st.want[i], hs)
				}
			}
		})
	}
}

func TestSession_ActivateKeys_EnsureBodyErrorDoesNotCommit(t *testing.T) {
	cat := newMemCatalog()
	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "p2"}
	cat.add(k1, "ok")
	cat.add(k2, "ok")

	cat.ensureFn = func(ctx context.Context, k spec.ProviderSkillKey) (string, error) {
		if k == k2 {
			return "", errors.New("boom")
		}
		return "ok", nil
	}

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	if _, err := s.ActivateKeys(t.Context(), []spec.ProviderSkillKey{k1}, spec.LoadModeReplace); err != nil {
		t.Fatalf("activate k1: %v", err)
	}

	_, err := s.ActivateKeys(t.Context(), []spec.ProviderSkillKey{k2}, spec.LoadModeAdd)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected boom error, got %v", err)
	}

	// State should still be only k1 active.
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeOrder) != 1 || s.activeOrder[0] != k1 {
		t.Fatalf("unexpected state committed on EnsureBody failure: %+v", s.activeOrder)
	}
}

func TestSession_ActivateKeys_MaxActiveIsInvalidArgument(t *testing.T) {
	cat := newMemCatalog()
	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "p2"}
	cat.add(k1, "ok")
	cat.add(k2, "ok")

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 1,
		Touch:               func() {},
	})

	_, err := s.ActivateKeys(t.Context(), []spec.ProviderSkillKey{k1, k2}, spec.LoadModeReplace)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestSession_ActivateKeys_RetriesOnConcurrentModification(t *testing.T) {
	cat := newMemCatalog()
	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "p2"}
	cat.add(k1, "ok")
	cat.add(k2, "ok")

	block := make(chan struct{})
	release := make(chan struct{})
	var blockOnce sync.Once

	cat.ensureFn = func(ctx context.Context, k spec.ProviderSkillKey) (string, error) {
		if k == k2 {
			// ActivateKeys may retry and call EnsureBody multiple times.
			// We only want to create the blocking window once.
			blockOnce.Do(func() {
				close(block)
				<-release
			})
		}
		return "ok", nil
	}

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	if _, err := s.ActivateKeys(t.Context(), []spec.ProviderSkillKey{k1}, spec.LoadModeReplace); err != nil {
		t.Fatalf("activate k1: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.ActivateKeys(t.Context(), []spec.ProviderSkillKey{k2}, spec.LoadModeAdd)
		done <- err
	}()

	<-block // ensure ActivateKeys is between snapshot and commit (blocked in EnsureBody)

	// Concurrent mutation: unload all.
	if _, uerr := s.toolUnload(t.Context(), spec.UnloadArgs{All: true}); uerr != nil {
		t.Fatalf("toolUnload(all): %v", uerr)
	}

	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ActivateKeys returned err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for ActivateKeys")
	}

	// Final state should reflect the concurrent unload + subsequent add => only k2 active.
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeOrder) != 1 || s.activeOrder[0] != k2 {
		t.Fatalf("unexpected final state: %+v", s.activeOrder)
	}
}

func TestSession_ActiveKeys_PrunesMissingCatalogSkills(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	cat := newToggleCatalog()
	k := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	cat.put(k, "body")

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	if _, err := s.ActivateKeys(ctx, []spec.ProviderSkillKey{k}, spec.LoadModeReplace); err != nil {
		t.Fatalf("ActivateKeys: %v", err)
	}

	// Remove from catalog; ActiveKeys should prune session state and return empty.
	cat.remove(k)

	keys, err := s.ActiveKeys(ctx)
	if err != nil {
		t.Fatalf("ActiveKeys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 active keys after prune, got %v", keys)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeOrder) != 0 {
		t.Fatalf("expected activeOrder pruned to empty, got %+v", s.activeOrder)
	}
}

func TestSession_ActivateKeys_SkillRemovedDuringActivation_DoesNotCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	cat := newToggleCatalog()
	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "p2"}
	cat.put(k1, "body-a")
	cat.put(k2, "body-b")

	block := make(chan struct{})
	release := make(chan struct{})
	cat.ensureFn = func(ctx context.Context, k spec.ProviderSkillKey) (string, error) {
		if k == k2 {
			close(block)
			<-release
		}
		return cat.defaultEnsureBody(ctx, k)
	}

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	// Baseline active set.
	if _, err := s.ActivateKeys(ctx, []spec.ProviderSkillKey{k1}, spec.LoadModeReplace); err != nil {
		t.Fatalf("ActivateKeys(k1): %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.ActivateKeys(ctx, []spec.ProviderSkillKey{k2}, spec.LoadModeAdd)
		done <- err
	}()

	<-block

	// Simulate concurrent removal from catalog while activation is in-flight.
	cat.remove(k2)
	close(release)

	err := <-done
	if !errors.Is(err, spec.ErrSkillNotFound) {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}

	// Ensure session state was not committed with missing key.
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeOrder) != 1 || s.activeOrder[0] != k1 {
		t.Fatalf("expected only k1 active after failed activation, got %+v", s.activeOrder)
	}
}

func TestSession_ActivateKeys_ValidationErrors(t *testing.T) {
	cat := newMemCatalog()
	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "p1"}
	cat.add(k1, "ok")

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	cases := []struct {
		name   string
		ctx    context.Context
		closed bool
		keys   []spec.ProviderSkillKey
		mode   spec.LoadMode
		isErr  func(error) bool
	}{
		{
			name: "invalid_mode",
			ctx:  t.Context(),
			keys: []spec.ProviderSkillKey{k1},
			mode: spec.LoadMode("nope"),
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "empty_keys",
			ctx:  t.Context(),
			keys: nil,
			mode: spec.LoadModeReplace,
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "unknown_key",
			ctx:  t.Context(),
			keys: []spec.ProviderSkillKey{{Type: "t", Name: "missing", Location: "p2"}},
			mode: spec.LoadModeReplace,
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrSkillNotFound)
			},
		},
		{
			name: "closed_session",
			ctx:  t.Context(),
			keys: []spec.ProviderSkillKey{k1},
			mode: spec.LoadModeReplace,
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrSessionNotFound)
			},
			closed: true,
		},
		{
			name: "ctx_canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			}(),
			keys: []spec.ProviderSkillKey{k1},
			mode: spec.LoadModeReplace,
			isErr: func(err error) bool {
				return errors.Is(err, context.Canceled)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.closed {
				s.closed.Store(true)
				t.Cleanup(func() { s.closed.Store(false) })
			}

			_, err := s.ActivateKeys(tc.ctx, tc.keys, tc.mode)
			if err == nil || !tc.isErr(err) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

type toggleCatalog struct {
	mu      sync.Mutex
	indexes map[spec.ProviderSkillKey]spec.ProviderSkillIndexRecord
	bodies  map[spec.ProviderSkillKey]string
	handles map[spec.ProviderSkillKey]spec.SkillHandle

	ensureFn func(ctx context.Context, k spec.ProviderSkillKey) (string, error)
}

func newToggleCatalog() *toggleCatalog {
	return &toggleCatalog{
		indexes: map[spec.ProviderSkillKey]spec.ProviderSkillIndexRecord{},
		bodies:  map[spec.ProviderSkillKey]string{},
		handles: map[spec.ProviderSkillKey]spec.SkillHandle{},
	}
}

func (c *toggleCatalog) ResolveHandle(h spec.SkillHandle) (spec.ProviderSkillKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, hh := range c.handles {
		if hh == h {
			return k, true
		}
	}
	return spec.ProviderSkillKey{}, false
}

func (c *toggleCatalog) HandleForKey(key spec.ProviderSkillKey) (spec.SkillHandle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.handles[key]
	return h, ok
}

func (c *toggleCatalog) EnsureBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	if c.ensureFn != nil {
		return c.ensureFn(ctx, key)
	}
	return c.defaultEnsureBody(ctx, key)
}

func (c *toggleCatalog) GetIndex(key spec.ProviderSkillKey) (spec.ProviderSkillIndexRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rec, ok := c.indexes[key]
	return rec, ok
}

func (c *toggleCatalog) put(k spec.ProviderSkillKey, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.indexes[k] = spec.ProviderSkillIndexRecord{Key: k, Description: "d:" + k.Name}
	c.bodies[k] = body
	c.handles[k] = spec.SkillHandle{Name: k.Name, Location: k.Location}
}

func (c *toggleCatalog) remove(k spec.ProviderSkillKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.indexes, k)
	delete(c.bodies, k)
	delete(c.handles, k)
}

func (c *toggleCatalog) defaultEnsureBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.bodies[key]
	if !ok {
		return "", spec.ErrSkillNotFound
	}
	return b, nil
}
