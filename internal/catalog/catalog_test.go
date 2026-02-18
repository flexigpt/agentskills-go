package catalog

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestCatalog_Add_ValidationAndProviderErrors(t *testing.T) {
	t.Parallel()

	baseProvider := &testProvider{typ: "t"}

	tests := []struct {
		name      string
		ctx       func() context.Context
		def       spec.SkillDef
		resolver  ProviderResolver
		wantIsErr error
		wantSub   string
	}{
		{
			name: "context canceled short-circuits",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			def:       spec.SkillDef{Type: "t", Name: "n", Location: "/p"},
			resolver:  mapResolver{"t": baseProvider},
			wantIsErr: context.Canceled,
		},
		{
			name:      "missing type",
			ctx:       t.Context,
			def:       spec.SkillDef{Type: "", Name: "n", Location: "/p"},
			resolver:  mapResolver{"t": baseProvider},
			wantIsErr: spec.ErrInvalidArgument,
		},
		{
			name:      "missing name",
			ctx:       t.Context,
			def:       spec.SkillDef{Type: "t", Name: "", Location: "/p"},
			resolver:  mapResolver{"t": baseProvider},
			wantIsErr: spec.ErrInvalidArgument,
		},
		{
			name:      "missing location",
			ctx:       t.Context,
			def:       spec.SkillDef{Type: "t", Name: "n", Location: ""},
			resolver:  mapResolver{"t": baseProvider},
			wantIsErr: spec.ErrInvalidArgument,
		},
		{
			name:      "provider not found",
			ctx:       t.Context,
			def:       spec.SkillDef{Type: "missing", Name: "n", Location: "/p"},
			resolver:  mapResolver{"t": baseProvider},
			wantIsErr: spec.ErrProviderNotFound,
			wantSub:   "unknown provider type",
		},
		{
			name: "provider Index error is returned",
			ctx:  t.Context,
			def:  spec.SkillDef{Type: "t", Name: "n", Location: "/p"},
			resolver: mapResolver{"t": &testProvider{
				typ: "t",
				indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
					return spec.ProviderSkillIndexRecord{}, errors.New("index-failed")
				},
			}},

			wantSub: "index-failed",
		},
		{
			name: "provider changes type",
			ctx:  t.Context,
			def:  spec.SkillDef{Type: "t", Name: "n", Location: "/p"},
			resolver: mapResolver{"t": &testProvider{
				typ: "t",
				indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
					return spec.ProviderSkillIndexRecord{
						Key:         spec.ProviderSkillKey{Type: "other", Name: def.Name, Location: def.Location},
						Description: "x",
					}, nil
				},
			}},
			wantIsErr: spec.ErrInvalidArgument,
			wantSub:   "provider changed type",
		},
		{
			name: "provider changes name",
			ctx:  t.Context,
			def:  spec.SkillDef{Type: "t", Name: "n", Location: "/p"},
			resolver: mapResolver{"t": &testProvider{
				typ: "t",
				indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
					return spec.ProviderSkillIndexRecord{
						Key:         spec.ProviderSkillKey{Type: def.Type, Name: "other", Location: def.Location},
						Description: "x",
					}, nil
				},
			}},
			wantIsErr: spec.ErrInvalidArgument,
			wantSub:   "provider changed name",
		},
		{
			name: "provider returns invalid key (blank canonical location)",
			ctx:  t.Context,
			def:  spec.SkillDef{Type: "t", Name: "n", Location: "/user"},
			resolver: mapResolver{"t": &testProvider{
				typ: "t",
				indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
					return spec.ProviderSkillIndexRecord{
						Key:         spec.ProviderSkillKey{Type: def.Type, Name: def.Name, Location: "   "},
						Description: "x",
					}, nil
				},
			}},
			wantIsErr: spec.ErrInvalidArgument,
			wantSub:   "provider returned invalid record key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cat := New(tt.resolver)
			_, err := cat.Add(tt.ctx(), tt.def)

			if err == nil {
				t.Fatalf("expected error")
			}
			if tt.wantIsErr != nil && !errors.Is(err, tt.wantIsErr) {
				t.Fatalf("expected errors.Is(err,%v)=true, got err=%v", tt.wantIsErr, err)
			}
			if tt.wantSub != "" && !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("expected err to contain %q, got %v", tt.wantSub, err)
			}
		})
	}
}

