package catalog

import (
	"context"
	"sync"
	"sync/atomic"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

type mapResolver map[string]spec.SkillProvider

func (r mapResolver) Provider(skillType string) (spec.SkillProvider, bool) {
	p, ok := r[skillType]
	return p, ok
}

type switchResolver struct {
	mu sync.RWMutex
	m  map[string]spec.SkillProvider
}

func newSwitchResolver() *switchResolver {
	return &switchResolver{m: map[string]spec.SkillProvider{}}
}

func (r *switchResolver) Provider(skillType string) (spec.SkillProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[skillType]
	return p, ok
}

func (r *switchResolver) Set(skillType string, p spec.SkillProvider) {
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

	indexFn    func(context.Context, spec.SkillDef) (spec.ProviderSkillIndexRecord, error)
	loadBodyFn func(context.Context, spec.ProviderSkillKey) (string, error)

	loadCalls atomic.Int32
}

func (p *testProvider) Type() string { return p.typ }

func (p *testProvider) Index(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
	if p.indexFn != nil {
		return p.indexFn(ctx, def)
	}
	return spec.ProviderSkillIndexRecord{
		Key:         spec.ProviderSkillKey(def),
		Description: "desc-" + def.Name,
		Properties:  map[string]any{"p": def.Name},
		Digest:      "digest-" + def.Name,
	}, nil
}

func (p *testProvider) LoadBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	p.loadCalls.Add(1)
	if p.loadBodyFn != nil {
		return p.loadBodyFn(ctx, key)
	}
	return "BODY:" + key.Name, nil
}

func (p *testProvider) ReadResource(
	ctx context.Context,
	key spec.ProviderSkillKey,
	resourceLocation string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, spec.ErrInvalidArgument
}

func (p *testProvider) RunScript(
	ctx context.Context,
	key spec.ProviderSkillKey,
	scriptLocation string,
	args []string,
	env map[string]string,
	workDir string,
) (spec.RunScriptOut, error) {
	return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
}
