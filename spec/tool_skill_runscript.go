package spec

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsRunScript llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skills.runscript"

func SkillsRunScriptTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019c4188-ac5a-7e5c-8e68-b22e897c4885",
		Slug:          "skills.runscript",
		Version:       "v1.0.0",
		DisplayName:   "Skills Run Script",
		Description:   "execute a script from within the selected active skill",
		Tags:          []string{"skills", "exec"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
"$schema":"http://json-schema.org/draft-07/schema#",
"type":"object",
"properties":{
	"skillName":{"type":"string","description":"skill name"},
	"skillLocation":{"type":"string","description":"provider-interpreted base location for the skill"},
	"scriptLocation":{"type":"string","description":"script location; typically relative to skillLocation"},
	"args":{"type":"array","items":{"type":"string"},"description":"positional arguments passed to the script"},
	"env":{"type":"object","additionalProperties":{"type":"string"},"description":"environment variables for the script process"},
	"workDir":{"type":"string","description":"working directory location for the script; typically relative to skillLocation"}
},
"required":["skillName","skillLocation","scriptLocation"],
"additionalProperties":false
}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsRunScript},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
