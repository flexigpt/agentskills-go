package session

import (
	"context"
	"errors"
	"sync"
	"testing"

	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

type recordingProvider struct {
	typ string

	mu sync.Mutex

	lastReadKey     spec.SkillKey
	lastReadPath    string
	lastReadEnc     spec.ReadEncoding
	readCalls       int
	lastRunKey      spec.SkillKey
	lastRunPath     string
	runCalls        int
	readReturnError error
	runReturnError  error
}

func (p *recordingProvider) Type() string { return p.typ }

func (p *recordingProvider) Index(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	return spec.SkillRecord{Key: key, Description: "d"}, nil
}

func (p *recordingProvider) LoadBody(ctx context.Context, key spec.SkillKey) (string, error) {
	return "body", nil
}

func (p *recordingProvider) ReadResource(
	ctx context.Context,
	key spec.SkillKey,
	resourcePath string,
	encoding spec.ReadEncoding,
) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readCalls++
	p.lastReadKey = key
	p.lastReadPath = resourcePath
	p.lastReadEnc = encoding
	if p.readReturnError != nil {
		return nil, p.readReturnError
	}
	return []llmtoolsgoSpec.ToolStoreOutputUnion{}, nil
}

func (p *recordingProvider) RunScript(
	ctx context.Context,
	key spec.SkillKey,
	scriptPath string,
	args []string,
	env map[string]string,
	workdir string,
) (spec.RunScriptOut, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runCalls++
	p.lastRunKey = key
	p.lastRunPath = scriptPath
	if p.runReturnError != nil {
		return spec.RunScriptOut{}, p.runReturnError
	}
	return spec.RunScriptOut{Path: scriptPath, ExitCode: 0}, nil
}

func TestTools_Read_DefaultSkillSelection(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	k1 := spec.SkillKey{Type: "t", Name: "a", Path: "p1"}
	k2 := spec.SkillKey{Type: "t", Name: "b", Path: "p2"}
	cat.add(k1, "ok")
	cat.add(k2, "ok")

	p := &recordingProvider{typ: "t"}
	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": p},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	// Activate one skill; omitted skill defaults to it.
	handles, err := s.ActivateKeys(t.Context(), []spec.SkillKey{k1}, spec.LoadModeReplace)
	if err != nil || len(handles) == 0 {
		t.Fatalf("ActivateKeys: %v", err)
	}

	_, err = s.toolRead(t.Context(), spec.ReadArgs{Path: "x.txt", Skill: handles[0]})
	if err != nil {
		t.Fatalf("toolRead: %v", err)
	}

	p.mu.Lock()
	if p.readCalls != 1 || p.lastReadKey != k1 || p.lastReadPath != "x.txt" || p.lastReadEnc != spec.ReadEncodingText {
		t.Fatalf(
			"unexpected provider call: calls=%d key=%+v path=%q enc=%q",
			p.readCalls,
			p.lastReadKey,
			p.lastReadPath,
			p.lastReadEnc,
		)
	}
	p.mu.Unlock()

	// Activate two skills; omitted skill should be invalid.
	_, err = s.ActivateKeys(t.Context(), []spec.SkillKey{k1, k2}, spec.LoadModeReplace)
	if err != nil {
		t.Fatalf("ActivateKeys: %v", err)
	}

	_, err = s.toolRead(t.Context(), spec.ReadArgs{Path: "x.txt"})
	if !errors.Is(err, spec.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument when multiple active and no skill, got %v", err)
	}

	// Explicit skill handle works.
	_, err = s.toolRead(t.Context(), spec.ReadArgs{Skill: spec.SkillHandle{Name: "b", Path: "p2"}, Path: "y.txt"})
	if err != nil {
		t.Fatalf("toolRead explicit: %v", err)
	}
}

func TestTools_RunScript_DefaultSkillSelection(t *testing.T) {
	t.Parallel()

	cat := newMemCatalog()
	k1 := spec.SkillKey{Type: "t", Name: "a", Path: "p1"}
	cat.add(k1, "ok")

	p := &recordingProvider{typ: "t"}
	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": p},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	handles, err := s.ActivateKeys(t.Context(), []spec.SkillKey{k1}, spec.LoadModeReplace)
	if err != nil || len(handles) == 0 {
		t.Fatalf("ActivateKeys: %v", err)
	}

	res, err := s.toolRunScript(t.Context(), spec.RunScriptArgs{Path: "scripts/x.sh", Skill: handles[0]})
	if err != nil {
		t.Fatalf("toolRunScript: %v", err)
	}
	if res.ExitCode != 0 || res.Path != "scripts/x.sh" {
		t.Fatalf("unexpected run result: %+v", res)
	}
}
