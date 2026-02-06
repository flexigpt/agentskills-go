package skilltool

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsUnload llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool/unload.Unload"

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
