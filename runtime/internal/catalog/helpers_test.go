package catalog

import (
	"context"
	"sync"
	"sync/atomic"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/provider"
	"github.com/flexigpt/agentskills-go/runtime/spec"
)

type mapResolver map[string]provider.SkillProvider

func (r mapResolver) Provider(skillType string) (provider.SkillProvider, bool) {
	p, ok := r[skillType]
	return p, ok
}

type switchResolver struct {
	mu sync.RWMutex
	m  map[string]provider.SkillProvider
}

func newSwitchResolver() *switchResolver {
	return &switchResolver{m: map[string]provider.SkillProvider{}}
}

func (r *switchResolver) Provider(skillType string) (provider.SkillProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[skillType]
	return p, ok
}

func (r *switchResolver) Set(skillType string, p provider.SkillProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p == nil {
		delete(r.m, skillType)
		return
	}
	r.m[skillType] = p
}

type testProvider struct {
	typ string

	indexFn    func(context.Context, provider.SkillDef) (provider.ProviderSkillIndexRecord, error)
	loadBodyFn func(context.Context, provider.ProviderSkillKey) (string, error)

	loadCalls atomic.Int32
}

func (p *testProvider) Type() string { return p.typ }

func (p *testProvider) Index(ctx context.Context, def provider.SkillDef) (provider.ProviderSkillIndexRecord, error) {
	if p.indexFn != nil {
		return p.indexFn(ctx, def)
	}
	return provider.ProviderSkillIndexRecord{
		Key:            provider.ProviderSkillKey(def),
		Description:    "desc-" + def.Name,
		RawFrontmatter: map[string]any{"p": def.Name},
		Digest:         "digest-" + def.Name,
	}, nil
}

func (p *testProvider) SupportsRunScript() bool {
	return true
}

func (p *testProvider) LoadBody(ctx context.Context, key provider.ProviderSkillKey) (string, error) {
	p.loadCalls.Add(1)
	if p.loadBodyFn != nil {
		return p.loadBodyFn(ctx, key)
	}
	return "BODY:" + key.Name, nil
}

func (p *testProvider) ReadResource(
	ctx context.Context,
	key provider.ProviderSkillKey,
	resourceLocation string,
	encoding provider.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, spec.ErrInvalidRuntimeArgument
}

func (p *testProvider) RunScript(
	ctx context.Context,
	key provider.ProviderSkillKey,
	scriptLocation string,
	args []string,
	env map[string]string,
	workDir string,
) (provider.RunScriptOut, error) {
	return provider.RunScriptOut{}, provider.ErrRunScriptUnsupported
}
