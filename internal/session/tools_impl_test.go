package session

import (
	"context"
	"errors"
	"maps"
	"sync"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"
)

type recordingProvider struct {
	typ string

	mu sync.Mutex

	readCalls    int
	lastReadKey  spec.ProviderSkillKey
	lastReadPath string
	lastReadEnc  spec.ReadResourceEncoding

	runCalls    int
	lastRunKey  spec.ProviderSkillKey
	lastRunPath string
	lastRunArgs []string
	lastRunEnv  map[string]string
	lastRunWD   string
	runOut      spec.RunScriptOut
	readErr     error
	runErr      error
}

func (p *recordingProvider) Type() string { return p.typ }

func (p *recordingProvider) Index(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
	return spec.ProviderSkillIndexRecord{
		Key: spec.ProviderSkillKey(def),
	}, nil
}

func (p *recordingProvider) LoadBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	return "body", nil
}

func (p *recordingProvider) ReadResource(
	ctx context.Context,
	key spec.ProviderSkillKey,
	resourcePath string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readCalls++
	p.lastReadKey = key
	p.lastReadPath = resourcePath
	p.lastReadEnc = encoding
	if p.readErr != nil {
		return nil, p.readErr
	}
	return []llmtoolsgoSpec.ToolOutputUnion{}, nil
}

func (p *recordingProvider) RunScript(
	ctx context.Context,
	key spec.ProviderSkillKey,
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
	p.lastRunArgs = append([]string(nil), args...)
	if env == nil {
		p.lastRunEnv = nil
	} else {
		p.lastRunEnv = make(map[string]string, len(env))
		maps.Copy(p.lastRunEnv, env)
	}
	p.lastRunWD = workdir
	if p.runErr != nil {
		return spec.RunScriptOut{}, p.runErr
	}
	if p.runOut.Location == "" && p.runOut.ExitCode == 0 && p.runOut.DurationMS == 0 && !p.runOut.TimedOut {
		return spec.RunScriptOut{Location: scriptPath, ExitCode: 0}, nil
	}
	return p.runOut, nil
}

func TestTools_toolLoad_ValidationsAndActivation(t *testing.T) {
	t.Parallel()

	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "abs1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "abs2"}
	h1 := spec.SkillHandle{Name: "a", Location: "rel1"}
	h2 := spec.SkillHandle{Name: "b", Location: "rel2"}

	newSessionForTest := func() (*Session, *memCatalog) {
		cat := newMemCatalog()
		cat.addWithHandle(k1, h1, "ok")
		cat.addWithHandle(k2, h2, "ok")

		s := newSession(SessionConfig{
			ID:                  "id",
			Catalog:             cat,
			Providers:           mapResolver{"t": &recordingProvider{typ: "t"}},
			MaxActivePerSession: 8,
			Touch:               func() {},
		})
		return s, cat
	}

	cases := []struct {
		name   string
		args   spec.LoadArgs
		isErr  func(error) bool
		want   []spec.SkillHandle
		verify func(t *testing.T, s *Session)
	}{
		{
			name: "rejects_empty_skills",
			args: spec.LoadArgs{Skills: nil, Mode: spec.LoadModeReplace},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "rejects_invalid_mode",
			args: spec.LoadArgs{Skills: []spec.SkillHandle{h1}, Mode: spec.LoadMode("nope")},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "rejects_missing_handle_fields",
			args: spec.LoadArgs{Skills: []spec.SkillHandle{{Name: "", Location: "x"}}},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "unknown_handle",
			args: spec.LoadArgs{Skills: []spec.SkillHandle{{Name: "nope", Location: "x"}}},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrSkillNotFound)
			},
		},
		{
			name: "defaults_mode_to_replace",
			args: spec.LoadArgs{Skills: []spec.SkillHandle{h1}, Mode: spec.LoadMode("")},
			want: []spec.SkillHandle{h1},
		},
		{
			name: "dedupes_handles",
			args: spec.LoadArgs{Skills: []spec.SkillHandle{h1, h1, h2}, Mode: spec.LoadModeReplace},
			want: []spec.SkillHandle{h1, h2},
		},
		{
			name: "add_appends_and_preserves_existing_order",
			args: spec.LoadArgs{Skills: []spec.SkillHandle{h2}, Mode: spec.LoadModeAdd},
			verify: func(t *testing.T, s *Session) {
				t.Helper()
				if _, err := s.toolLoad(t.Context(), spec.LoadArgs{Skills: []spec.SkillHandle{h1}}); err != nil {
					t.Fatalf("pre toolLoad: %v", err)
				}
			},
			want: []spec.SkillHandle{h1, h2},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := newSessionForTest()
			if tc.verify != nil {
				tc.verify(t, s)
			}

			out, err := s.toolLoad(t.Context(), tc.args)
			if tc.isErr != nil {
				if err == nil || !tc.isErr(err) {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("toolLoad: %v", err)
			}
			if len(out.ActiveSkills) != len(tc.want) {
				t.Fatalf(
					"unexpected active count: got=%d want=%d active=%+v",
					len(out.ActiveSkills),
					len(tc.want),
					out.ActiveSkills,
				)
			}
			for i := range tc.want {
				if out.ActiveSkills[i] != tc.want[i] {
					t.Fatalf(
						"unexpected active[%d]: got=%+v want=%+v all=%+v",
						i,
						out.ActiveSkills[i],
						tc.want[i],
						out.ActiveSkills,
					)
				}
			}
		})
	}
}

