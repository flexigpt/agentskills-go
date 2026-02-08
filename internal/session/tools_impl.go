package session

import (
	"context"
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
	s.touchSession()
	if s.isClosed() {
		return spec.LoadResult{}, spec.ErrSessionNotFound
	}
	mode := args.Mode
	if strings.TrimSpace(string(mode)) == "" {
		mode = spec.LoadModeReplace
	}
	if mode != spec.LoadModeReplace && mode != spec.LoadModeAdd {
		return spec.LoadResult{}, fmt.Errorf("%w: mode must be 'replace' or 'add'", spec.ErrInvalidArgument)
	}
	if len(args.Skills) == 0 {
		return spec.LoadResult{}, fmt.Errorf("%w: skills is required", spec.ErrInvalidArgument)
	}

	// Resolve handles -> internal keys (dedupe).
	reqKeys := make([]spec.SkillKey, 0, len(args.Skills))
	seen := map[spec.SkillKey]struct{}{}

	for _, h := range args.Skills {
		if strings.TrimSpace(h.Name) == "" || strings.TrimSpace(h.Path) == "" {
			return spec.LoadResult{}, fmt.Errorf("%w: each skill requires name and path", spec.ErrInvalidArgument)
		}
		k, ok := s.catalog.ResolveHandle(h)
		if !ok {
			return spec.LoadResult{}, fmt.Errorf("%w: unknown skill handle: %+v", spec.ErrSkillNotFound, h)
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}

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
	s.touchSession()
	if s.isClosed() {
		return spec.UnloadResult{}, spec.ErrSessionNotFound
	}
	if !args.All && len(args.Skills) == 0 {
		return spec.UnloadResult{}, fmt.Errorf("%w: skills is required unless all=true", spec.ErrInvalidArgument)
	}

	if args.All {
		s.mu.Lock()
		s.activeSet = map[spec.SkillKey]struct{}{}

		s.activeOrder = nil
		s.stateVersion++

		handles, err := s.activeHandlesLocked()
		s.mu.Unlock()
		if err != nil {
			return spec.UnloadResult{}, err
		}
		return spec.UnloadResult{ActiveSkills: handles}, nil
	}

	// Resolve handles to keys.
	rm := map[spec.SkillKey]struct{}{}

	for _, h := range args.Skills {
		if strings.TrimSpace(h.Name) == "" || strings.TrimSpace(h.Path) == "" {
			return spec.UnloadResult{}, fmt.Errorf("%w: each skill requires name and path", spec.ErrInvalidArgument)
		}
		k, ok := s.catalog.ResolveHandle(h)
		if !ok {
			return spec.UnloadResult{}, fmt.Errorf("%w: unknown skill handle: %+v", spec.ErrSkillNotFound, h)
		}
		rm[k] = struct{}{}

	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for k := range rm {
		delete(s.activeSet, k)
	}
	// Filter order.
	next := s.activeOrder[:0]
	for _, k := range s.activeOrder {
		if _, remove := rm[k]; remove {
			continue
		}
		if _, ok := s.activeSet[k]; !ok {
			continue
		}
		next = append(next, k)

	}
	s.activeOrder = next
	s.stateVersion++

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
	s.touchSession()
	if s.isClosed() {
		return nil, spec.ErrSessionNotFound
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

	s.mu.Lock()
	active := s.isActiveLocked(k)
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
	s.touchSession()
	if s.isClosed() {
		return spec.RunScriptResult{}, spec.ErrSessionNotFound
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

	s.mu.Lock()
	active := s.isActiveLocked(k)

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
