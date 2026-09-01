package session

import (
	"context"
	"fmt"
	"strings"
	"sync"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/provider"
	"github.com/flexigpt/agentskills-go/runtime/spec"
)

const (
	relStr           = "rel"
	absStr           = "abs"
	nopeStr          = "nope"
	unknownHandleStr = "unknown_handle"
)

type mapResolver map[string]provider.SkillProvider

func (r mapResolver) Provider(skillType string) (provider.SkillProvider, bool) {
	p, ok := r[skillType]
	return p, ok
}

type memCatalog struct {
	mu sync.Mutex

	indexes     map[provider.ProviderSkillKey]provider.ProviderSkillIndexRecord
	bodies      map[provider.ProviderSkillKey]string
	handles     map[provider.ProviderSkillKey]spec.SkillHandle
	handleToKey map[spec.SkillHandle]provider.ProviderSkillKey

	ensureFn func(context.Context, provider.ProviderSkillKey) (string, error)
}

func newMemCatalog() *memCatalog {
	return &memCatalog{
		indexes:     map[provider.ProviderSkillKey]provider.ProviderSkillIndexRecord{},
		bodies:      map[provider.ProviderSkillKey]string{},
		handles:     map[provider.ProviderSkillKey]spec.SkillHandle{},
		handleToKey: map[spec.SkillHandle]provider.ProviderSkillKey{},
	}
}

func (c *memCatalog) ResolveHandle(h spec.SkillHandle) (provider.ProviderSkillKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.handleToKey[h]
	return k, ok
}

func (c *memCatalog) HandleForKey(key provider.ProviderSkillKey) (spec.SkillHandle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.handles[key]
	return h, ok
}

func (c *memCatalog) EnsureBody(ctx context.Context, key provider.ProviderSkillKey) (string, error) {
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

func (c *memCatalog) GetIndex(key provider.ProviderSkillKey) (provider.ProviderSkillIndexRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.indexes[key]
	return r, ok
}

func (c *memCatalog) add(k provider.ProviderSkillKey, body string) {
	c.addWithHandle(k, spec.SkillHandle{Name: k.Name, Location: k.Location}, body)
}

func (c *memCatalog) addWithHandle(k provider.ProviderSkillKey, h spec.SkillHandle, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.indexes[k] = provider.ProviderSkillIndexRecord{
		Key:         k,
		Description: "d-" + k.Name,
	}
	c.bodies[k] = body

	c.handles[k] = h
	c.handleToKey[h] = k
}

type canonProvider struct {
	typ string
	// If def.Location == relStr, normalize to absStr.
}

func (p *canonProvider) Type() string { return p.typ }

func (p *canonProvider) Index(ctx context.Context, def provider.SkillDef) (provider.ProviderSkillIndexRecord, error) {
	if err := ctx.Err(); err != nil {
		return provider.ProviderSkillIndexRecord{}, err
	}

	key := provider.ProviderSkillKey(def)
	if key.Location == relStr {
		key.Location = absStr
	}

	if strings.TrimSpace(key.Type) == "" || strings.TrimSpace(key.Name) == "" || strings.TrimSpace(key.Location) == "" {
		return provider.ProviderSkillIndexRecord{}, fmt.Errorf("%w: invalid", spec.ErrInvalidRuntimeArgument)
	}

	return provider.ProviderSkillIndexRecord{Key: key, Description: "d"}, nil
}

func (p *canonProvider) SupportsRunScript() bool {
	return false
}

func (p *canonProvider) LoadBody(ctx context.Context, key provider.ProviderSkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "body", nil
}

func (p *canonProvider) ReadResource(
	ctx context.Context,
	key provider.ProviderSkillKey,
	resourcePath string,
	encoding provider.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, provider.ErrInvalidProviderArgument
}

func (p *canonProvider) RunScript(
	ctx context.Context,
	key provider.ProviderSkillKey,
	scriptPath string,
	args []string,
	env map[string]string,
	workdir string,
) (provider.RunScriptOut, error) {
	return provider.RunScriptOut{}, provider.ErrRunScriptUnsupported
}
