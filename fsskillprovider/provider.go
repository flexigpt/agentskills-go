package fsskillprovider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flexigpt/llmtools-go/fstool"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

const Type = "fs"

type Option func(*Provider) error

// WithRunScripts enables RunScript. Default is disabled.
func WithRunScripts(enabled bool) Option {
	return func(p *Provider) error {
		p.runScriptsEnabled = enabled
		return nil
	}
}

type Provider struct {
	runScriptsEnabled bool
}

func New(opts ...Option) (*Provider, error) {
	p := &Provider{
		runScriptsEnabled: false,
	}
	for _, o := range opts {
		if o == nil {
			continue
		}
		if err := o(p); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *Provider) Type() string { return Type }

func (p *Provider) Index(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	if strings.TrimSpace(key.Type) == "" {
		return spec.SkillRecord{}, fmt.Errorf("%w: key.type is required", spec.ErrInvalidArgument)
	}
	if key.Type != Type {
		return spec.SkillRecord{}, fmt.Errorf("%w: wrong provider type: %q", spec.ErrInvalidArgument, key.Type)
	}
	if strings.TrimSpace(key.Name) == "" {
		return spec.SkillRecord{}, fmt.Errorf("%w: key.name is required", spec.ErrInvalidArgument)
	}
	if strings.TrimSpace(key.Path) == "" {
		return spec.SkillRecord{}, fmt.Errorf("%w: key.path is required", spec.ErrInvalidArgument)
	}

	root, err := canonicalRoot(key.Path)
	if err != nil {
		return spec.SkillRecord{}, err
	}

	name, desc, props, digest, err := indexSkillDir(ctx, root)
	if err != nil {
		return spec.SkillRecord{}, err
	}

	if name != key.Name {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: key.name=%q does not match SKILL.md frontmatter.name=%q",
			spec.ErrInvalidArgument,
			key.Name,
			name,
		)
	}

	key.Path = root // normalize path in the returned record

	return spec.SkillRecord{
		Key:         key,
		Description: desc,
		Properties:  props,
		Digest:      digest,
	}, nil
}

func (p *Provider) LoadBody(ctx context.Context, key spec.SkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := canonicalRoot(key.Path)
	if err != nil {
		return "", err
	}
	return loadSkillBody(ctx, root)
}

func (p *Provider) ReadResource(
	ctx context.Context,
	key spec.SkillKey,
	resourcePath string,
	encoding spec.ReadEncoding,
) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := canonicalRoot(key.Path)
	if err != nil {
		return nil, err
	}

	rel := strings.TrimSpace(resourcePath)
	if rel == "" {
		return nil, fmt.Errorf("%w: resource path is required", spec.ErrInvalidArgument)
	}

	abs, err := joinUnderRoot(root, rel)
	if err != nil {
		return nil, err
	}

	enc := encoding
	if strings.TrimSpace(string(enc)) == "" {
		enc = spec.ReadEncodingText
	}
	switch enc {
	case spec.ReadEncodingText, spec.ReadEncodingBinary:
	default:
		return nil, fmt.Errorf("%w: unknown encoding: %q", spec.ErrInvalidArgument, enc)
	}

	return fstool.ReadFile(ctx, fstool.ReadFileArgs{
		Path:     abs,
		Encoding: string(enc),
	})
}

func (p *Provider) RunScript(
	ctx context.Context,
	key spec.SkillKey,
	scriptPath string,
	args []string,
	env map[string]string,
	workdir string,
) (spec.RunScriptResult, error) {
	if err := ctx.Err(); err != nil {
		return spec.RunScriptResult{}, err
	}
	if !p.runScriptsEnabled {
		return spec.RunScriptResult{}, spec.ErrRunScriptUnsupported
	}

	root, err := canonicalRoot(key.Path)
	if err != nil {
		return spec.RunScriptResult{}, err
	}

	sp := strings.TrimSpace(scriptPath)
	if sp == "" {
		return spec.RunScriptResult{}, fmt.Errorf("%w: script path is required", spec.ErrInvalidArgument)
	}

	// Provider-enforced constraint: script must be under scripts.
	if !relIsUnderDir(sp, "scripts") {
		return spec.RunScriptResult{}, fmt.Errorf(
			"%w: script path must be under scripts/: %q",
			spec.ErrInvalidArgument,
			sp,
		)
	}

	scriptAbs, err := joinUnderRoot(root, sp)
	if err != nil {
		return spec.RunScriptResult{}, err
	}

	wd := strings.TrimSpace(workdir)
	var workdirAbs string
	switch wd {
	case "", ".":
		workdirAbs = root
	default:
		workdirAbs, err = joinUnderRoot(root, wd)
		if err != nil {
			return spec.RunScriptResult{}, err
		}
	}

	cmdName, cmdArgs, err := buildExecCommand(scriptAbs, args)
	if err != nil {
		return spec.RunScriptResult{}, err
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	cmd.Dir = workdirAbs
	cmd.Env = mergeEnv(os.Environ(), env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	dur := time.Since(start)

	res := spec.RunScriptResult{
		Path:       sp,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMS: dur.Milliseconds(),
	}

	if runErr == nil {
		res.ExitCode = 0
		return res, nil
	}

	// Context timeout flag (best-effort).
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
	}

	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, nil
	}

	return spec.RunScriptResult{}, runErr
}

func canonicalRoot(p string) (string, error) {
	root := strings.TrimSpace(p)
	if root == "" {
		return "", fmt.Errorf("%w: empty path", spec.ErrInvalidArgument)
	}

	abs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil && strings.TrimSpace(resolved) != "" {
		abs = resolved
	}

	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%w: not a directory: %s", spec.ErrInvalidArgument, abs)
	}
	return abs, nil
}

func buildExecCommand(scriptAbs string, args []string) (name string, outArgs []string, err error) {
	ext := strings.ToLower(filepath.Ext(scriptAbs))

	if isWindows() {
		switch ext {
		case ".ps1":
			if _, e := exec.LookPath("pwsh"); e == nil {
				return "pwsh", append(
					[]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptAbs},
					args...), nil
			}
			if _, e := exec.LookPath("powershell"); e == nil {
				return "powershell", append(
					[]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptAbs},
					args...), nil
			}
			return "", nil, fmt.Errorf("%w: powershell not found to run %q", spec.ErrRunScriptUnsupported, scriptAbs)

		case ".bat", ".cmd":
			// "cmd" /C "scriptAbs" arg1 arg2 ...
			a := append([]string{"/C", scriptAbs}, args...)
			return "cmd", a, nil

		default:
			// Best-effort: execute directly (for .exe) else cmd /C.
			if ext == ".exe" {
				return scriptAbs, args, nil
			}
			return "cmd", append([]string{"/C", scriptAbs}, args...), nil
		}
	}

	switch ext {
	case ".sh":
		return "sh", append([]string{scriptAbs}, args...), nil
	default:
		// Run directly (requires executable bit/shebang if not a binary).
		return scriptAbs, args, nil
	}
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	// Preserve base, override by key (case-sensitive; matches Go/Unix behavior).
	out := make([]string, 0, len(base)+len(overrides))
	seen := map[string]struct{}{}

	for _, kv := range base {
		k := kv
		if before, _, ok := strings.Cut(kv, "="); ok {
			k = before
		}
		if v, ok := overrides[k]; ok {
			out = append(out, k+"="+v)
			seen[k] = struct{}{}
		} else {
			out = append(out, kv)
			seen[k] = struct{}{}
		}
	}

	for k, v := range overrides {
		if _, ok := seen[k]; ok {
			continue
		}
		out = append(out, k+"="+v)
	}

	return out
}
