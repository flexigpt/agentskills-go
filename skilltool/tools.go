package skilltool

import (
	"context"
	"errors"

	"github.com/flexigpt/agentskills-go/spec"
	"github.com/flexigpt/llmtools-go"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

// Register registers the skills runtime tools into an existing llmtools-go Registry.
// Session binding is done by closure via sessionID.
func Register(r *llmtools.Registry, rt spec.Runtime, sessionID spec.SessionID) error {
	if r == nil {
		return errors.New("nil registry")
	}
	if rt == nil {
		return errors.New("nil runtime")
	}

	if err := llmtools.RegisterTypedAsTextTool(
		r,
		SkillsLoadTool(),
		func(ctx context.Context, args spec.LoadArgs) (spec.LoadResult, error) {
			return rt.Load(ctx, sessionID, args)
		},
	); err != nil {
		return err
	}

	if err := llmtools.RegisterTypedAsTextTool(
		r,
		SkillsUnloadTool(),
		func(ctx context.Context, args spec.UnloadArgs) (spec.UnloadResult, error) {
			return rt.Unload(ctx, sessionID, args)
		},
	); err != nil {
		return err
	}

	if err := llmtools.RegisterOutputsTool(
		r,
		SkillsReadTool(),
		func(ctx context.Context, args spec.ReadArgs) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
			return rt.Read(ctx, sessionID, args)
		},
	); err != nil {
		return err
	}

	if err := llmtools.RegisterTypedAsTextTool(
		r,
		SkillsRunScriptTool(),
		func(ctx context.Context, args spec.RunScriptArgs) (spec.RunScriptResult, error) {
			return rt.RunScript(ctx, sessionID, args)
		},
	); err != nil {
		return err
	}

	return nil
}

func Tools() []llmtoolsgoSpec.Tool {
	return []llmtoolsgoSpec.Tool{
		SkillsLoadTool(),
		SkillsUnloadTool(),
		SkillsReadTool(),
		SkillsRunScriptTool(),
	}
}
