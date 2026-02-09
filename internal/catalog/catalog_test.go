package catalog

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

type mapResolver map[string]spec.SkillProvider

func (r mapResolver) Provider(skillType string) (spec.SkillProvider, bool) {
	p, ok := r[skillType]
	return p, ok
}

type provider struct {
	typ string

	indexFn    func(context.Context, spec.SkillKey) (spec.SkillRecord, error)
	loadBodyFn func(context.Context, spec.SkillKey) (string, error)

	loadCalls atomic.Int32
}

func (p *provider) Type() string { return p.typ }

func (p *provider) Index(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	if p.indexFn != nil {
		return p.indexFn(ctx, key)
	}
	return spec.SkillRecord{Key: key, Description: "desc-" + key.Name}, nil
}

func (p *provider) LoadBody(ctx context.Context, key spec.SkillKey) (string, error) {
	p.loadCalls.Add(1)
	if p.loadBodyFn != nil {
		return p.loadBodyFn(ctx, key)
	}
	return "BODY:" + key.Name, nil
}

func (p *provider) ReadResource(
	ctx context.Context,
	key spec.SkillKey,
	resourcePath string,
	encoding spec.ReadEncoding,
) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	return nil, spec.ErrInvalidArgument
}

func (p *provider) RunScript(
	ctx context.Context,
	key spec.SkillKey,
	scriptPath string,
	args []string,
	env map[string]string,
	workdir string,
) (spec.RunScriptOut, error) {
	return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
}