func TestCatalog_Add_Duplicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		defs      []spec.SkillDef
		provider  spec.SkillProvider
		wantIsErr error
	}{
		{
			name: "same exact def twice",
			defs: []spec.SkillDef{
				{Type: "t", Name: "n", Location: "/p"},
				{Type: "t", Name: "n", Location: "/p"},
			},
			provider:  &testProvider{typ: "t"},
			wantIsErr: spec.ErrSkillAlreadyExists,
		},
		{
			name: "different user def, but provider canonicalizes to same internal key",
			defs: []spec.SkillDef{
				{Type: "t", Name: "n", Location: "/user-A"},
				{Type: "t", Name: "n", Location: "/user-B"},
			},
			provider: &testProvider{
				typ: "t",
				indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
					return spec.ProviderSkillIndexRecord{
						Key:         spec.ProviderSkillKey{Type: def.Type, Name: def.Name, Location: "/CANON"},
						Description: "x",
					}, nil
				},
			},
			wantIsErr: spec.ErrSkillAlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cat := New(mapResolver{"t": tt.provider})
			for i, def := range tt.defs {
				_, err := cat.Add(t.Context(), def)
				if i < len(tt.defs)-1 {
					if err != nil {
						t.Fatalf("Add[%d] unexpected err: %v", i, err)
					}
					continue
				}
				if err == nil {
					t.Fatalf("expected error on last Add")
				}
				if !errors.Is(err, tt.wantIsErr) {
					t.Fatalf("expected errors.Is(err,%v)=true, got err=%v", tt.wantIsErr, err)
				}
			}
		})
	}
}

func TestCatalog_Handles_DisambiguateOnNameAndUserLocationCollision_AndResolve(t *testing.T) {
	t.Parallel()

	pa := &testProvider{typ: "a"}
	pb := &testProvider{typ: "b"}
	c := New(mapResolver{"a": pa, "b": pb})

	defA := spec.SkillDef{Type: "a", Name: "same", Location: "/p"}
	defB := spec.SkillDef{Type: "b", Name: "same", Location: "/p"}

	if _, err := c.Add(t.Context(), defA); err != nil {
		t.Fatalf("Add A: %v", err)
	}
	if _, err := c.Add(t.Context(), defB); err != nil {
		t.Fatalf("Add B: %v", err)
	}

	keyA, ok := c.ResolveDef(defA)
	if !ok {
		t.Fatalf("ResolveDef(A) failed")
	}
	keyB, ok := c.ResolveDef(defB)
	if !ok {
		t.Fatalf("ResolveDef(B) failed")
	}

	ha, ok := c.HandleForKey(keyA)
	if !ok {
		t.Fatalf("HandleForKey(A) not found")
	}
	hb, ok := c.HandleForKey(keyB)
	if !ok {
		t.Fatalf("HandleForKey(B) not found")
	}

	if ha.Location != "/p" || hb.Location != "/p" {
		t.Fatalf("expected user location to be returned, got ha=%+v hb=%+v", ha, hb)
	}

	if ha.Name == hb.Name {
		t.Fatalf("expected disambiguated names, got ha=%+v hb=%+v", ha, hb)
	}

	re := regexp.MustCompile(`^same#[0-9a-f]{8}$`)
	if !re.MatchString(ha.Name) || !re.MatchString(hb.Name) {
		t.Fatalf("expected opaque hash suffix, got ha=%+v hb=%+v", ha, hb)
	}

	// ResolveHandle trims spaces (so callers can be sloppy).
	haSpaced := spec.SkillHandle{Name: "  " + ha.Name + "  ", Location: " " + ha.Location + " "}
	rka, ok := c.ResolveHandle(haSpaced)
	if !ok || rka != keyA {
		t.Fatalf("ResolveHandle(haSpaced) failed: ok=%v got=%+v want=%+v", ok, rka, keyA)
	}
	rkb, ok := c.ResolveHandle(hb)
	if !ok || rkb != keyB {
		t.Fatalf("ResolveHandle(hb) failed: ok=%v got=%+v want=%+v", ok, rkb, keyB)
	}
}

