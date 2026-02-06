package skilltool

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsRead llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool/read.Read"

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