func TestTools_toolUnload_ValidationsAndBehavior(t *testing.T) {
	t.Parallel()

	k1 := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "abs1"}
	k2 := spec.ProviderSkillKey{Type: "t", Name: "b", Location: "abs2"}
	h1 := spec.SkillHandle{Name: "a", Location: "rel1"}
	h2 := spec.SkillHandle{Name: "b", Location: "rel2"}

	newLoadedSession := func(t *testing.T) *Session {
		t.Helper()

		cat := newMemCatalog()
		cat.addWithHandle(k1, h1, "ok")
		cat.addWithHandle(k2, h2, "ok")

		s := newSession(SessionConfig{
			ID:                  "id",
			Catalog:             cat,
			Providers:           mapResolver{"t": &recordingProvider{typ: "t"}},
			MaxActivePerSession: 8,
			Touch:               func() {},
		})

		if _, err := s.toolLoad(t.Context(), spec.LoadArgs{Skills: []spec.SkillHandle{h1, h2}}); err != nil {
			t.Fatalf("toolLoad: %v", err)
		}
		return s
	}

	cases := []struct {
		name  string
		args  spec.UnloadArgs
		isErr func(error) bool
		want  []spec.SkillHandle
	}{
		{
			name: "rejects_missing_skills_when_all_false",
			args: spec.UnloadArgs{All: false, Skills: nil},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "rejects_missing_handle_fields",
			args: spec.UnloadArgs{Skills: []spec.SkillHandle{{Name: "", Location: "x"}}},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "unknown_handle",
			args: spec.UnloadArgs{Skills: []spec.SkillHandle{{Name: "nope", Location: "x"}}},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrSkillNotFound)
			},
		},
		{
			name: "unload_subset",
			args: spec.UnloadArgs{Skills: []spec.SkillHandle{h1}},
			want: []spec.SkillHandle{h2},
		},
		{
			name: "unload_all",
			args: spec.UnloadArgs{All: true},
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newLoadedSession(t)

			out, err := s.toolUnload(t.Context(), tc.args)
			if tc.isErr != nil {
				if err == nil || !tc.isErr(err) {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("toolUnload: %v", err)
			}

			if len(out.ActiveSkills) != len(tc.want) {
				t.Fatalf(
					"unexpected active count: got=%d want=%d active=%+v",
					len(out.ActiveSkills),
					len(tc.want),
					out.ActiveSkills,
				)
			}
			for i := range tc.want {
				if out.ActiveSkills[i] != tc.want[i] {
					t.Fatalf(
						"unexpected active[%d]: got=%+v want=%+v all=%+v",
						i,
						out.ActiveSkills[i],
						tc.want[i],
						out.ActiveSkills,
					)
				}
			}
		})
	}
}

