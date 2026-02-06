package agentskills

import (
	"errors"

	"github.com/flexigpt/llmtools-go"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/skilltool"
	"github.com/flexigpt/agentskills-go/spec"
)

type Session struct {
	rt *Runtime
	id spec.SessionID
}

func (s *Session) ID() spec.SessionID { return s.id }

// Tools returns the skills tool specs (skills.load/unload/read/run_script).
func (s *Session) Tools() []llmtoolsgoSpec.Tool { return skilltool.Tools() }

// RegisterTools registers skills tools into an existing llmtools-go Registry.
func (s *Session) RegisterTools(reg *llmtools.Registry) error {
	if s == nil || s.rt == nil {
		return errors.New("nil session runtime")
	}
	return skilltool.Register(reg, s.rt, s.id)
}

// NewToolsRegistry returns a new llmtools-go Registry containing only the skills tools.
func (s *Session) NewToolsRegistry(opts ...llmtools.RegistryOption) (*llmtools.Registry, error) {
	if s == nil || s.rt == nil {
		return nil, errors.New("nil session runtime")
	}
	return skilltool.NewSkillsRegistry(s.rt, s.id, opts...)
}

// NewToolsBuiltinRegistry returns a new llmtools-go Registry containing builtins + skills tools.
func (s *Session) NewToolsBuiltinRegistry(opts ...llmtools.RegistryOption) (*llmtools.Registry, error) {
	if s == nil || s.rt == nil {
		return nil, errors.New("nil session runtime")
	}
	return skilltool.NewSkillsBuiltinRegistry(s.rt, s.id, opts...)
}
