package integration

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/flexigpt/agentskills-go/document"
	"github.com/flexigpt/agentskills-go/provider"
	"github.com/flexigpt/agentskills-go/runtime"
	"github.com/flexigpt/agentskills-go/runtime/spec"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

const (
	availableSkillsStart         = "<<<AVAILABLE_SKILLS>>>"
	availableSkillsEnd           = "<<<END_AVAILABLE_SKILLS>>>"
	activeSkillsStart            = "<<<ACTIVE_SKILLS>>>"
	activeSkillsEnd              = "<<<END_ACTIVE_SKILLS>>>"
	skillsPromptStart            = "<<<SKILLS_PROMPT>>>"
	skillsPromptEnd              = "<<<END_SKILLS_PROMPT>>>"
	nextAvailableSkillsSeparator = "---"
	nextActiveSkillsSeparator    = "<!-- SKILL SEPARATOR -->"
	nonePromptString             = "(none)"
)

type fakeProvider struct {
	typ string

	indexCalls    atomic.Int32
	loadBodyCalls atomic.Int32

	indexFn    func(context.Context, provider.SkillDef) (provider.ProviderSkillIndexRecord, error)
	loadBodyFn func(context.Context, provider.ProviderSkillKey) (string, error)
}

func (p *fakeProvider) Type() string { return p.typ }

func (p *fakeProvider) Index(ctx context.Context, def provider.SkillDef) (provider.ProviderSkillIndexRecord, error) {
	p.indexCalls.Add(1)
	if p.indexFn != nil {
		return p.indexFn(ctx, def)
	}
	return provider.ProviderSkillIndexRecord{
		Key:         provider.ProviderSkillKey(def),
		Description: "desc:" + def.Type + ":" + def.Name,
	}, nil
}

func (p *fakeProvider) SupportsRunScript() bool {
	return false
}

func (p *fakeProvider) LoadBody(ctx context.Context, key provider.ProviderSkillKey) (string, error) {
	p.loadBodyCalls.Add(1)
	if p.loadBodyFn != nil {
		return p.loadBodyFn(ctx, key)
	}
	// Include markup + '&' so we can detect CDATA vs escaping.
	return "BODY<" + key.Name + ">&", nil
}

func (p *fakeProvider) ReadResource(
	ctx context.Context,
	key provider.ProviderSkillKey,
	resourceLocation string,
	encoding provider.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, provider.ErrInvalidProviderArgument
}

func (p *fakeProvider) RunScript(
	ctx context.Context,
	key provider.ProviderSkillKey,
	scriptLocation string,
	args []string,
	env map[string]string,
	workDir string,
) (provider.RunScriptOut, error) {
	return provider.RunScriptOut{}, provider.ErrRunScriptUnsupported
}

type runtimeTestProvider struct {
	typ string
}

func (p *runtimeTestProvider) Type() string { return p.typ }

func (p *runtimeTestProvider) Index(
	ctx context.Context,
	def provider.SkillDef,
) (provider.ProviderSkillIndexRecord, error) {
	if err := ctx.Err(); err != nil {
		return provider.ProviderSkillIndexRecord{}, err
	}

	key := provider.ProviderSkillKey{Type: def.Type, Name: def.Name, Location: "CANON:" + def.Location}

	switch def.Name {
	case "instructions":
		return provider.ProviderSkillIndexRecord{
			Key:         key,
			Description: "instruction skill",
			Insert:      document.SkillInsertInstructions,
		}, nil
	case "template":
		return provider.ProviderSkillIndexRecord{
			Key:         key,
			Description: "template skill",
			Insert:      document.SkillInsertUserMessage,
			Arguments: []document.SkillArgument{
				{Name: "name", Default: "World"},
				{Name: "mood", Default: "calm"},
			},
			RawFrontmatter: map[string]any{"insert": "user-message", "kind": "template"},
		}, nil
	default:
		return provider.ProviderSkillIndexRecord{Key: key, Description: "skill"}, nil
	}
}

func (p *runtimeTestProvider) SupportsRunScript() bool {
	return false
}

func (p *runtimeTestProvider) LoadBody(ctx context.Context, key provider.ProviderSkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch key.Name {
	case "instructions":
		return "# Instructions\nUse this skill for guidance.\n", nil
	case "template":
		return "Hello $name, mood={{ mood }}. Escaped \\$name and {{ unknown }}.\n", nil
	default:
		return "BODY:" + key.Name, nil
	}
}