func TestTools_toolRead_Validations_DefaultEncoding_CanonicalKeyPassedToProvider(t *testing.T) {
	// Canonical internal key uses abs, LLM-facing handle uses rel.
	k := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "abs"}
	h := spec.SkillHandle{Name: "a", Location: "rel"}

	cat := newMemCatalog()
	cat.addWithHandle(k, h, "ok")

	p := &recordingProvider{typ: "t"}

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": p},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	// Activate via toolLoad (handle -> canonical key).
	if _, err := s.toolLoad(t.Context(), spec.LoadArgs{Skills: []spec.SkillHandle{h}}); err != nil {
		t.Fatalf("toolLoad: %v", err)
	}

	cases := []struct {
		name  string
		args  spec.ReadResourceArgs
		isErr func(error) bool
		check func(t *testing.T)
	}{
		{
			name: "rejects_missing_skill_fields",
			args: spec.ReadResourceArgs{SkillName: "", SkillLocation: "x", ResourceLocation: "r"},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "rejects_missing_resource_location",
			args: spec.ReadResourceArgs{SkillName: "a", SkillLocation: "rel", ResourceLocation: ""},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "unknown_handle",
			args: spec.ReadResourceArgs{SkillName: "nope", SkillLocation: "x", ResourceLocation: "r"},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrSkillNotFound)
			},
		},
		{
			name: "not_active",
			args: spec.ReadResourceArgs{SkillName: "a", SkillLocation: "rel", ResourceLocation: "r"},
			check: func(t *testing.T) {
				t.Helper()
				if _, err := s.toolUnload(t.Context(), spec.UnloadArgs{All: true}); err != nil {
					t.Fatalf("toolUnload(all): %v", err)
				}
			},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrSkillNotActive)
			},
		},
		{
			name: "provider_not_found",
			args: spec.ReadResourceArgs{SkillName: "a", SkillLocation: "rel", ResourceLocation: "r"},
			check: func(t *testing.T) {
				t.Helper()
				// Re-activate then remove provider.
				if _, err := s.toolLoad(t.Context(), spec.LoadArgs{Skills: []spec.SkillHandle{h}}); err != nil {
					t.Fatalf("toolLoad: %v", err)
				}
				s.providers = mapResolver{} // no providers
			},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrProviderNotFound)
			},
		},
		{
			name: "success_default_encoding_and_canonical_key_passed",
			args: spec.ReadResourceArgs{
				SkillName:        "a",
				SkillLocation:    "rel",
				ResourceLocation: "x.txt",
				Encoding:         spec.ReadResourceEncoding(""), // default => text
			},
			check: func(t *testing.T) {
				t.Helper()
				s.providers = mapResolver{"t": p}
				if _, err := s.toolLoad(t.Context(), spec.LoadArgs{Skills: []spec.SkillHandle{h}}); err != nil {
					t.Fatalf("toolLoad: %v", err)
				}
			},
			isErr: nil,
		},
		{
			name: "success_explicit_binary_encoding",
			args: spec.ReadResourceArgs{
				SkillName:        "a",
				SkillLocation:    "rel",
				ResourceLocation: "y.bin",
				Encoding:         spec.ReadResourceEncodingBinary,
			},
			isErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.check != nil {
				tc.check(t)
			}

			_, err := s.toolRead(t.Context(), tc.args)
			if tc.isErr != nil {
				if err == nil || !tc.isErr(err) {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("toolRead: %v", err)
			}
		})
	}

	// Assert last successful call details (from the last success case).
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.readCalls == 0 {
		t.Fatalf("expected provider ReadResource to be called")
	}
	if p.lastReadKey != k {
		t.Fatalf("expected canonical key passed to provider: got=%+v want=%+v", p.lastReadKey, k)
	}
	if p.lastReadPath != "y.bin" {
		t.Fatalf("expected lastReadPath y.bin, got %q", p.lastReadPath)
	}
	if p.lastReadEnc != spec.ReadResourceEncodingBinary {
		t.Fatalf("expected lastReadEnc binary, got %q", p.lastReadEnc)
	}
}

