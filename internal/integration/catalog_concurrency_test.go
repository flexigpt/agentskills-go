package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/flexigpt/agentskills-go/internal/catalog"
	"github.com/flexigpt/agentskills-go/spec"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

type fakeResolver struct{ p spec.SkillProvider }

func (r fakeResolver) Provider(skillType string) (spec.SkillProvider, bool) {
	if r.p == nil || r.p.Type() != skillType {
		return nil, false
	}
	return r.p, true
}

type blockingProvider struct {
	loadStarted chan struct{}
	release     chan struct{}
}

func (p *blockingProvider) Type() string { return "fake" }

func (p *blockingProvider) Index(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	return spec.SkillRecord{
		Key:         key,
		Description: "desc",
	}, nil
}

func (p *blockingProvider) LoadBody(ctx context.Context, key spec.SkillKey) (string, error) {
	close(p.loadStarted)
	<-p.release
	return "body", nil
}

func (p *blockingProvider) ReadResource(
	ctx context.Context,
	key spec.SkillKey,
	resourcePath string,
	encoding spec.ReadEncoding,
) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	return nil, spec.ErrInvalidArgument
}

func (p *blockingProvider) RunScript(
	ctx context.Context,
	key spec.SkillKey,
	scriptPath string,
	args []string,
	env map[string]string,
	workdir string,
) (spec.RunScriptOut, error) {
	return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
}

func TestCatalog_EnsureBody_WaitHonorsContext_AndRemoveDoesNotPanic(t *testing.T) {
	t.Parallel()

	p := &blockingProvider{
		loadStarted: make(chan struct{}),
		release:     make(chan struct{}),
	}
	cat := catalog.New(fakeResolver{p: p})

	key := spec.SkillKey{Type: "fake", Name: "s", Path: "/x"}

	_, err := cat.Add(t.Context(), key)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Start loader.
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = cat.EnsureBody(t.Context(), key)
	})

	<-p.loadStarted // ensure in-flight load exists

	// Start a waiter that cancels quickly: must not block on <-ch forever.
	waitCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, werr := cat.EnsureBody(waitCtx, key)
	if werr == nil {
		t.Fatalf("expected context timeout/cancel error for waiter")
	}

	// Remove while load in-flight; should not panic (double-close) and should wake waiters.
	_, _ = cat.Remove(key)

	// Release provider to let loader finish.
	close(p.release)

	wg.Wait()
}
