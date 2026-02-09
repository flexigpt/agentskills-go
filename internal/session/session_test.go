package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

type memCatalog struct {
	mu sync.Mutex

	recs    map[spec.SkillKey]spec.SkillRecord
	bodies  map[spec.SkillKey]string
	handles map[spec.SkillKey]spec.SkillHandle

	ensureFn func(context.Context, spec.SkillKey) (string, error)
}

func newMemCatalog() *memCatalog {
	return &memCatalog{
		recs:    map[spec.SkillKey]spec.SkillRecord{},
		bodies:  map[spec.SkillKey]string{},
		handles: map[spec.SkillKey]spec.SkillHandle{},
	}
}

func (c *memCatalog) ResolveHandle(h spec.SkillHandle) (spec.SkillKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, hh := range c.handles {
		if hh == h {
			return k, true
		}
	}
	return spec.SkillKey{}, false
}

func (c *memCatalog) HandleForKey(key spec.SkillKey) (spec.SkillHandle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.handles[key]
	return h, ok
}

func (c *memCatalog) EnsureBody(ctx context.Context, key spec.SkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.ensureFn != nil {
		return c.ensureFn(ctx, key)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.bodies[key]
	if !ok {
		return "", spec.ErrSkillNotFound
	}
	return b, nil
}

func (c *memCatalog) Get(key spec.SkillKey) (spec.SkillRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.recs[key]
	return r, ok
}

func (c *memCatalog) add(k spec.SkillKey, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs[k] = spec.SkillRecord{Key: k, Description: "d-" + k.Name}
	c.bodies[k] = body
	c.handles[k] = spec.SkillHandle{Name: k.Name, Path: k.Path}
}

type mapResolver map[string]spec.SkillProvider

func (r mapResolver) Provider(skillType string) (spec.SkillProvider, bool) {
	p, ok := r[skillType]
	return p, ok
}

type canonProvider struct {
	typ string
	// If key.Path == "rel", normalize to "abs".
}

func (p *canonProvider) Type() string { return p.typ }

func (p *canonProvider) Index(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	if key.Path == "rel" {
		key.Path = "abs"
	}
	if strings.TrimSpace(key.Type) == "" || strings.TrimSpace(key.Name) == "" || strings.TrimSpace(key.Path) == "" {
		return spec.SkillRecord{}, fmt.Errorf("%w: invalid", spec.ErrInvalidArgument)
	}
	return spec.SkillRecord{Key: key, Description: "d"}, nil
}

func (p *canonProvider) LoadBody(ctx context.Context, key spec.SkillKey) (string, error) {
	return "body", nil
}

func (p *canonProvider) ReadResource(
	ctx context.Context,
	key spec.SkillKey,
	resourcePath string,
	encoding spec.ReadEncoding,
) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	return nil, spec.ErrInvalidArgument
}

func (p *canonProvider) RunScript(
	ctx context.Context,
	key spec.SkillKey,
	scriptPath string,
	args []string,
	env map[string]string,
	workdir string,
) (spec.RunScriptResult, error) {
	return spec.RunScriptResult{}, spec.ErrRunScriptUnsupported
}

func TestSession_ActivateKeys_ReplaceAddDedupeAndCanonicalize(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	k1 := spec.SkillKey{Type: "t", Name: "a", Path: "abs"}
	k2 := spec.SkillKey{Type: "t", Name: "b", Path: "p2"}
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
	hs, err = s.ActivateKeys(t.Context(), []spec.SkillKey{{Type: "t", Name: "a", Path: "rel"}}, spec.LoadModeReplace)
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
	k1 := spec.SkillKey{Type: "t", Name: "a", Path: "p1"}
	k2 := spec.SkillKey{Type: "t", Name: "b", Path: "p2"}
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
	k1 := spec.SkillKey{Type: "t", Name: "a", Path: "p1"}
	k2 := spec.SkillKey{Type: "t", Name: "b", Path: "p2"}
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
	k1 := spec.SkillKey{Type: "t", Name: "a", Path: "p1"}
	k2 := spec.SkillKey{Type: "t", Name: "b", Path: "p2"}
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