func TestTools_toolRunScript_Validations_ArgsEnvWorkDirPassed(t *testing.T) {
	k := spec.ProviderSkillKey{Type: "t", Name: "a", Location: "abs"}
	h := spec.SkillHandle{Name: "a", Location: "rel"}

	cat := newMemCatalog()
	cat.addWithHandle(k, h, "ok")

	p := &recordingProvider{typ: "t"}

	s := newSession(SessionConfig{
		ID:                  "id",
		Catalog:             cat,
		Providers:           mapResolver{"t": p},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})

	// Activate via toolLoad.
	if _, err := s.toolLoad(t.Context(), spec.LoadArgs{Skills: []spec.SkillHandle{h}}); err != nil {
		t.Fatalf("toolLoad: %v", err)
	}

	cases := []struct {
		name  string
		args  spec.RunScriptArgs
		isErr func(error) bool
		check func(t *testing.T)
	}{
		{
			name: "rejects_missing_skill_fields",
			args: spec.RunScriptArgs{SkillName: "", SkillLocation: "x", ScriptLocation: "s.sh"},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "rejects_missing_script_location",
			args: spec.RunScriptArgs{SkillName: "a", SkillLocation: "rel", ScriptLocation: ""},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrInvalidArgument)
			},
		},
		{
			name: "unknown_handle",
			args: spec.RunScriptArgs{SkillName: "nope", SkillLocation: "x", ScriptLocation: "s.sh"},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrSkillNotFound)
			},
		},
		{
			name: "not_active",
			args: spec.RunScriptArgs{SkillName: "a", SkillLocation: "rel", ScriptLocation: "s.sh"},
			check: func(t *testing.T) {
				t.Helper()
				if _, err := s.toolUnload(t.Context(), spec.UnloadArgs{All: true}); err != nil {
					t.Fatalf("toolUnload(all): %v", err)
				}
			},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrSkillNotActive)
			},
		},
		{
			name: "provider_not_found",
			args: spec.RunScriptArgs{SkillName: "a", SkillLocation: "rel", ScriptLocation: "s.sh"},
			check: func(t *testing.T) {
				t.Helper()
				if _, err := s.toolLoad(t.Context(), spec.LoadArgs{Skills: []spec.SkillHandle{h}}); err != nil {
					t.Fatalf("toolLoad: %v", err)
				}
				s.providers = mapResolver{}
			},
			isErr: func(err error) bool {
				return errors.Is(err, spec.ErrProviderNotFound)
			},
		},
		{
			name: "success_args_env_workdir_and_canonical_key_passed",
			args: spec.RunScriptArgs{
				SkillName:      "a",
				SkillLocation:  "rel",
				ScriptLocation: "scripts/x.sh",
				Args:           []string{"1", "2"},
				Env:            map[string]string{"K": "V"},
				WorkDir:        "wd",
			},
			check: func(t *testing.T) {
				t.Helper()
				s.providers = mapResolver{"t": p}
				if _, err := s.toolLoad(t.Context(), spec.LoadArgs{Skills: []spec.SkillHandle{h}}); err != nil {
					t.Fatalf("toolLoad: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.check != nil {
				tc.check(t)
			}
			_, err := s.toolRunScript(t.Context(), tc.args)
			if tc.isErr != nil {
				if err == nil || !tc.isErr(err) {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("toolRunScript: %v", err)
			}
		})
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.runCalls == 0 {
		t.Fatalf("expected provider RunScript to be called")
	}
	if p.lastRunKey != k {
		t.Fatalf("expected canonical key passed to provider: got=%+v want=%+v", p.lastRunKey, k)
	}
	if p.lastRunPath != "scripts/x.sh" {
		t.Fatalf("unexpected lastRunPath: %q", p.lastRunPath)
	}
	if len(p.lastRunArgs) != 2 || p.lastRunArgs[0] != "1" || p.lastRunArgs[1] != "2" {
		t.Fatalf("unexpected lastRunArgs: %+v", p.lastRunArgs)
	}
	if p.lastRunEnv["K"] != "V" || len(p.lastRunEnv) != 1 {
		t.Fatalf("unexpected lastRunEnv: %+v", p.lastRunEnv)
	}
	if p.lastRunWD != "wd" {
		t.Fatalf("unexpected lastRunWD: %q", p.lastRunWD)
	}
}
