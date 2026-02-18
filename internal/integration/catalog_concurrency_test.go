package integration

import (
	"context"
	"errors"
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

func (p *blockingProvider) Index(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
	return spec.ProviderSkillIndexRecord{
		Key:         spec.ProviderSkillKey(def),
		Description: "desc",
	}, nil
}

func (p *blockingProvider) LoadBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	// Signal that the load has started and then block until released.
	close(p.loadStarted)

	select {
	case <-p.release:
		return "body", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (p *blockingProvider) ReadResource(
	ctx context.Context,
	key spec.ProviderSkillKey,
	resourceLocation string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, spec.ErrInvalidArgument
}

func (p *blockingProvider) RunScript(
	ctx context.Context,
	key spec.ProviderSkillKey,
	scriptLocation string,
	args []string,
	env map[string]string,
	workDir string,
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

	def := spec.SkillDef{
		Type:     "fake",
		Name:     "s",
		Location: "/x",
	}

	if _, err := cat.Add(t.Context(), def); err != nil {
		t.Fatalf("add: %v", err)
	}
	psKey, isPresent := cat.ResolveDef(def)
	if !isPresent {
		t.Fatalf("could not find added definition")
	}

	// Start a loader that will block inside provider.LoadBody.
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = cat.EnsureBody(t.Context(), psKey)
	})

	<-p.loadStarted // ensure in-flight load exists

	// Start a waiter that cancels quickly: must not block forever waiting for the in-flight load.
	waitCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, werr := cat.EnsureBody(waitCtx, psKey)
	if werr == nil {
		t.Fatalf("expected context timeout/cancel error for waiter")
	}
	if !errors.Is(werr, context.DeadlineExceeded) && !errors.Is(werr, context.Canceled) {
		t.Fatalf("expected deadline/cancel error, got: %v", werr)
	}

	// Remove while load in-flight; should not panic (e.g. double-close) and should wake waiters.
	_, _, _ = cat.Remove(def)

	// Release provider to let loader finish.
	close(p.release)

	wg.Wait()
}
