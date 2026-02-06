package skilltool

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const (
	FuncIDSkillsLoad llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool/load.Load"
)

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