func TestCatalog_AddValidationAndProviderErrors(t *testing.T) {
	t.Parallel()

	p := &provider{typ: "t"}

	tests := []struct {
		name      string
		key       spec.SkillKey
		resolver  ProviderResolver
		wantIsErr error
		wantSub   string
	}{
		{
			name:      "missing type",
			key:       spec.SkillKey{Type: "", Name: "n", Path: "p"},
			resolver:  mapResolver{"t": p},
			wantIsErr: spec.ErrInvalidArgument,
		},
		{
			name:      "missing name",
			key:       spec.SkillKey{Type: "t", Name: "", Path: "p"},
			resolver:  mapResolver{"t": p},
			wantIsErr: spec.ErrInvalidArgument,
		},
		{
			name:      "missing path",
			key:       spec.SkillKey{Type: "t", Name: "n", Path: ""},
			resolver:  mapResolver{"t": p},
			wantIsErr: spec.ErrInvalidArgument,
		},
		{
			name:      "provider not found",
			key:       spec.SkillKey{Type: "missing", Name: "n", Path: "p"},
			resolver:  mapResolver{"t": p},
			wantIsErr: spec.ErrProviderNotFound,
			wantSub:   "unknown provider type",
		},
		{
			name: "provider changes type",
			key:  spec.SkillKey{Type: "t", Name: "n", Path: "p"},
			resolver: mapResolver{"t": &provider{
				typ: "t",
				indexFn: func(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
					key.Type = "other"
					return spec.SkillRecord{Key: key, Description: "x"}, nil
				},
			}},
			wantIsErr: spec.ErrInvalidArgument,
			wantSub:   "provider changed key.type",
		},
		{
			name: "provider changes name",
			key:  spec.SkillKey{Type: "t", Name: "n", Path: "p"},
			resolver: mapResolver{"t": &provider{
				typ: "t",
				indexFn: func(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
					key.Name = "other"
					return spec.SkillRecord{Key: key, Description: "x"}, nil
				},
			}},
			wantIsErr: spec.ErrInvalidArgument,
			wantSub:   "provider changed key.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cat := New(tt.resolver)
			_, err := cat.Add(t.Context(), tt.key)
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

func TestCatalog_HandleDisambiguationOnCollision(t *testing.T) {
	t.Parallel()

	pa := &provider{typ: "a"}
	pb := &provider{typ: "b"}
	c := New(mapResolver{"a": pa, "b": pb})

	// Same name+path, different type => collision group => LLM names become "type:name".
	ka := spec.SkillKey{Type: "a", Name: "same", Path: "/p"}
	kb := spec.SkillKey{Type: "b", Name: "same", Path: "/p"}

	_, err := c.Add(t.Context(), ka)
	if err != nil {
		t.Fatalf("Add a: %v", err)
	}
	_, err = c.Add(t.Context(), kb)
	if err != nil {
		t.Fatalf("Add b: %v", err)
	}

	ha, ok := c.HandleForKey(ka)
	if !ok {
		t.Fatalf("HandleForKey a not found")
	}
	hb, ok := c.HandleForKey(kb)
	if !ok {
		t.Fatalf("HandleForKey b not found")
	}

	if ha.Name == hb.Name {
		t.Fatalf("expected disambiguated names, got ha=%+v hb=%+v", ha, hb)
	}
	if ha.Name != "a:same" || hb.Name != "b:same" {
		t.Fatalf("unexpected LLM names: ha=%+v hb=%+v", ha, hb)
	}

	rka, ok := c.ResolveHandle(ha)
	if !ok || rka != ka {
		t.Fatalf("ResolveHandle(ha) failed: ok=%v got=%+v want=%+v", ok, rka, ka)
	}
	rkb, ok := c.ResolveHandle(hb)
	if !ok || rkb != kb {
		t.Fatalf("ResolveHandle(hb) failed: ok=%v got=%+v want=%+v", ok, rkb, kb)
	}
}

func TestCatalog_EnsureBody_CachesSuccess_SingleFlightConcurrency(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})

	var once sync.Once
	p := &provider{
		typ: "t",
		loadBodyFn: func(ctx context.Context, key spec.SkillKey) (string, error) {
			once.Do(func() { close(started) })
			<-release
			return "B", nil
		},
	}
	c := New(mapResolver{"t": p})

	key := spec.SkillKey{Type: "t", Name: "n", Path: "p"}
	if _, err := c.Add(t.Context(), key); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Start many concurrent waiters; only one provider.LoadBody should run.
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

	p := &provider{
		typ: "t",
		loadBodyFn: func(ctx context.Context, key spec.SkillKey) (string, error) {
			// Signal that LoadBody was actually entered.
			select {
			case <-started:
				// Already signaled.
			default:
				close(started)
			}
			// Block until test cancels.
			<-ctx.Done()
			close(release)
			return "", ctx.Err()
		},
	}
	c := New(mapResolver{"t": p})

	key := spec.SkillKey{Type: "t", Name: "n", Path: "p"}
	if _, err := c.Add(t.Context(), key); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Cancel *during* an in-flight LoadBody so finishBodyLoad sees context cancellation
	// and (correctly) does NOT cache it.
	cctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.EnsureBody(cctx, key)
		errCh <- err
	}()

	<-started
	cancel()

	// Ensure the first call completes before we mutate provider behavior (avoid races).
	<-release
	if err := <-errCh; err == nil {
		t.Fatalf("expected cancel error")
	}

	// Next call should try again (LoadBody call count increments).
	ctx, cancel2 := context.WithTimeout(t.Context(), 200*time.Millisecond)
	t.Cleanup(cancel2)

	// Make LoadBody return quickly now by swapping provider function.
	p.loadBodyFn = func(ctx context.Context, key spec.SkillKey) (string, error) { return "OK", nil }

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
	p := &provider{
		typ: "t",
		loadBodyFn: func(ctx context.Context, key spec.SkillKey) (string, error) {
			once.Do(func() { close(started) })
			<-release
			return "OK", nil
		},
	}
	c := New(mapResolver{"t": p})

	key := spec.SkillKey{Type: "t", Name: "n", Path: "p"}
	if _, err := c.Add(t.Context(), key); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Start loader.
	go func() { _, _ = c.EnsureBody(t.Context(), key) }()
	<-started

	// Waiter with timeout must return, not deadlock.
	waitCtx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	t.Cleanup(cancel)

	_, err := c.EnsureBody(waitCtx, key)
	if err == nil {
		t.Fatalf("expected timeout error for waiter")
	}

	// Remove while in-flight; should not panic and should wake waiters.
	_, _ = c.Remove(key)

	close(release)
	// If Remove didn't wake, EnsureBody could deadlock; race test covers panic.
}
