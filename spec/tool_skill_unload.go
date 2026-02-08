package spec

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsUnload llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/spec/tools.skills.unload"

func SkillsUnloadTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2402",
		Slug:          "skills.unload",
		Version:       "v1.0.0",
		DisplayName:   "Skills Unload",
		Description:   "Unload skills from the current session. Uses (name + path) handles; session-bound (no sessionID arg).",
		Tags:          []string{"skills"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
  "$schema":"http://json-schema.org/draft-07/schema#",
  "type":"object",
  "definitions":{
    "skill_handle":{
      "type":"object",
      "properties":{
        "name":{"type":"string"},
        "path":{"type":"string"}
      },
      "required":["name","path"],
      "additionalProperties":false
    }
  },
  "properties":{
    "skills":{"type":"array","items":{"$ref":"#/definitions/skill_handle"}},
    "all":{"type":"boolean","default":false}
  },
  "additionalProperties":false
}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsUnload},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
