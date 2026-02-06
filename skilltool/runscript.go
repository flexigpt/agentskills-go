package skilltool

import llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

const FuncIDSkillsRunScript llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool/runscript.RunScript"

func SkillsRunScriptTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2404",
		Slug:          "skills.run_script",
		Version:       "v1.0.0",
		DisplayName:   "Skills Run Script",
		Description:   "Execute a script from the active skill's scripts/ directory.",
		Tags:          []string{"skills", "shell", "exec"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
		  "$schema":"http://json-schema.org/draft-07/schema#",
		  "type":"object",
		  "properties":{
		    "skill":{"type":"string","description":"Optional skill name. If omitted, uses most recently loaded skill."},
		    "path":{"type":"string","description":"Relative path under skill root; must be under scripts/"},
		    "args":{"type":"array","items":{"type":"string"}},
		    "env":{"type":"object","additionalProperties":{"type":"string"}},
		    "workdir":{"type":"string","description":"Relative workdir under skill root. Default '.'"}
		  },
		  "required":["path"],
		  "additionalProperties":false
		}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: FuncIDSkillsRunScript},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}
