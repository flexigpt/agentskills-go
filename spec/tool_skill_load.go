package spec

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsLoad llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/spec/tools.skills.load"

func SkillsLoadTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2401",
		Slug:          "skills.load",
		Version:       "v1.0.0",
		DisplayName:   "Skills Load",
		Description:   "Load one or more skills into the current session (progressive disclosure). Uses (name + path) handles; session-bound (no sessionID arg).",
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
    "skills":{"type":"array","items":{"$ref":"#/definitions/skill_handle"},"minItems":1},
    "mode":{"type":"string","enum":["replace","add"],"default":"replace"}
  },
  "required":["skills"],
  "additionalProperties":false
}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsLoad},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
