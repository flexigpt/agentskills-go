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

type userLocationError struct {
	input string
	err   error
}

func (e userLocationError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("invalid skill location %q", e.input)
	}
	return fmt.Sprintf("invalid skill location %q: %v", e.input, e.err)
}

func (e userLocationError) Unwrap() error { return e.err }

type Provider struct {
	runScriptsEnabled   bool
	execPolicy          exectool.ExecutionPolicy
	runScriptPolicy     exectool.RunScriptPolicy
	allowedScriptExt    []string
	allowedScriptExtSet bool
}

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
		// Normalize/clone now to avoid sharing maps/slices with caller.
		norm, err := exectool.NormalizeRunScriptPolicy(policy)
		if err != nil {
			return err
		}
		p.runScriptPolicy = norm
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
		p.allowedScriptExtSet = true

		return nil
	}
}

func New(opts ...Option) (*Provider, error) {
	p := &Provider{
		runScriptsEnabled: false,
		execPolicy:        exectool.DefaultExecutionPolicy(),
		runScriptPolicy:   exectool.DefaultRunScriptPolicy(),
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
	// Apply extension policy precedence:
	//  1) WithAllowedScriptExtensions(...) if explicitly set
	//  2) otherwise, keep caller-provided runScriptPolicy.AllowedExtensions if non-empty
	//  3) otherwise, apply provider defaults (OS-specific)
	if p.allowedScriptExtSet {
		p.runScriptPolicy.AllowedExtensions = append([]string(nil), p.allowedScriptExt...)
	} else if len(p.runScriptPolicy.AllowedExtensions) == 0 {
		if isWindows() {
			p.runScriptPolicy.AllowedExtensions = append([]string(nil), defaultAllowedScriptsExtensionWin...)
		} else {
			p.runScriptPolicy.AllowedExtensions = append([]string(nil), defaultAllowedScriptsExtensionNonWin...)
		}
	}

	// Harden: deep-clone + normalize tool policy so later external mutations can't race.
	norm, err := exectool.NormalizeRunScriptPolicy(p.runScriptPolicy)
	if err != nil {
		return nil, err
	}
	p.runScriptPolicy = norm
	return p, nil
}

func (p *Provider) Type() string { return Type }

func (p *Provider) Index(ctx context.Context, def spec.SkillDef) (spec.ProviderSkillIndexRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.ProviderSkillIndexRecord{}, err
	}
	if strings.TrimSpace(def.Type) == "" {
		return spec.ProviderSkillIndexRecord{}, fmt.Errorf("%w: def.type is required", spec.ErrInvalidArgument)
	}
	if def.Type != Type {
		return spec.ProviderSkillIndexRecord{}, fmt.Errorf(
			"%w: wrong provider type: %q",
			spec.ErrInvalidArgument,
			def.Type,
		)
	}
	if strings.TrimSpace(def.Name) == "" {
		return spec.ProviderSkillIndexRecord{}, fmt.Errorf("%w: def.name is required", spec.ErrInvalidArgument)
	}
	if strings.TrimSpace(def.Location) == "" {
		return spec.ProviderSkillIndexRecord{}, fmt.Errorf("%w: def.location is required", spec.ErrInvalidArgument)
	}

	root, err := canonicalRoot(def.Location)
	if err != nil {
		return spec.ProviderSkillIndexRecord{}, err
	}

	name, desc, props, digest, err := indexSkillDir(ctx, root)
	if err != nil {
		return spec.ProviderSkillIndexRecord{}, err
	}

	if name != def.Name {
		return spec.ProviderSkillIndexRecord{}, fmt.Errorf(
			"%w: key.name=%q does not match SKILL.md frontmatter.name=%q",
			spec.ErrInvalidArgument,

			def.Name,
			name,
		)
	}

	k := spec.ProviderSkillKey{Type: def.Type, Name: def.Name, Location: root} // canonical/internal
	return spec.ProviderSkillIndexRecord{
		Key:         k,
		Description: desc,
		Properties:  props,
		Digest:      digest,
	}, nil
}

func (p *Provider) LoadBody(ctx context.Context, key spec.ProviderSkillKey) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if key.Type != Type {
		return "", fmt.Errorf("%w: wrong provider type: %q", spec.ErrInvalidArgument, key.Type)
	}
	root, err := canonicalRoot(key.Location)
	if err != nil {
		return "", err
	}
	return loadSkillBody(ctx, root)
}