func (p *runtimeTestProvider) ReadResource(
	ctx context.Context,
	key provider.ProviderSkillKey,
	resourceLocation string,
	encoding provider.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	return nil, spec.ErrInvalidRuntimeArgument
}

func (p *runtimeTestProvider) RunScript(
	ctx context.Context,
	key provider.ProviderSkillKey,
	scriptLocation string,
	args []string,
	env map[string]string,
	workDir string,
) (provider.RunScriptOut, error) {
	return provider.RunScriptOut{}, provider.ErrRunScriptUnsupported
}

func mustNewRuntime(t *testing.T, opts ...runtime.Option) *runtime.Runtime {
	t.Helper()
	rt, err := runtime.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if rt == nil {
		t.Fatalf("New: got nil runtime")
	}
	return rt
}

func mustAddSkill(t *testing.T, rt *runtime.Runtime, ctx context.Context, def provider.SkillDef) spec.SkillRecord {
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
	rt *runtime.Runtime,
	ctx context.Context,
	opts ...runtime.SessionOption,
) (spec.SessionID, []provider.SkillDef) {
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

type parsedAvailableSkill struct {
	Name        string
	Location    string
	Description string
}

type parsedAvailablePrompt struct {
	Skills []parsedAvailableSkill
}

type parsedActiveSkill struct {
	Name string
	Body string
}

type parsedActivePrompt struct {
	Skills []parsedActiveSkill
}

type parsedSkillsPrompt struct {
	Available *parsedAvailablePrompt
	Active    *parsedActivePrompt
}

func assertStandaloneAvailablePrompt(t *testing.T, s string) {
	t.Helper()

	if !strings.HasPrefix(s, availableSkillsStart) {
		t.Fatalf("expected available-skills standalone prompt root, got:\n%s", s)
	}
	if strings.Contains(s, skillsPromptStart) {
		t.Fatalf("did not expect combined wrapper in standalone available prompt:\n%s", s)
	}
	if strings.Contains(s, activeSkillsStart) {
		t.Fatalf("did not expect active section in standalone available prompt:\n%s", s)
	}
	if !strings.Contains(s, availableSkillsEnd) {
		t.Fatalf("missing available-skills end delimiter:\n%s", s)
	}
}

func assertStandaloneActivePrompt(t *testing.T, s string) {
	t.Helper()

	if !strings.HasPrefix(s, activeSkillsStart) {
		t.Fatalf("expected active-skills standalone prompt root, got:\n%s", s)
	}
	if strings.Contains(s, skillsPromptStart) {
		t.Fatalf("did not expect combined wrapper in standalone active prompt:\n%s", s)
	}
	if strings.Contains(s, availableSkillsStart) {
		t.Fatalf("did not expect available section in standalone active prompt:\n%s", s)
	}
	if !strings.Contains(s, activeSkillsEnd) {
		t.Fatalf("missing active-skills end delimiter:\n%s", s)
	}
}

func assertWrappedSkillsPrompt(t *testing.T, s string) {
	t.Helper()

	if !strings.HasPrefix(s, skillsPromptStart) {
		t.Fatalf("expected combined wrapper start delimiter, got:\n%s", s)
	}
	if !strings.Contains(s, skillsPromptEnd) {
		t.Fatalf("expected combined wrapper end delimiter, got:\n%s", s)
	}
	if !strings.Contains(s, availableSkillsStart) || !strings.Contains(s, availableSkillsEnd) {
		t.Fatalf("expected combined wrapper to contain available section, got:\n%s", s)
	}
	if !strings.Contains(s, activeSkillsStart) || !strings.Contains(s, activeSkillsEnd) {
		t.Fatalf("expected combined wrapper to contain active section, got:\n%s", s)
	}
}

func assertFirstRecordNotPrefixedBySeparator(t *testing.T, s, start, end string, isActiveSkill bool) {
	t.Helper()

	body := mustExtractPromptBlock(t, s, start, end)
	first := firstNonEmptyLine(body)
	if first == "" || first == nonePromptString {
		return
	}
	if isActiveSkill && first == nextActiveSkillsSeparator {
		t.Fatalf("unexpected leading separator after %s:\n%s", start, s)
	} else if first == nextAvailableSkillsSeparator {
		t.Fatalf("unexpected leading separator after %s:\n%s", start, s)
	}

	if !strings.HasPrefix(first, "name: ") {
		t.Fatalf("expected first content line after %s to begin with %q, got %q\nprompt=%s", start, "name: ", first, s)
	}
}

func mustParseSkillsPromptDocument(t *testing.T, s string) parsedSkillsPrompt {
	t.Helper()

	if strings.Contains(s, skillsPromptStart) {
		s = mustExtractPromptBlock(t, s, skillsPromptStart, skillsPromptEnd)
	}

	var doc parsedSkillsPrompt
	if strings.Contains(s, availableSkillsStart) {
		av := mustParseAvailableSkillsPrompt(t, s)
		doc.Available = &av
	}
	if strings.Contains(s, activeSkillsStart) {
		act := mustParseActiveSkillsPrompt(t, s)
		doc.Active = &act
	}
	return doc
}

func mustParseAvailableSkillsPrompt(t *testing.T, s string) parsedAvailablePrompt {
	t.Helper()

	body := mustExtractPromptBlock(t, s, availableSkillsStart, availableSkillsEnd)
	if strings.TrimSpace(body) == "" || strings.TrimSpace(body) == nonePromptString {
		return parsedAvailablePrompt{}
	}

	recs := splitPromptRecords(body, false)
	out := make([]parsedAvailableSkill, 0, len(recs))

	for _, rec := range recs {
		lines := strings.Split(strings.TrimRight(rec, "\r\n"), "\n")
		var item parsedAvailableSkill

		for _, line := range lines {
			switch {
			case strings.HasPrefix(line, "name: "):
				item.Name = strings.TrimPrefix(line, "name: ")
			case strings.HasPrefix(line, "location: "):
				item.Location = strings.TrimPrefix(line, "location: ")
			case strings.HasPrefix(line, "description: "):
				item.Description = strings.TrimPrefix(line, "description: ")
			default:
				t.Fatalf("unexpected available-skill line %q in record:\n%s", line, rec)
			}
		}

		if item.Name == "" {
			t.Fatalf("available-skill record missing name:\n%s", rec)
		}

		out = append(out, item)
	}

	return parsedAvailablePrompt{Skills: out}
}

func mustParseActiveSkillsPrompt(t *testing.T, s string) parsedActivePrompt {
	t.Helper()

	body := mustExtractPromptBlock(t, s, activeSkillsStart, activeSkillsEnd)
	if strings.TrimSpace(body) == "" || strings.TrimSpace(body) == nonePromptString {
		return parsedActivePrompt{}
	}

	recs := splitPromptRecords(body, true)
	out := make([]parsedActiveSkill, 0, len(recs))

	for _, rec := range recs {
		lines := strings.Split(strings.TrimRight(rec, "\r\n"), "\n")
		if len(lines) < 2 {
			t.Fatalf("active-skill record too short:\n%s", rec)
		}
		if !strings.HasPrefix(lines[0], "name: ") {
			t.Fatalf("active-skill record missing name header:\n%s", rec)
		}
		if lines[1] != "body:" {
			t.Fatalf("active-skill record missing body header:\n%s", rec)
		}

		item := parsedActiveSkill{
			Name: strings.TrimPrefix(lines[0], "name: "),
		}
		if len(lines) > 2 {
			item.Body = strings.Join(lines[2:], "\n")
		}

		out = append(out, item)
	}

	return parsedActivePrompt{Skills: out}
}

func mustExtractPromptBlock(t *testing.T, s, start, end string) string {
	t.Helper()

	startIdx := strings.Index(s, start)
	if startIdx < 0 {
		t.Fatalf("missing start delimiter %q in prompt:\n%s", start, s)
	}
	startIdx += len(start)

	if startIdx < len(s) && s[startIdx] == '\n' {
		startIdx++
	}

	rest := s[startIdx:]
	before, _, ok := strings.Cut(rest, end)
	if !ok {
		t.Fatalf("missing end delimiter %q in prompt:\n%s", end, s)
	}

	return strings.TrimRight(before, "\r\n")
}

func splitPromptRecords(body string, isActiveSkill bool) []string {
	body = strings.TrimRight(body, "\r\n")
	if body == "" {
		return nil
	}

	lines := strings.Split(body, "\n")
	recs := make([]string, 0, 1)
	cur := make([]string, 0, len(lines))

	flush := func() {
		if len(cur) == 0 {
			return
		}
		rec := strings.Join(cur, "\n")
		if strings.TrimSpace(rec) != "" {
			recs = append(recs, rec)
		}
		cur = nil
	}

	for _, line := range lines {
		if (isActiveSkill && line == nextActiveSkillsSeparator) || line == nextAvailableSkillsSeparator {
			flush()
			continue
		}

		cur = append(cur, line)
	}
	flush()

	return recs
}

func firstNonEmptyLine(s string) string {
	for line := range strings.SplitSeq(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}
