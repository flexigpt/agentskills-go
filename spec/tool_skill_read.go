package spec

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsRead llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/spec/tools.skills.read"

func SkillsReadTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2403",
		Slug:          "skills.read",
		Version:       "v1.0.0",
		DisplayName:   "Skills Read",
		Description:   "Read a skill-scoped resource relative to the skill base path. Skill is required; session-bound (no sessionID arg).",
		Tags:          []string{"skills", "read"},
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
    "skill":{"$ref":"#/definitions/skill_handle"},
    "path":{"type":"string"},
    "encoding":{"type":"string","enum":["text","binary"],"default":"text"}
  },
  "required":["skill","path"],
  "additionalProperties":false
}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsRead},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
