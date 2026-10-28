package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexigpt/agentskills-go/provider"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

func TestProviderScopesResourcesAndScriptsToSkillRoot(t *testing.T) {
	skillRoot := t.TempDir()
	outsideRoot := t.TempDir()

	value, err := New(WithRunScripts(true))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	key := provider.ProviderSkillKey{
		Type:     Type,
		Name:     "scope-test",
		Location: skillRoot,
	}

	writeScopedSkillFixture(
		t,
		filepath.Join(skillRoot, "resource.txt"),
		"inside resource\n",
	)
	writeScopedSkillFixture(
		t,
		filepath.Join(skillRoot, "script.py"),
		"print('inside script')\n",
	)
	writeScopedSkillFixture(
		t,
		filepath.Join(outsideRoot, "outside.txt"),
		"outside resource\n",
	)
	writeScopedSkillFixture(
		t,
		filepath.Join(outsideRoot, "outside.py"),
		"print('outside script')\n",
	)

	outputs, err := value.ReadResource(
		t.Context(),
		key,
		"resource.txt",
		provider.ReadResourceEncodingText,
	)
	if err != nil {
		t.Fatalf("ReadResource(inside resource) error = %v", err)
	}
	if len(outputs) != 1 || outputs[0].Kind != llmtoolsgoSpec.ToolOutputKindText {
		t.Fatalf(
			"ReadResource(inside resource) outputs = %#v, want one text output",
			outputs,
		)
	}

	_, err = value.ReadResource(
		t.Context(),
		key,
		filepath.Join(outsideRoot, "outside.txt"),
		provider.ReadResourceEncodingText,
	)
	assertScopedSkillError(t, err, "outside allowed roots")

	_, err = value.RunScript(
		t.Context(),
		key,
		filepath.Join(outsideRoot, "outside.py"),
		nil,
		nil,
		"",
	)
	assertScopedSkillError(t, err, "outside allowed roots")

	_, err = value.RunScript(
		t.Context(),
		key,
		"script.py",
		nil,
		nil,
		outsideRoot,
	)
	assertScopedSkillError(t, err, "outside allowed roots")
}

func writeScopedSkillFixture(
	t *testing.T,
	path string,
	content string,
) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func assertScopedSkillError(
	t *testing.T,
	err error,
	wantSubstring string,
) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want text containing %q", wantSubstring)
	}
	if !strings.Contains(
		strings.ToLower(err.Error()),
		strings.ToLower(wantSubstring),
	) {
		t.Fatalf(
			"error = %q, want text containing %q",
			err.Error(),
			wantSubstring,
		)
	}
}
