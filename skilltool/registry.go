package skilltool

import (
	"errors"

	"github.com/flexigpt/agentskills-go/spec"
	"github.com/flexigpt/llmtools-go"
)

// NewSkillsRegistry creates an llmtools-go Registry and registers ONLY the skills tools into it.
func NewSkillsRegistry(
	rt spec.Runtime,
	sessionID spec.SessionID,
	opts ...llmtools.RegistryOption,
) (*llmtools.Registry, error) {
	if rt == nil {
		return nil, errors.New("nil runtime")
	}
	r, err := llmtools.NewRegistry(opts...)
	if err != nil {
		return nil, err
	}
	if err := Register(r, rt, sessionID); err != nil {
		return nil, err
	}
	return r, nil
}
