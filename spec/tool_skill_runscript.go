package spec

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsRunScript llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/spec/tools.skills.runscript"

func SkillsRunScriptTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019c4188-ac5a-7e5c-8e68-b22e897c4885",
		Slug:          "skills.runscript",
		Version:       "v1.0.0",
		DisplayName:   "Skills Run Script",
		Description:   "Execute a script from within the selected active skill. Skill is required; session-bound (no sessionID arg).",
		Tags:          []string{"skills", "shell", "exec"},
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
    "path":{"type":"string","description":"Relative script path; must be under skill root (optionally scripts/)."},
    "args":{"type":"array","items":{"type":"string"}},
    "env":{"type":"object","additionalProperties":{"type":"string"}},
    "workdir":{"type":"string","description":"Relative workdir under skill base. Default provider-specific (fs: skill base)."}
  },
  "required":["skill","path"],
  "additionalProperties":false
}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsRunScript},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
