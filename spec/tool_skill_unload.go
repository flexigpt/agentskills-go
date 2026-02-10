package spec

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsUnload llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skills.unload"

func SkillsUnloadTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019c4188-ce79-7a71-ac5a-a54cf5de3815",
		Slug:          "skills.unload",
		Version:       "v1.0.0",
		DisplayName:   "Skills Unload",
		Description:   "unload skills from the current session",
		Tags:          []string{"skills"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
"$schema":"http://json-schema.org/draft-07/schema#",
"type":"object",
"properties":{
	"skills":{
		"type":"array",
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
	"all":{
		"type":"boolean",
		"default":false,
		"description":"if true, unload all active skills"
	}
},
"anyOf":[
	{
		"required":["all"],
		"properties":{"all":{"const":true}}
	},
	{
		"required":["skills"],
		"properties":{"skills":{"minItems":1}}
	}
],
"additionalProperties":false
}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsUnload},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
