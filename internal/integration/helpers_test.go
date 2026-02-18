package integration

import (
	"context"
	"encoding/xml"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/flexigpt/agentskills-go"
	"github.com/flexigpt/agentskills-go/spec"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

type fakeProvider struct {
	typ string

	indexCalls    atomic.Int32
	loadBodyCalls atomic.Int32

	indexFn    func(context.Context, spec.SkillDef) (spec.ProviderSkillIndexRecord, error)
	loadBodyFn func(context.Context, spec.ProviderSkillKey) (string, error)
}

func (p *fakeProvider) Type() string { return p.typ }

func (p *fakeProvider) Index(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
	p.indexCalls.Add(1)
	if p.indexFn != nil {
		return p.indexFn(ctx, def)
	}
	return spec.ProviderSkillIndexRecord{
		Key:         spec.ProviderSkillKey(def),
		Description: "desc:" + def.Type + ":" + def.Name,
	}, nil
}

func (p *fakeProvider) LoadBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	p.loadBodyCalls.Add(1)
	if p.loadBodyFn != nil {
		return p.loadBodyFn(ctx, key)
	}
	// Include markup + '&' so we can detect CDATA vs escaping.
	return "BODY<" + key.Name + ">&", nil
}

func (p *fakeProvider) ReadResource(
	ctx context.Context,
	key spec.ProviderSkillKey,
	resourceLocation string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, spec.ErrInvalidArgument
}

func (p *fakeProvider) RunScript(
	ctx context.Context,
	key spec.ProviderSkillKey,
	scriptLocation string,
	args []string,
	env map[string]string,
	workDir string,
) (spec.RunScriptOut, error) {
	return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
}

func mustNewRuntime(t *testing.T, opts ...agentskills.Option) *agentskills.Runtime {
	t.Helper()
	rt, err := agentskills.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if rt == nil {
		t.Fatalf("New: got nil runtime")
	}
	return rt
}

func mustAddSkill(t *testing.T, rt *agentskills.Runtime, ctx context.Context, def spec.SkillDef) spec.SkillRecord {
	t.Helper()
	rec, err := rt.AddSkill(ctx, def)
	if err != nil {
		t.Fatalf("AddSkill(%+v): %v", def, err)
	}
	if rec.Def != def {
		t.Fatalf("AddSkill: returned record.Def mismatch: got=%+v want=%+v", rec.Def, def)
	}
	return rec
}

func mustNewSession(
	t *testing.T,
	rt *agentskills.Runtime,
	ctx context.Context,
	opts ...agentskills.SessionOption,
) (spec.SessionID, []spec.SkillDef) {
	t.Helper()
	sid, active, err := rt.NewSession(ctx, opts...)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sid == "" {
		t.Fatalf("NewSession: got empty session id")
	}
	return sid, active
}

func xmlRootName(t *testing.T, s string) string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(s))
	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("xmlRootName token: %v\nxml=%s", err, s)
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local
		}
	}
}

type availableSkillsDoc struct {
	XMLName xml.Name `xml:"availableSkills"`
	Skills  []struct {
		Name        string `xml:"name"`
		Description string `xml:"description"`
		Location    string `xml:"location"`
	} `xml:"skill"`
}

type activeSkillsDoc struct {
	XMLName xml.Name `xml:"activeSkills"`
	Skills  []struct {
		Name string `xml:"name,attr"`
		Body string `xml:",chardata"`
	} `xml:"skill"`
}

type skillsPromptDoc struct {
	XMLName   xml.Name            `xml:"skillsPrompt"`
	Available *availableSkillsDoc `xml:"availableSkills"`
	Active    *activeSkillsDoc    `xml:"activeSkills"`
}

const availableSkillsRoot = "availableSkills"

func mustUnmarshalAvailable(t *testing.T, s string) availableSkillsDoc {
	t.Helper()
	var doc availableSkillsDoc
	if err := xml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("unmarshal availableSkills: %v\nxml=%s", err, s)
	}
	if doc.XMLName.Local != availableSkillsRoot {
		t.Fatalf("expected root %s, got %q", availableSkillsRoot, doc.XMLName.Local)
	}
	return doc
}

func mustUnmarshalActive(t *testing.T, s string) activeSkillsDoc {
	t.Helper()
	var doc activeSkillsDoc
	if err := xml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("unmarshal activeSkills: %v\nxml=%s", err, s)
	}
	if doc.XMLName.Local != "activeSkills" {
		t.Fatalf("expected root activeSkills, got %q", doc.XMLName.Local)
	}
	return doc
}

func mustUnmarshalPrompt(t *testing.T, s string) skillsPromptDoc {
	t.Helper()
	var doc skillsPromptDoc
	if err := xml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("unmarshal skillsPrompt: %v\nxml=%s", err, s)
	}
	if doc.XMLName.Local != "skillsPrompt" {
		t.Fatalf("expected root skillsPrompt, got %q", doc.XMLName.Local)
	}
	return doc
}
