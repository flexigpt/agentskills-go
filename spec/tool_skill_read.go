package spec

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsReadResource llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skills.readresource"

func SkillsReadResourceTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019c4188-73e6-7301-8d3d-28a5d9e23f9e",
		Slug:          "skills.readresource",
		Version:       "v1.0.0",
		DisplayName:   "Skills Read Resource",
		Description:   "read a skill-scoped resource relative to an active skill base location",
		Tags:          []string{"skills"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
"$schema":"http://json-schema.org/draft-07/schema#",
"type":"object",
"properties":{
	"skillName":{"type":"string","description":"skill name"},
	"skillLocation":{"type":"string","description":"provider-interpreted base location for the skill"},
	"resourceLocation":{"type":"string","description":"resource location; typically relative to skillLocation (provider-defined semantics)"},
	"encoding":{
		"type":"string",
		"enum":["text","binary"],
		"default":"text",
		"description":"text: return as UTF-8 text; binary: return binary as a base64-encoded string"
	}
},
"required":["skillName","skillLocation","resourceLocation"],
"additionalProperties":false
}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsReadResource},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
