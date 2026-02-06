package spec

import (
	"context"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

// Runtime is the interface that tools bind to.
// Implementations (like package agentskills Runtime) own session state.
type Runtime interface {
	Load(ctx context.Context, sessionID SessionID, args LoadArgs) (LoadResult, error)
	Unload(ctx context.Context, sessionID SessionID, args UnloadArgs) (UnloadResult, error)
	Read(ctx context.Context, sessionID SessionID, args ReadArgs) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error)
	RunScript(ctx context.Context, sessionID SessionID, args RunScriptArgs) (RunScriptResult, error)
}