func TestCatalog_HandleForKey_DoesNotLeakCanonicalLocation(t *testing.T) {
	t.Parallel()

	p := &testProvider{
		typ: "t",
		indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
			// Canonical location differs from user-provided def.Location.
			return spec.ProviderSkillIndexRecord{
				Key:         spec.ProviderSkillKey{Type: def.Type, Name: def.Name, Location: "/CANONICAL/LOCATION"},
				Description: "x",
			}, nil
		},
		loadBodyFn: func(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
			// EnsureBody must pass the canonical key to providers.
			if key.Location != "/CANONICAL/LOCATION" {
				return "", errors.New("expected canonical key.location")
			}
			return "OK", nil
		},
	}

	c := New(mapResolver{"t": p})

	def := spec.SkillDef{Type: "t", Name: "n", Location: "/user/location"}
	if _, err := c.Add(t.Context(), def); err != nil {
		t.Fatalf("Add: %v", err)
	}

	key, ok := c.ResolveDef(def)
	if !ok {
		t.Fatalf("ResolveDef failed")
	}
	if key.Location != "/CANONICAL/LOCATION" {
		t.Fatalf("expected canonical key location from ResolveDef, got %+v", key)
	}

	h, ok := c.HandleForKey(key)
	if !ok {
		t.Fatalf("HandleForKey not found")
	}
	if h.Location != def.Location {
		t.Fatalf("expected HandleForKey to return user location %q, got %q", def.Location, h.Location)
	}

	body, err := c.EnsureBody(t.Context(), key)
	if err != nil {
		t.Fatalf("EnsureBody: %v", err)
	}
	if body != "OK" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestCatalog_Remove_RecomputesLLMNamesAndIndexes(t *testing.T) {
	t.Parallel()

	pa := &testProvider{typ: "a"}
	pb := &testProvider{typ: "b"}
	c := New(mapResolver{"a": pa, "b": pb})

	// Collide by (name, user location) => both get "#<hash>".
	defA := spec.SkillDef{Type: "a", Name: "same", Location: "/p"}
	defB := spec.SkillDef{Type: "b", Name: "same", Location: "/p"}

	if _, err := c.Add(t.Context(), defA); err != nil {
		t.Fatalf("Add A: %v", err)
	}
	if _, err := c.Add(t.Context(), defB); err != nil {
		t.Fatalf("Add B: %v", err)
	}

	keyB, ok := c.ResolveDef(defB)
	if !ok {
		t.Fatalf("ResolveDef(B) failed")
	}

	oldHB, ok := c.HandleForKey(keyB)
	if !ok {
		t.Fatalf("HandleForKey(B) failed")
	}
	if !strings.HasPrefix(oldHB.Name, "same#") {
		t.Fatalf("expected disambiguated name before removal, got %+v", oldHB)
	}

	// Remove A: B should revert to plain "same" (no collision group anymore).
	rec, removedKey, ok := c.Remove(defA)
	if !ok {
		t.Fatalf("Remove(A) expected ok=true")
	}
	if rec.Def != defA {
		t.Fatalf("Remove(A) returned wrong record: got %+v want %+v", rec.Def, defA)
	}
	if removedKey.Type != "a" || removedKey.Name != "same" {
		t.Fatalf("unexpected removed canonical key: %+v", removedKey)
	}

	newHB, ok := c.HandleForKey(keyB)
	if !ok {
		t.Fatalf("HandleForKey(B) after removal failed")
	}
	if newHB.Name != "same" || newHB.Location != "/p" {
		t.Fatalf("expected reverted handle after removal, got %+v", newHB)
	}

	// Old handle must no longer resolve.
	if _, ok := c.ResolveHandle(oldHB); ok {
		t.Fatalf("expected old handle to stop resolving after recompute, oldHB=%+v", oldHB)
	}
	// New handle resolves.
	if got, ok := c.ResolveHandle(newHB); !ok || got != keyB {
		t.Fatalf("expected new handle to resolve to B: ok=%v got=%+v want=%+v", ok, got, keyB)
	}
}

