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
) (spec.RunScriptOut, error) {
	return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
}
