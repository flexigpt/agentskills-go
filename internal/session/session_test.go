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