func (p *Provider) ReadResource(
	ctx context.Context,
	key spec.ProviderSkillKey,
	resourceLocation string,
	encoding spec.ReadResourceEncoding,
) ([]llmtoolsgoSpec.ToolOutputUnion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key.Type != Type {
		return nil, fmt.Errorf("%w: wrong provider type: %q", spec.ErrInvalidArgument, key.Type)
	}
	root, err := canonicalRoot(key.Location)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(resourceLocation) == "" {
		return nil, fmt.Errorf("%w: resource location is required", spec.ErrInvalidArgument)
	}

	enc := encoding
	if strings.TrimSpace(string(enc)) == "" {
		enc = spec.ReadResourceEncodingText
	}
	switch enc {
	case spec.ReadResourceEncodingText, spec.ReadResourceEncodingBinary:
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
	return ft.ReadFile(ctx, fstool.ReadFileArgs{Path: resourceLocation, Encoding: string(enc)})
}

// RunScript
//
// Security / hardening responsibilities:
// This provider is intentionally thin and delegates most hardening to llmtools-go tools.
//
// Provider responsibility (skill-specific):
//   - Ensure all filesystem access (read/execute) is scoped to the *skill root directory*.
//     We do this by configuring the underlying tool sandbox as:
//     allowedRoots = [skillRoot]
//     workBaseDir  = skillRoot
//
// Tool responsibility (generic hardening; enforced by llmtools-go/exectool runscript):
//   - Path resolution/normalization and sandbox enforcement against allowedRoots
//   - Refuse symlink traversal (workdir + script path + parent components)
//   - Validate env/args (format + defense-in-depth limits)
//   - Safe argv-to-shell quoting and interpreter selection by extension
//   - Timeouts/output caps and process-tree termination
//   - Dangerous command blocking + optional heuristics
//
// Note: By design, scripts may live anywhere under the skill root (ideally under "scripts/").
func (p *Provider) RunScript(
	ctx context.Context,
	key spec.ProviderSkillKey,
	scriptLocation string,
	args []string,
	env map[string]string,
	workdir string,
) (spec.RunScriptOut, error) {
	if err := ctx.Err(); err != nil {
		return spec.RunScriptOut{}, err
	}
	if key.Type != Type {
		return spec.RunScriptOut{}, fmt.Errorf("%w: wrong provider type: %q", spec.ErrInvalidArgument, key.Type)
	}
	if !p.runScriptsEnabled {
		return spec.RunScriptOut{}, spec.ErrRunScriptUnsupported
	}

	root, err := canonicalRoot(key.Location)
	if err != nil {
		return spec.RunScriptOut{}, err
	}

	sp := strings.TrimSpace(scriptLocation)
	if sp == "" {
		return spec.RunScriptOut{}, fmt.Errorf("%w: script location is required", spec.ErrInvalidArgument)
	}

	et, err := exectool.NewExecTool(
		exectool.WithAllowedRoots([]string{root}),
		exectool.WithWorkBaseDir(root),
		exectool.WithExecutionPolicy(p.execPolicy),
		exectool.WithRunScriptPolicy(p.runScriptPolicy),
	)
	if err != nil {
		return spec.RunScriptOut{}, err
	}

	res, err := et.RunScript(ctx, exectool.RunScriptArgs{
		Path:    sp,
		Args:    args,
		Env:     env,
		WorkDir: workdir,
	})
	if err != nil {
		return spec.RunScriptOut{}, err
	}
	if res == nil {
		return spec.RunScriptOut{}, errors.New("runscript returned nil result")
	}
	return spec.RunScriptOut{
		Location:   sp, // keep skill-relative path in receipt
		ExitCode:   res.ExitCode,
		Stdout:     res.Stdout,
		Stderr:     res.Stderr,
		TimedOut:   res.TimedOut,
		DurationMS: res.DurationMS,
	}, nil
}

func canonicalRoot(p string) (string, error) {
	orig := p
	root := strings.TrimSpace(p)
	if root == "" {
		return "", fmt.Errorf("%w: empty path", spec.ErrInvalidArgument)
	}
	if strings.ContainsRune(root, '\x00') {
		return "", fmt.Errorf("%w: path contains NUL byte", spec.ErrInvalidArgument)
	}

	clean := filepath.Clean(filepath.FromSlash(root))
	if isWindows() {
		// Reject drive-relative paths like "C:foo" (ambiguous).
		if vol := filepath.VolumeName(clean); vol != "" && !filepath.IsAbs(clean) {
			return "", fmt.Errorf(
				"%w: windows drive-relative paths like %q are not supported",
				spec.ErrInvalidArgument,
				root,
			)
		}
	}

	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("%w: %w", spec.ErrInvalidArgument, userLocationError{input: orig, err: err})
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil && strings.TrimSpace(resolved) != "" {
		abs = resolved
	}

	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%w: %w", spec.ErrInvalidArgument, userLocationError{input: orig, err: err})
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%w: not a directory: %q", spec.ErrInvalidArgument, orig)
	}
	return abs, nil
}

func isWindows() bool { return runtime.GOOS == "windows" }