func TestCatalog_EnsureBody_CachesSuccess_SingleFlightConcurrency(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})

	var once sync.Once
	p := &testProvider{
		typ: "t",
		loadBodyFn: func(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
			once.Do(func() { close(started) })
			<-release
			return "B", nil
		},
	}

	c := New(mapResolver{"t": p})
	def := spec.SkillDef{Type: "t", Name: "n", Location: "/p"}

	if _, err := c.Add(t.Context(), def); err != nil {
		t.Fatalf("Add: %v", err)
	}
	key, ok := c.ResolveDef(def)
	if !ok {
		t.Fatalf("ResolveDef failed")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)

	errs := make([]error, n)
	bodies := make([]string, n)

	for i := range n {
		go func() {
			defer wg.Done()
			body, err := c.EnsureBody(ctx, key)
			errs[i] = err
			bodies[i] = body
		}()
	}

	<-started
	close(release)
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("EnsureBody[%d] unexpected err: %v", i, errs[i])
		}
		if bodies[i] != "B" {
			t.Fatalf("EnsureBody[%d] unexpected body: %q", i, bodies[i])
		}
	}
	if got := p.loadCalls.Load(); got != 1 {
		t.Fatalf("expected 1 LoadBody call, got %d", got)
	}

	// Second call should be cached.
	body, err := c.EnsureBody(t.Context(), key)
	if err != nil || body != "B" {
		t.Fatalf("cached EnsureBody unexpected: body=%q err=%v", body, err)
	}
	if got := p.loadCalls.Load(); got != 1 {
		t.Fatalf("expected still 1 LoadBody call after cache, got %d", got)
	}
}

func TestCatalog_EnsureBody_DoesNotCacheContextCancel(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})

	p := &testProvider{
		typ: "t",
		loadBodyFn: func(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			<-ctx.Done()
			close(release)
			return "", ctx.Err()
		},
	}

	c := New(mapResolver{"t": p})
	def := spec.SkillDef{Type: "t", Name: "n", Location: "/p"}

	if _, err := c.Add(t.Context(), def); err != nil {
		t.Fatalf("Add: %v", err)
	}
	key, ok := c.ResolveDef(def)
	if !ok {
		t.Fatalf("ResolveDef failed")
	}

	cctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.EnsureBody(cctx, key)
		errCh <- err
	}()

	<-started
	cancel()
	<-release

	if err := <-errCh; err == nil {
		t.Fatalf("expected cancel error")
	}

	// Next call should try again.
	p.loadBodyFn = func(ctx context.Context, key spec.ProviderSkillKey) (string, error) { return "OK", nil }

	ctx, cancel2 := context.WithTimeout(t.Context(), 200*time.Millisecond)
	t.Cleanup(cancel2)

	body, err := c.EnsureBody(ctx, key)
	if err != nil || body != "OK" {
		t.Fatalf("expected OK after retry, got body=%q err=%v", body, err)
	}
	if got := p.loadCalls.Load(); got < 2 {
		t.Fatalf("expected at least 2 LoadBody calls (cancel not cached), got %d", got)
	}
}

