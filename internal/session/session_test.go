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

func TestSession_ActivateKeys_ReplaceAddDedupeAndCanonicalize(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	k1 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "a", Location: "abs"}}
	k2 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "b", Location: "p2"}}
	cat.add(k1, "B1")
	cat.add(k2, "B2")

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	hs, err := s.ActivateKeys(t.Context(), []spec.SkillKey{k1}, spec.LoadModeReplace)
	if err != nil {
		t.Fatalf("ActivateKeys replace: %v", err)
	}
	if len(hs) != 1 || hs[0].Name != "a" {
		t.Fatalf("unexpected handles: %+v", hs)
	}

	hs, err = s.ActivateKeys(t.Context(), []spec.SkillKey{k1, k2, k2}, spec.LoadModeAdd)
	if err != nil {
		t.Fatalf("ActivateKeys add: %v", err)
	}
	if len(hs) != 2 || hs[0].Name != "a" || hs[1].Name != "b" {
		t.Fatalf("unexpected handles after add: %+v", hs)
	}

	// Canonicalize: request with non-canonical key not in catalog, provider.Index normalizes to abs and matches.
	hs, err = s.ActivateKeys(
		t.Context(),
		[]spec.SkillKey{{Type: "t", SkillHandle: spec.SkillHandle{Name: "a", Location: "rel"}}},
		spec.LoadModeReplace,
	)
	if err != nil {
		t.Fatalf("ActivateKeys canonicalize: %v", err)
	}
	if len(hs) != 1 || hs[0].Name != "a" {
		t.Fatalf("unexpected handles after canonicalize: %+v", hs)
	}
}

func TestSession_ActivateKeys_EnsureBodyErrorDoesNotCommit(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	k1 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "a", Location: "p1"}}
	k2 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "b", Location: "p2"}}
	cat.add(k1, "ok")
	cat.add(k2, "ok")

	cat.ensureFn = func(ctx context.Context, k spec.SkillKey) (string, error) {
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

	_, err := s.ActivateKeys(t.Context(), []spec.SkillKey{k1}, spec.LoadModeReplace)
	if err != nil {
		t.Fatalf("activate k1: %v", err)
	}

	_, err = s.ActivateKeys(t.Context(), []spec.SkillKey{k2}, spec.LoadModeAdd)
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
	t.Parallel()

	cat := newMemCatalog()
	k1 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "a", Location: "p1"}}
	k2 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "b", Location: "p2"}}
	cat.add(k1, "ok")
	cat.add(k2, "ok")

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 1,
		Touch:               func() {},
	})

	_, err := s.ActivateKeys(t.Context(), []spec.SkillKey{k1, k2}, spec.LoadModeReplace)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestSession_ActivateKeys_RetriesOnConcurrentModification(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	k1 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "a", Location: "p1"}}
	k2 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "b", Location: "p2"}}
	cat.add(k1, "ok")
	cat.add(k2, "ok")

	block := make(chan struct{})
	release := make(chan struct{})
	var blockOnce sync.Once

	cat.ensureFn = func(ctx context.Context, k spec.SkillKey) (string, error) {
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

	_, err := s.ActivateKeys(t.Context(), []spec.SkillKey{k1}, spec.LoadModeReplace)
	if err != nil {
		t.Fatalf("activate k1: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.ActivateKeys(t.Context(), []spec.SkillKey{k2}, spec.LoadModeAdd)
		done <- err
	}()

	<-block // ensure ActivateKeys is between snapshot and commit (blocked in EnsureBody)

	// Concurrent mutation: unload all.
	_, uerr := s.toolUnload(t.Context(), spec.UnloadArgs{All: true})
	if uerr != nil {
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
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	cat := newToggleCatalog()
	k := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "a", Location: "p1"}}
	cat.put(k, "body")

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": &canonProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	if _, err := s.ActivateKeys(ctx, []spec.SkillKey{k}, spec.LoadModeReplace); err != nil {
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
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	cat := newToggleCatalog()
	k1 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "a", Location: "p1"}}
	k2 := spec.SkillKey{Type: "t", SkillHandle: spec.SkillHandle{Name: "b", Location: "p2"}}
	cat.put(k1, "body-a")
	cat.put(k2, "body-b")

	block := make(chan struct{})
	release := make(chan struct{})
	cat.ensureFn = func(ctx context.Context, k spec.SkillKey) (string, error) {
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
	if _, err := s.ActivateKeys(ctx, []spec.SkillKey{k1}, spec.LoadModeReplace); err != nil {
		t.Fatalf("ActivateKeys(k1): %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.ActivateKeys(ctx, []spec.SkillKey{k2}, spec.LoadModeAdd)
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

type toggleCatalog struct {
	mu       sync.Mutex
	recs     map[spec.SkillKey]spec.SkillRecord
	bodies   map[spec.SkillKey]string
	ensureFn func(ctx context.Context, k spec.SkillKey) (string, error)
}

func newToggleCatalog() *toggleCatalog {
	return &toggleCatalog{
		recs:   map[spec.SkillKey]spec.SkillRecord{},
		bodies: map[spec.SkillKey]string{},
	}
}

func (c *toggleCatalog) ResolveHandle(h spec.SkillHandle) (spec.SkillKey, bool) {
	// Not needed for these tests.
	return spec.SkillKey{}, false
}

func (c *toggleCatalog) HandleForKey(key spec.SkillKey) (spec.SkillHandle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.recs[key]; !ok {
		return spec.SkillHandle{}, false
	}
	return spec.SkillHandle{Name: key.Name, Location: key.Location}, true
}

func (c *toggleCatalog) EnsureBody(ctx context.Context, key spec.SkillKey) (string, error) {
	if c.ensureFn != nil {
		return c.ensureFn(ctx, key)
	}
	return c.defaultEnsureBody(ctx, key)
}

func (c *toggleCatalog) Get(key spec.SkillKey) (spec.SkillRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rec, ok := c.recs[key]
	return rec, ok
}

func (c *toggleCatalog) put(k spec.SkillKey, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs[k] = spec.SkillRecord{Key: k, Description: "d:" + k.Name}
	c.bodies[k] = body
}

func (c *toggleCatalog) remove(k spec.SkillKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.recs, k)
	delete(c.bodies, k)
}

func (c *toggleCatalog) defaultEnsureBody(ctx context.Context, key spec.SkillKey) (string, error) {
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
