package spec

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsLoad llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skills.load"

func SkillsLoadTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019c4188-4db4-7ad8-bdd0-4126aa1fee00",
		Slug:          "skills.load",
		Version:       "v1.0.0",
		DisplayName:   "Skills Load",
		Description:   "load one or more skills into the current session",
		Tags:          []string{"skills"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
"$schema":"http://json-schema.org/draft-07/schema#",
"type":"object",
"properties":{
	"skills":{
		"type":"array",
		"minItems":1,
		"items":{
			"type":"object",
			"properties":{
				"name":{"type":"string","description":"skill name"},
				"location":{"type":"string","description":"provider-interpreted base location for the skill"}
			},
			"required":["name","location"],
			"additionalProperties":false
		}
	},
	"mode":{
		"type":"string",
		"enum":["replace","add"],
		"default":"replace",
		"description":"replace: replace the active skill set; add: add to the active skill set."
	}
},
"required":["skills"],
"additionalProperties":false
}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsLoad},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
