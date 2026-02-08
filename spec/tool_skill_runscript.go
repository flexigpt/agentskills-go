package spec

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsRunScript llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/spec/tools.skills.run_script"

func SkillsRunScriptTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2404",
		Slug:          "skills.run_script",
		Version:       "v1.0.0",
		DisplayName:   "Skills Run Script",
		Description:   "Execute a script from within the selected active skill. Skill is required; provider enforces script constraints; session-bound (no sessionID arg).",
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
    "path":{"type":"string","description":"Relative script path; provider enforces constraints (e.g. must be under scripts/)."},
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