func TestCatalog_EnsureBody_WaitHonorsContext_AndRemoveWakesWaiters(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})

	var once sync.Once
	p := &testProvider{
		typ: "t",
		loadBodyFn: func(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
			once.Do(func() { close(started) })
			<-release
			return "OK", nil
		},
	}

	c := New(mapResolver{"t": p})
	def := spec.SkillDef{Type: "t", Name: "n", Location: "/p"}

	if _, err := c.Add(t.Context(), def); err != nil {
		t.Fatalf("Add: %v", err)
	}
	key, ok := c.ResolveDef(def)
	if !ok {
		t.Fatalf("ResolveDef failed")
	}

	// Start the loader (will block on release).
	go func() { _, _ = c.EnsureBody(t.Context(), key) }()
	<-started

	// A waiter with a short timeout must return promptly.
	waitCtx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	t.Cleanup(cancel)

	_, err := c.EnsureBody(waitCtx, key)
	if err == nil {
		t.Fatalf("expected timeout error for waiter")
	}

	// A waiter with a long timeout should be woken by Remove and return ErrSkillNotFound.
	waiterErrCh := make(chan error, 1)
	go func() {
		_, err := c.EnsureBody(t.Context(), key)
		waiterErrCh <- err
	}()

	// Remove while in-flight; should wake the waiter via closing bodyWait.
	_, _, _ = c.Remove(def)

	select {
	case err := <-waiterErrCh:
		if !errors.Is(err, spec.ErrSkillNotFound) {
			t.Fatalf("expected waiter to return ErrSkillNotFound after Remove, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("waiter did not return; Remove may not have woken waiters")
	}

	close(release)
}

func TestCatalog_EnsureBody_CachesNonContextError(t *testing.T) {
	t.Parallel()

	p := &testProvider{
		typ: "t",
		loadBodyFn: func(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
			return "", errors.New("boom")
		},
	}
	c := New(mapResolver{"t": p})

	def := spec.SkillDef{Type: "t", Name: "n", Location: "/p"}
	if _, err := c.Add(t.Context(), def); err != nil {
		t.Fatalf("Add: %v", err)
	}
	key, ok := c.ResolveDef(def)
	if !ok {
		t.Fatalf("ResolveDef failed")
	}

	_, err := c.EnsureBody(t.Context(), key)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected boom error, got %v", err)
	}
	if got := p.loadCalls.Load(); got != 1 {
		t.Fatalf("expected 1 LoadBody call, got %d", got)
	}

	_, err = c.EnsureBody(t.Context(), key)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected cached boom error, got %v", err)
	}
	if got := p.loadCalls.Load(); got != 1 {
		t.Fatalf("expected still 1 LoadBody call (error cached), got %d", got)
	}
}

func TestCatalog_EnsureBody_UsesPrepopulatedIndexBodyWithoutLoadBody(t *testing.T) {
	t.Parallel()

	p := &testProvider{
		typ: "t",
		indexFn: func(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
			return spec.ProviderSkillIndexRecord{
				Key:         spec.ProviderSkillKey(def),
				Description: "x",
				SkillBody:   "PRELOADED",
			}, nil
		},
	}
	c := New(mapResolver{"t": p})

	def := spec.SkillDef{Type: "t", Name: "n", Location: "/p"}
	if _, err := c.Add(t.Context(), def); err != nil {
		t.Fatalf("Add: %v", err)
	}
	key, ok := c.ResolveDef(def)
	if !ok {
		t.Fatalf("ResolveDef failed")
	}

	body, err := c.EnsureBody(t.Context(), key)
	if err != nil {
		t.Fatalf("EnsureBody: %v", err)
	}
	if body != "PRELOADED" {
		t.Fatalf("unexpected body: %q", body)
	}
	if got := p.loadCalls.Load(); got != 0 {
		t.Fatalf("expected 0 LoadBody calls, got %d", got)
	}
}

func TestCatalog_EnsureBody_UnknownKey_ReturnsSkillNotFound(t *testing.T) {
	t.Parallel()

	c := New(mapResolver{"t": &testProvider{typ: "t"}})

	_, err := c.EnsureBody(t.Context(), spec.ProviderSkillKey{
		Type:     "t",
		Name:     "missing",
		Location: "/p",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, spec.ErrSkillNotFound) {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestCatalog_EnsureBody_ProviderNotFoundIsCached(t *testing.T) {
	t.Parallel()

	res := newSwitchResolver()
	p := &testProvider{typ: "t"}
	res.Set("t", p)

	c := New(res)

	def := spec.SkillDef{Type: "t", Name: "n", Location: "/p"}
	if _, err := c.Add(t.Context(), def); err != nil {
		t.Fatalf("Add: %v", err)
	}
	key, ok := c.ResolveDef(def)
	if !ok {
		t.Fatalf("ResolveDef failed")
	}

	// Remove provider after Add, before EnsureBody.
	res.Set("t", nil)

	_, err := c.EnsureBody(t.Context(), key)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, spec.ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}

	// Restore provider; EnsureBody should still return cached ErrProviderNotFound (no retry).
	res.Set("t", p)

	_, err = c.EnsureBody(t.Context(), key)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, spec.ErrProviderNotFound) {
		t.Fatalf("expected cached ErrProviderNotFound, got %v", err)
	}
	if got := p.loadCalls.Load(); got != 0 {
		t.Fatalf("expected 0 LoadBody calls (provider not found happens before LoadBody), got %d", got)
	}
}
