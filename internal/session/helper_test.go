package session

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/flexigpt/agentskills-go/spec"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

type mapResolver map[string]spec.SkillProvider

func (r mapResolver) Provider(skillType string) (spec.SkillProvider, bool) {
	p, ok := r[skillType]
	return p, ok
}

type memCatalog struct {
	mu sync.Mutex

	indexes     map[spec.ProviderSkillKey]spec.ProviderSkillIndexRecord
	bodies      map[spec.ProviderSkillKey]string
	handles     map[spec.ProviderSkillKey]spec.SkillHandle
	handleToKey map[spec.SkillHandle]spec.ProviderSkillKey

	ensureFn func(context.Context, spec.ProviderSkillKey) (string, error)
}

func newMemCatalog() *memCatalog {
	return &memCatalog{
		indexes:     map[spec.ProviderSkillKey]spec.ProviderSkillIndexRecord{},
		bodies:      map[spec.ProviderSkillKey]string{},
		handles:     map[spec.ProviderSkillKey]spec.SkillHandle{},
		handleToKey: map[spec.SkillHandle]spec.ProviderSkillKey{},
	}
}

func (c *memCatalog) ResolveHandle(h spec.SkillHandle) (spec.ProviderSkillKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.handleToKey[h]
	return k, ok
}

func (c *memCatalog) HandleForKey(key spec.ProviderSkillKey) (spec.SkillHandle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.handles[key]
	return h, ok
}

func (c *memCatalog) EnsureBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
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

func (c *memCatalog) GetIndex(key spec.ProviderSkillKey) (spec.ProviderSkillIndexRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.indexes[key]
	return r, ok
}

func (c *memCatalog) add(k spec.ProviderSkillKey, body string) {
	c.addWithHandle(k, spec.SkillHandle{Name: k.Name, Location: k.Location}, body)
}

func (c *memCatalog) addWithHandle(k spec.ProviderSkillKey, h spec.SkillHandle, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.indexes[k] = spec.ProviderSkillIndexRecord{
		Key:         k,
		Description: "d-" + k.Name,
	}
	c.bodies[k] = body

	c.handles[k] = h
	c.handleToKey[h] = k
}

type canonProvider struct {
	typ string
	// If def.Location == "rel", normalize to "abs".
}

func (p *canonProvider) Type() string { return p.typ }

func (p *canonProvider) Index(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.ProviderSkillIndexRecord{}, err
	}

	key := spec.ProviderSkillKey(def)
	if key.Location == "rel" {
		key.Location = "abs"
	}

	if strings.TrimSpace(key.Type) == "" || strings.TrimSpace(key.Name) == "" || strings.TrimSpace(key.Location) == "" {
		return spec.ProviderSkillIndexRecord{}, fmt.Errorf("%w: invalid", spec.ErrInvalidArgument)
	}

	return spec.ProviderSkillIndexRecord{Key: key, Description: "d"}, nil
}

func (p *canonProvider) LoadBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "body", nil
}

func (p *canonProvider) ReadResource(
	ctx context.Context,
	key spec.ProviderSkillKey,
	resourcePath string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, spec.ErrInvalidArgument
}

func (p *canonProvider) RunScript(
	ctx context.Context,
	key spec.ProviderSkillKey,
	scriptPath string,
	args []string,
	env map[string]string,
	workdir string,
) (spec.RunScriptOut, error) {
	return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
}
