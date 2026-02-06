package skilltool

import (
	"context"
	"errors"

	"github.com/flexigpt/agentskills-go/spec"
	"github.com/flexigpt/llmtools-go"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

const (
	FuncIDSkillsLoad      llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool.Load"
	FuncIDSkillsUnload    llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool.Unload"
	FuncIDSkillsRead      llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool.Read"
	FuncIDSkillsRunScript llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool.RunScript"
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

	// "skills.load" -> typed -> text output (JSON).
	if err := llmtools.RegisterTypedAsTextTool[spec.LoadArgs, spec.LoadResult](
		r,
		SkillsLoadTool(),
		func(ctx context.Context, args spec.LoadArgs) (spec.LoadResult, error) {
			return rt.Load(ctx, sessionID, args)
		},
	); err != nil {
		return err
	}

	// "skills.unload" -> typed -> text output (JSON).
	if err := llmtools.RegisterTypedAsTextTool[spec.UnloadArgs, spec.UnloadResult](
		r,
		SkillsUnloadTool(),
		func(ctx context.Context, args spec.UnloadArgs) (spec.UnloadResult, error) {
			return rt.Unload(ctx, sessionID, args)
		},
	); err != nil {
		return err
	}

	// "skills.read" -> typed -> outputs.
	if err := llmtools.RegisterOutputsTool[spec.ReadArgs](
		r,
		SkillsReadTool(),
		func(ctx context.Context, args spec.ReadArgs) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
			return rt.Read(ctx, sessionID, args)
		},
	); err != nil {
		return err
	}

	// "skills.run_script" -> typed -> text output (JSON).
	if err := llmtools.RegisterTypedAsTextTool[spec.RunScriptArgs, spec.RunScriptResult](
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

func SkillsLoadTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2401",
		Slug:          "skills.load",
		Version:       "v1.0.0",
		DisplayName:   "Skills Load",
		Description:   "Load one or more skills into the current session (progressive disclosure).",
		Tags:          []string{"skills"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
		  "$schema":"http://json-schema.org/draft-07/schema#",
		  "type":"object",
		  "properties":{
		    "names":{"type":"array","items":{"type":"string"}},
		    "mode":{"type":"string","enum":["replace","add"],"default":"replace"}
		  },
		  "required":["names"],
		  "additionalProperties":false
		}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsLoad},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}

func SkillsUnloadTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2402",
		Slug:          "skills.unload",
		Version:       "v1.0.0",
		DisplayName:   "Skills Unload",
		Description:   "Unload skills from the current session.",
		Tags:          []string{"skills"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
		  "$schema":"http://json-schema.org/draft-07/schema#",
		  "type":"object",
		  "properties":{
		    "names":{"type":"array","items":{"type":"string"}},
		    "all":{"type":"boolean","default":false}
		  },
		  "additionalProperties":false
		}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsUnload},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}

func SkillsReadTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2403",
		Slug:          "skills.read",
		Version:       "v1.0.0",
		DisplayName:   "Skills Read",
		Description:   "Read a skill-scoped file relative to the active skill root.",
		Tags:          []string{"skills", "fs", "read"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
		  "$schema":"http://json-schema.org/draft-07/schema#",
		  "type":"object",
		  "properties":{
		    "skill":{"type":"string","description":"Optional skill name. If omitted, uses most recently loaded skill."},
		    "path":{"type":"string"},
		    "encoding":{"type":"string","enum":["text","binary"],"default":"text"}
		  },
		  "required":["path"],
		  "additionalProperties":false
		}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsRead},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}

func SkillsRunScriptTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2404",
		Slug:          "skills.run_script",
		Version:       "v1.0.0",
		DisplayName:   "Skills Run Script",
		Description:   "Execute a script from the active skill's scripts/ directory.",
		Tags:          []string{"skills", "shell", "exec"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
		  "$schema":"http://json-schema.org/draft-07/schema#",
		  "type":"object",
		  "properties":{
		    "skill":{"type":"string","description":"Optional skill name. If omitted, uses most recently loaded skill."},
		    "path":{"type":"string","description":"Relative path under skill root; must be under scripts/"},
		    "args":{"type":"array","items":{"type":"string"}},
		    "env":{"type":"object","additionalProperties":{"type":"string"}},
		    "workdir":{"type":"string","description":"Relative workdir under skill root. Default '.'"}
		  },
		  "required":["path"],
		  "additionalProperties":false
		}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsRunScript},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
