package fsskillprovider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flexigpt/llmtools-go/fstool"
	"github.com/flexigpt/llmtools-go/shelltool"
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

// WithRunScriptPolicy configures the shelltool policy used for RunScript.
func WithRunScriptPolicy(policy shelltool.ShellCommandPolicy) Option {
	return func(p *Provider) error {
		p.runScriptPolicy = policy
		return nil
	}
}

type Provider struct {
	runScriptsEnabled bool
	runScriptPolicy   shelltool.ShellCommandPolicy
}

func New(opts ...Option) (*Provider, error) {
	p := &Provider{
		runScriptsEnabled: false,
		runScriptPolicy:   shelltool.DefaultShellCommandPolicy,
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
	if key.Type != Type {
		return "", fmt.Errorf("%w: wrong provider type: %q", spec.ErrInvalidArgument, key.Type)
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
	if key.Type != Type {
		return nil, fmt.Errorf("%w: wrong provider type: %q", spec.ErrInvalidArgument, key.Type)
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
	if key.Type != Type {
		return spec.RunScriptResult{}, fmt.Errorf("%w: wrong provider type: %q", spec.ErrInvalidArgument, key.Type)
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

	// Resolve script path with symlink-hardening: resolved path must remain under root.
	scriptAbs, err := resolveExistingPathUnderRoot(root, sp)
	if err != nil {
		return spec.RunScriptResult{}, err
	}
	// Disallow the script file itself being a symlink.
	if lst, lerr := os.Lstat(filepath.Join(root, filepath.Clean(sp))); lerr == nil {
		if lst.Mode()&os.ModeSymlink != 0 {
			return spec.RunScriptResult{}, fmt.Errorf(
				"%w: script must not be a symlink: %q",
				spec.ErrInvalidArgument,
				sp,
			)
		}
	}

	wd := strings.TrimSpace(workdir)
	var workdirAbs string
	switch wd {
	case "", ".":
		workdirAbs = root
	default:
		workdirAbs, err = resolveExistingDirUnderRoot(root, wd)
		if err != nil {
			return spec.RunScriptResult{}, err
		}
	}
	shellName, cmdStr := buildScriptCommand(scriptAbs, args)

	st, err := shelltool.NewShellTool(
		shelltool.WithShellAllowedWorkdirRoots([]string{root}),
		shelltool.WithShellCommandPolicy(p.runScriptPolicy),
		// We don't need shelltool sessions for this use-case.
		shelltool.WithShellMaxSessions(8),
		shelltool.WithShellSessionTTL(30*60*1e9), // 30m; harmless even if unused
	)
	if err != nil {
		return spec.RunScriptResult{}, err
	}

	resp, err := st.Run(ctx, shelltool.ShellCommandArgs{
		Commands:  []string{cmdStr},
		Workdir:   workdirAbs,
		Env:       env,
		Shell:     shellName,
		SessionID: "",
	})
	if err != nil {
		return spec.RunScriptResult{}, err
	}

	out := spec.RunScriptResult{Path: sp}
	if resp != nil && len(resp.Results) > 0 {
		r0 := resp.Results[0]
		out.ExitCode = r0.ExitCode
		out.Stdout = r0.Stdout
		out.Stderr = r0.Stderr
		out.TimedOut = r0.TimedOut
		out.DurationMS = r0.DurationMS
	}
	return out, nil
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

// resolveExistingPathUnderRoot resolves rel under root, follows symlinks, and ensures the final
// resolved path remains within root.
func resolveExistingPathUnderRoot(root, rel string) (string, error) {
	cand, err := joinUnderRoot(root, rel)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(cand)
	if err != nil {
		return "", err
	}
	ok, err := withinRoot(root, resolved)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: path escapes root via symlink: %q", spec.ErrInvalidArgument, rel)
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("%w: expected file, got directory: %q", spec.ErrInvalidArgument, rel)
	}
	return resolved, nil
}

func resolveExistingDirUnderRoot(root, rel string) (string, error) {
	cand, err := joinUnderRoot(root, rel)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(cand)
	if err != nil {
		return "", err
	}
	ok, err := withinRoot(root, resolved)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: path escapes root via symlink: %q", spec.ErrInvalidArgument, rel)
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%w: expected directory: %q", spec.ErrInvalidArgument, rel)
	}
	return resolved, nil
}

// buildScriptCommand chooses a shell and builds a cross-platform command string for shelltool.
func buildScriptCommand(scriptAbs string, args []string) (shellName shelltool.ShellName, invoke string) {
	ext := strings.ToLower(filepath.Ext(scriptAbs))

	if isWindows() {
		// Prefer pwsh, then powershell, then cmd.
		if ext == ".ps1" {
			if _, err := exec.LookPath("pwsh"); err == nil {
				return shelltool.ShellNamePwsh, psInvoke(scriptAbs, args)
			}
			return shelltool.ShellNamePowershell, psInvoke(scriptAbs, args)
		}
		// "".bat/.cmd" (or fallback): cmd call.
		return shelltool.ShellNameCmd, cmdInvoke(scriptAbs, args)
	}

	if ext == ".sh" {
		return shelltool.ShellNameSh, posixInvokeWithInterpreter("sh", scriptAbs, args)
	}
	return shelltool.ShellNameSh, posixInvoke(scriptAbs, args)
}

func posixInvoke(program string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, posixQuote(program))
	for _, a := range args {
		parts = append(parts, posixQuote(a))
	}
	return strings.Join(parts, " ")
}

func posixInvokeWithInterpreter(interpreter, scriptAbs string, args []string) string {
	parts := make([]string, 0, 2+len(args))
	parts = append(parts, posixQuote(interpreter), posixQuote(scriptAbs))
	for _, a := range args {
		parts = append(parts, posixQuote(a))
	}
	return strings.Join(parts, " ")
}

func psInvoke(scriptAbs string, args []string) string {
	parts := make([]string, 0, 2+len(args))
	parts = append(parts, "&", psQuote(scriptAbs))
	for _, a := range args {
		parts = append(parts, psQuote(a))
	}
	return strings.Join(parts, " ")
}

func cmdInvoke(scriptAbs string, args []string) string {
	parts := make([]string, 0, 2+len(args))
	parts = append(parts, "call", cmdQuote(scriptAbs))
	for _, a := range args {
		parts = append(parts, cmdQuote(a))
	}
	return strings.Join(parts, " ")
}

func cmdQuote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t") || strings.ContainsRune(s, '"') {
		s = strings.ReplaceAll(s, `"`, `""`)
		return `"` + s + `"`
	}
	return s
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func posixQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsRune(s, '\'') {
		return "'" + s + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
