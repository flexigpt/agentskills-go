package skilltool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/flexigpt/llmtools-go"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

const (
	funcIDSkillsLoad      llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool.Load"
	funcIDSkillsUnload    llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool.Unload"
	funcIDSkillsRead      llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool.Read"
	funcIDSkillsRunScript llmtoolsgoSpec.FuncID = "github.com/flexigpt/agentskills-go/skilltool.RunScript"
)

func Tools() []llmtoolsgoSpec.Tool {
	return []llmtoolsgoSpec.Tool{
		SkillsLoadTool(),
		SkillsUnloadTool(),
		SkillsReadTool(),
		SkillsRunScriptTool(),
	}
}

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
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: funcIDSkillsLoad},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}

func SkillsUnloadTool() llmtoolsgoSpec.Tool {
	return llmtoolsgoSpec.Tool{
		SchemaVersion: llmtoolsgoSpec.SchemaVersion,
		ID:            "019bfeda-33f2-7315-9007-de55935d2402",
		Slug:          "skills.unload",
		Version:       "v1.0.0",
		DisplayName:   "Skills Unload",
		Description:   "Unload skills from the current session.",
		Tags:          []string{"skills"},
		ArgSchema: llmtoolsgoSpec.JSONSchema(`{
		  "$schema":"http://json-schema.org/draft-07/schema#",
		  "type":"object",
		  "properties":{
		    "names":{"type":"array","items":{"type":"string"}},
		    "all":{"type":"boolean","default":false}
		  },
		  "additionalProperties":false
		}`),
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: funcIDSkillsUnload},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}

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
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: funcIDSkillsRead},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}

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
		GoImpl:     llmtoolsgoSpec.GoToolImpl{FuncID: funcIDSkillsRunScript},
		CreatedAt:  llmtoolsgoSpec.SchemaStartTime,
		ModifiedAt: llmtoolsgoSpec.SchemaStartTime,
	}
}

func Bind(
	rt spec.Runtime,
	sessionID spec.SessionID,
) (map[llmtoolsgoSpec.FuncID]llmtoolsgoSpec.ToolFunc, error) {
	if rt == nil {
		return nil, errors.New("nil runtime")
	}
	out := map[llmtoolsgoSpec.FuncID]llmtoolsgoSpec.ToolFunc{}

	out[funcIDSkillsLoad] = func(ctx context.Context, in json.RawMessage) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
		args, err := decodeStrict[spec.LoadArgs](in)
		if err != nil {
			return nil, err
		}
		res, err := rt.Load(ctx, sessionID, args)
		if err != nil {
			return nil, err
		}
		return textJSON(res)
	}

	out[funcIDSkillsUnload] = func(ctx context.Context, in json.RawMessage) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
		args, err := decodeStrict[spec.UnloadArgs](in)
		if err != nil {
			return nil, err
		}
		res, err := rt.Unload(ctx, sessionID, args)
		if err != nil {
			return nil, err
		}
		return textJSON(res)
	}

	out[funcIDSkillsRead] = func(ctx context.Context, in json.RawMessage) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
		args, err := decodeStrict[spec.ReadArgs](in)
		if err != nil {
			return nil, err
		}
		return rt.Read(ctx, sessionID, args)
	}

	out[funcIDSkillsRunScript] = func(ctx context.Context, in json.RawMessage) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
		args, err := decodeStrict[spec.RunScriptArgs](in)
		if err != nil {
			return nil, err
		}
		res, err := rt.RunScript(ctx, sessionID, args)
		if err != nil {
			return nil, err
		}
		return textJSON(res)
	}

	return out, nil
}

func Register(r *llmtools.Registry, rt spec.Runtime, sessionID spec.SessionID) error {
	if r == nil {
		return errors.New("nil registry")
	}
	bound, err := Bind(rt, sessionID)
	if err != nil {
		return err
	}
	for _, t := range Tools() {
		fn := bound[t.GoImpl.FuncID]
		if fn == nil {
			return fmt.Errorf("missing bound tool func for %s", t.GoImpl.FuncID)
		}
		if err := r.RegisterTool(t, fn); err != nil {
			return err
		}
	}
	return nil
}

func textJSON(v any) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode output: %w", err)
	}
	s := string(raw)
	if s == "" || s == "null" {
		return nil, nil
	}
	return []llmtoolsgoSpec.ToolStoreOutputUnion{
		{
			Kind: llmtoolsgoSpec.ToolStoreOutputKindText,
			TextItem: &llmtoolsgoSpec.ToolStoreOutputText{
				Text: s,
			},
		},
	}, nil
}

func decodeStrict[T any](raw json.RawMessage) (T, error) {
	var zero T

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var v T
	if err := dec.Decode(&v); err != nil {
		return zero, fmt.Errorf("invalid input: %w", err)
	}

	// Must be EOF after the first value.
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return zero, errors.New("invalid input: trailing data")
	} else if !errors.Is(err, io.EOF) {
		return zero, errors.New("invalid input: trailing data")
	}

	return v, nil
}
