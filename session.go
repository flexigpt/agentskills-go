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

func (s *Session) Tools() []llmtoolsgoSpec.Tool { return skilltool.Tools() }

func (s *Session) RegisterTools(reg *llmtools.Registry) error {
	if s == nil || s.rt == nil {
		return errors.New("nil session runtime")
	}
	return skilltool.Register(reg, s.rt, s.id)
}
