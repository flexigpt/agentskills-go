package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/flexigpt/llmtools-go"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

func (s *Session) NewRegistry(opts ...llmtools.RegistryOption) (*llmtools.Registry, error) {
	r, err := llmtools.NewRegistry(opts...)
	if err != nil {
		return nil, err
	}

	if err := llmtools.RegisterTypedAsTextTool(r, spec.SkillsLoadTool(), s.toolLoad); err != nil {
		return nil, err
	}
	if err := llmtools.RegisterTypedAsTextTool(r, spec.SkillsUnloadTool(), s.toolUnload); err != nil {
		return nil, err
	}
	if err := llmtools.RegisterOutputsTool(r, spec.SkillsReadTool(), s.toolRead); err != nil {
		return nil, err
	}
	if err := llmtools.RegisterTypedAsTextTool(r, spec.SkillsRunScriptTool(), s.toolRunScript); err != nil {
		return nil, err
	}

	return r, nil
}

func (s *Session) toolLoad(ctx context.Context, args spec.LoadArgs) (spec.LoadResult, error) {
	if err := ctx.Err(); err != nil {
		return spec.LoadResult{}, err
	}
	mode := args.Mode
	if strings.TrimSpace(string(mode)) == "" {
		mode = spec.LoadModeReplace
	}
	if mode != spec.LoadModeReplace && mode != spec.LoadModeAdd {
		return spec.LoadResult{}, errors.New("mode must be 'replace' or 'add'")
	}
	if len(args.Skills) == 0 {
		return spec.LoadResult{}, errors.New("skills is required")
	}

	// Resolve handles -> internal keys (dedupe).
	reqKeys := make([]spec.SkillKey, 0, len(args.Skills))
	seen := map[string]struct{}{}
	for _, h := range args.Skills {
		if strings.TrimSpace(h.Name) == "" || strings.TrimSpace(h.Path) == "" {
			return spec.LoadResult{}, fmt.Errorf("%w: each skill requires name and path", spec.ErrInvalidArgument)
		}
		k, ok := s.catalog.ResolveHandle(h)
		if !ok {
			return spec.LoadResult{}, fmt.Errorf("%w: unknown skill handle: %+v", spec.ErrSkillNotFound, h)
		}
		ks := keyStr(k)
		if _, ok := seen[ks]; ok {
			continue
		}
		seen[ks] = struct{}{}
		reqKeys = append(reqKeys, k)
	}

	handles, err := s.ActivateKeys(ctx, reqKeys, mode)
	if err != nil {
		return spec.LoadResult{}, err
	}
	return spec.LoadResult{ActiveSkills: handles}, nil
}

func (s *Session) toolUnload(ctx context.Context, args spec.UnloadArgs) (spec.UnloadResult, error) {
	if err := ctx.Err(); err != nil {
		return spec.UnloadResult{}, err
	}
	if !args.All && len(args.Skills) == 0 {
		return spec.UnloadResult{}, errors.New("skills is required unless all=true")
	}

	if args.All {
		s.mu.Lock()
		s.activeByKey = map[string]spec.SkillKey{}
		s.activeOrder = nil
		handles, err := s.activeHandlesLocked()
		s.mu.Unlock()
		if err != nil {
			return spec.UnloadResult{}, err
		}
		return spec.UnloadResult{ActiveSkills: handles}, nil
	}

	// Resolve handles to keys.
	rm := map[string]struct{}{}
	for _, h := range args.Skills {
		if strings.TrimSpace(h.Name) == "" || strings.TrimSpace(h.Path) == "" {
			return spec.UnloadResult{}, fmt.Errorf("%w: each skill requires name and path", spec.ErrInvalidArgument)
		}
		k, ok := s.catalog.ResolveHandle(h)
		if !ok {
			return spec.UnloadResult{}, fmt.Errorf("%w: unknown skill handle: %+v", spec.ErrSkillNotFound, h)
		}
		rm[keyStr(k)] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for ks := range rm {
		delete(s.activeByKey, ks)
	}
	// Filter order.
	next := s.activeOrder[:0]
	for _, ks := range s.activeOrder {
		if _, remove := rm[ks]; remove {
			continue
		}
		if _, ok := s.activeByKey[ks]; !ok {
			continue
		}
		next = append(next, ks)
	}
	s.activeOrder = next

	handles, err := s.activeHandlesLocked()
	if err != nil {
		return spec.UnloadResult{}, err
	}
	return spec.UnloadResult{ActiveSkills: handles}, nil
}

func (s *Session) toolRead(ctx context.Context, args spec.ReadArgs) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Skill.Name) == "" || strings.TrimSpace(args.Skill.Path) == "" {
		return nil, fmt.Errorf("%w: skill.name and skill.path are required", spec.ErrInvalidArgument)
	}
	if strings.TrimSpace(args.Path) == "" {
		return nil, fmt.Errorf("%w: path is required", spec.ErrInvalidArgument)
	}

	k, ok := s.catalog.ResolveHandle(args.Skill)
	if !ok {
		return nil, fmt.Errorf("%w: unknown skill handle: %+v", spec.ErrSkillNotFound, args.Skill)
	}

	ks := keyStr(k)
	s.mu.Lock()
	active := s.isActiveLocked(ks)
	s.mu.Unlock()
	if !active {
		return nil, spec.ErrSkillNotActive
	}

	p, ok := s.providers.Provider(k.Type)
	if !ok || p == nil {
		return nil, spec.ErrProviderNotFound
	}

	enc := args.Encoding
	if strings.TrimSpace(string(enc)) == "" {
		enc = spec.ReadEncodingText
	}

	return p.ReadResource(ctx, k, args.Path, enc)
}

func (s *Session) toolRunScript(ctx context.Context, args spec.RunScriptArgs) (spec.RunScriptResult, error) {
	if err := ctx.Err(); err != nil {
		return spec.RunScriptResult{}, err
	}
	if strings.TrimSpace(args.Skill.Name) == "" || strings.TrimSpace(args.Skill.Path) == "" {
		return spec.RunScriptResult{}, fmt.Errorf("%w: skill.name and skill.path are required", spec.ErrInvalidArgument)
	}
	if strings.TrimSpace(args.Path) == "" {
		return spec.RunScriptResult{}, fmt.Errorf("%w: path is required", spec.ErrInvalidArgument)
	}

	k, ok := s.catalog.ResolveHandle(args.Skill)
	if !ok {
		return spec.RunScriptResult{}, fmt.Errorf("%w: unknown skill handle: %+v", spec.ErrSkillNotFound, args.Skill)
	}

	ks := keyStr(k)
	s.mu.Lock()
	active := s.isActiveLocked(ks)
	s.mu.Unlock()
	if !active {
		return spec.RunScriptResult{}, spec.ErrSkillNotActive
	}

	p, ok := s.providers.Provider(k.Type)
	if !ok || p == nil {
		return spec.RunScriptResult{}, spec.ErrProviderNotFound
	}

	return p.RunScript(ctx, k, args.Path, args.Args, args.Env, args.Workdir)
}
