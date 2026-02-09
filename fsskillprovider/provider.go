package fsskillprovider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/flexigpt/llmtools-go/exectool"
	"github.com/flexigpt/llmtools-go/fstool"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

const Type = "fs"

var (
	defaultAllowedScriptsExtensionWin    = []string{".ps1", ".py"}
	defaultAllowedScriptsExtensionNonWin = []string{".sh", ".py"}
)

type Option func(*Provider) error

// WithRunScripts enables RunScript. Default is disabled.
func WithRunScripts(enabled bool) Option {
	return func(p *Provider) error {
		p.runScriptsEnabled = enabled
		return nil
	}
}

// WithExecutionPolicy configures the exectool execution policy (timeouts/output caps/etc).
func WithExecutionPolicy(policy exectool.ExecutionPolicy) Option {
	return func(p *Provider) error {
		p.execPolicy = policy
		return nil
	}
}

// WithRunScriptPolicy configures exectool's RunScriptPolicy.
// NOTE: This is a *tool policy*. The provider does not implement hardening itself; it delegates
// to llmtools-go/exectool. This option just configures that tool.
func WithRunScriptPolicy(policy exectool.RunScriptPolicy) Option {
	return func(p *Provider) error {
		p.runScriptPolicy = policy
		return nil
	}
}

// WithAllowedScriptExtensions restricts which script extensions may be executed.
// This configures exectool.RunScriptPolicy.AllowedExtensions.
// Defaults:
//   - Windows: [".ps1", ".py"]
//   - non-Windows: [".sh", ".py"]
func WithAllowedScriptExtensions(exts []string) Option {
	return func(p *Provider) error {
		out := make([]string, 0, len(exts))
		for _, e := range exts {
			e = strings.ToLower(strings.TrimSpace(e))
			if e == "" {
				continue
			}
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}

			out = append(out, e)
		}

		p.allowedScriptExt = out
		return nil
	}
}

type Provider struct {
	runScriptsEnabled bool
	execPolicy        exectool.ExecutionPolicy
	runScriptPolicy   exectool.RunScriptPolicy
	allowedScriptExt  []string
}

func New(opts ...Option) (*Provider, error) {
	p := &Provider{
		runScriptsEnabled: false,
		execPolicy:        exectool.DefaultExecutionPolicy,
		runScriptPolicy:   exectool.DefaultRunScriptPolicy,
		allowedScriptExt:  nil,
	}
	for _, o := range opts {
		if o == nil {
			continue
		}
		if err := o(p); err != nil {
			return nil, err
		}
	}
	// Defaults if unset.
	if len(p.allowedScriptExt) == 0 {
		if isWindows() {
			p.allowedScriptExt = defaultAllowedScriptsExtensionWin
		} else {
			p.allowedScriptExt = defaultAllowedScriptsExtensionNonWin
		}
	}
	// Apply default/option-based extension restrictions to the tool policy.
	p.runScriptPolicy.AllowedExtensions = append([]string(nil), p.allowedScriptExt...)
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

	if strings.TrimSpace(resourcePath) == "" {
		return nil, fmt.Errorf("%w: resource path is required", spec.ErrInvalidArgument)
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

	// Hardening is delegated to llmtools-go/fstool (readfile):
	//   - path normalization and allowedRoots sandbox checks
	//   - refuse symlink traversal (file and parent dirs)
	//   - file size cap + MIME/text/binary handling (+ safe PDF text extraction)
	//
	// Provider responsibility: scope reads to the skill root dir.
	ft, err := fstool.NewFSTool(
		fstool.WithAllowedRoots([]string{root}),
		fstool.WithWorkBaseDir(root),
	)
	if err != nil {
		return nil, err
	}
	return ft.ReadFile(ctx, fstool.ReadFileArgs{Path: resourcePath, Encoding: string(enc)})
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
	// Hardening is delegated to llmtools-go/exectool (runscript):
	//   - path normalization + allowedRoots sandbox checks
	//   - refuse symlink traversal in workdir/script path
	//   - env + args validation/limits
	//   - interpreter selection by extension + safe quoting
	//   - timeouts/output caps + process-tree termination
	//   - dangerous command blocklist + optional heuristics
	//
	// Provider responsibility: scope execution to the skill root dir via allowedRoots=[root].
	et, err := exectool.NewExecTool(
		exectool.WithAllowedRoots([]string{root}),
		exectool.WithWorkBaseDir(root),
		exectool.WithExecutionPolicy(p.execPolicy),
		exectool.WithRunScriptPolicy(p.runScriptPolicy),
	)
	if err != nil {
		return spec.RunScriptResult{}, err
	}

	res, err := et.RunScript(ctx, exectool.RunScriptArgs{
		Path:    sp,
		Args:    args,
		Env:     env,
		Workdir: workdir,
	})
	if err != nil {
		return spec.RunScriptResult{}, err
	}
	if res == nil {
		return spec.RunScriptResult{}, errors.New("runscript returned nil result")
	}
	return spec.RunScriptResult{
		Path:       sp, // keep skill-relative path in receipt
		ExitCode:   res.ExitCode,
		Stdout:     res.Stdout,
		Stderr:     res.Stderr,
		TimedOut:   res.TimedOut,
		DurationMS: res.DurationMS,
	}, nil
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

func isWindows() bool { return runtime.GOOS == "windows" }
