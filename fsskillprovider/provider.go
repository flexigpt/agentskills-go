package fsskillprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/flexigpt/agentskills-go"
	"github.com/flexigpt/llmtools-go/exectool"
	"github.com/flexigpt/llmtools-go/fstool"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/spec"
)

const (
	Type   = "fs"
	extPy  = ".py"
	extSh  = ".sh"
	extPs1 = ".ps1"

	goosWindows = "windows"
)

var (
	defaultAllowedScriptsExtensionWin    = []string{extPs1, extPy}
	defaultAllowedScriptsExtensionNonWin = []string{extSh, extPy}
)

const (
	skillFileName   = agentskills.SkillDocumentFileName
	maxSkillMDBytes = agentskills.MaxSkillDocumentBytes
)

type skillDirIndex struct {
	Name        string
	Description string
	DisplayName string

	Insert    spec.SkillInsert
	Arguments []spec.SkillArgument

	Tags []string

	Resources spec.SkillResourceInfo

	Props map[string]any

	Warnings []string

	Digest string
}

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

	meta, err := indexSkillDir(ctx, root)
	if err != nil {
		return spec.ProviderSkillIndexRecord{}, err
	}

	if meta.Name != def.Name {
		return spec.ProviderSkillIndexRecord{}, fmt.Errorf(
			"%w: key.name=%q does not match SKILL.md frontmatter.name=%q",
			spec.ErrInvalidArgument,

			def.Name,
			meta.Name,
		)
	}

	k := spec.ProviderSkillKey{Type: def.Type, Name: def.Name, Location: root} // canonical/internal
	return spec.ProviderSkillIndexRecord{
		Key:            k,
		Name:           meta.Name,
		Description:    meta.Description,
		DisplayName:    meta.DisplayName,
		Insert:         meta.Insert,
		Arguments:      meta.Arguments,
		Tags:           append([]string(nil), meta.Tags...),
		Resources:      meta.Resources,
		RawFrontmatter: meta.Props,
		Warnings:       meta.Warnings,
		Digest:         meta.Digest,
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

// indexSkillDir reads and validates SKILL.md frontmatter, returning metadata and digest.
// It does NOT return the body; body is loaded separately for progressive disclosure.
// Assumes canonical root passed. All validations already done.
func indexSkillDir(
	ctx context.Context,
	rootDir string,
) (skillDirIndex, error) {
	if err := ctx.Err(); err != nil {
		return skillDirIndex{}, fmt.Errorf("indexSkillDir: %w", err)
	}

	root := strings.TrimSpace(rootDir)
	if root == "" {
		return skillDirIndex{}, fmt.Errorf("%w: empty rootDir", spec.ErrInvalidArgument)
	}

	skillMDPath := filepath.Join(root, skillFileName)

	// Disallow SKILL.md being a symlink.
	if lst, lerr := os.Lstat(skillMDPath); lerr == nil {
		if lst.Mode()&os.ModeSymlink != 0 {
			return skillDirIndex{}, errors.New("SKILL.md must not be a symlink")
		}
		if !lst.Mode().IsRegular() {
			return skillDirIndex{}, errors.New("SKILL.md must be a regular file")
		}
	}

	b, sha, err := readAllLimitedAndDigest(skillMDPath)
	if err != nil {
		return skillDirIndex{}, fmt.Errorf("indexSkillDir: %w", err)
	}

	document, parseWarnings, err := agentskills.ParseSkillDocument(
		b,
		spec.ParseSkillDocumentOptions{
			ExpectedName: filepath.Base(root),
		},
	)
	if err != nil {
		return skillDirIndex{}, fmt.Errorf("indexSkillDir: %w", err)
	}

	resources, resourceWarnings, err := indexSkillResources(ctx, root)
	if err != nil {
		return skillDirIndex{}, fmt.Errorf("indexSkillDir: %w", err)
	}
	warnings := append(
		append([]string(nil), parseWarnings...),
		resourceWarnings...,
	)

	return skillDirIndex{
		Name:        document.Name,
		Description: document.Description,
		DisplayName: document.DisplayName,
		Insert:      document.Insert,
		Arguments:   append([]spec.SkillArgument(nil), document.Arguments...),
		Tags:        append([]string(nil), document.Tags...),
		Resources:   resources,
		Props:       document.RawFrontmatter,
		Warnings:    uniqueStrings(warnings),
		Digest:      "sha256:" + sha,
	}, nil
}

func loadSkillBody(ctx context.Context, rootDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("loadSkillBody: %w", err)
	}

	// Assumes canonical root passed. All validations already done.
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return "", fmt.Errorf("%w: empty rootDir", spec.ErrInvalidArgument)
	}

	skillMDPath := filepath.Join(root, skillFileName)

	// Match index hardening: disallow symlink / non-regular file (check before reading).
	if lst, lerr := os.Lstat(skillMDPath); lerr == nil {
		if lst.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("SKILL.md must not be a symlink")
		}
		if !lst.Mode().IsRegular() {
			return "", errors.New("SKILL.md must be a regular file")
		}
	}
	b, _, err := readAllLimitedAndDigest(skillMDPath)
	if err != nil {
		return "", fmt.Errorf("loadSkillBody: %w", err)
	}

	document, _, err := agentskills.ParseSkillDocument(
		b,
		spec.ParseSkillDocumentOptions{
			ExpectedName: filepath.Base(root),
		},
	)
	if err != nil {
		return "", fmt.Errorf("loadSkillBody: %w", err)
	}
	return document.MarkdownBody, nil
}

func indexSkillResources(
	ctx context.Context,
	rootDir string,
) (spec.SkillResourceInfo, []string, error) {
	if err := ctx.Err(); err != nil {
		return spec.SkillResourceInfo{}, nil, err
	}

	root := strings.TrimSpace(rootDir)
	if root == "" {
		return spec.SkillResourceInfo{}, nil, fmt.Errorf("%w: empty rootDir", spec.ErrInvalidArgument)
	}

	var info spec.SkillResourceInfo
	var warnings []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		rel := resourceRelLocation(root, path)
		if walkErr != nil {
			warnings = append(warnings, fmt.Sprintf("resource scan skipped %q: %v", rel, walkErr))
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d == nil {
			return nil
		}

		if rel == "." {
			return nil
		}
		if rel == skillFileName {
			return nil
		}

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		st, err := d.Info()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("resource scan skipped %q: %v", rel, err))
			return nil
		}
		if !st.Mode().IsRegular() {
			return nil
		}

		info.TotalCount++
		if len(info.Locations) < spec.MaxSkillResourceLocations {
			info.Locations = append(info.Locations, rel)
		}
		return nil
	})
	if err != nil {
		return spec.SkillResourceInfo{}, warnings, err
	}

	info.HasResources = info.TotalCount > 0
	info.MoreLocations = info.TotalCount > len(info.Locations)
	return info, uniqueStrings(warnings), nil
}

func resourceRelLocation(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.TrimSpace(rel) == "" {
		return "."
	}
	return filepath.ToSlash(rel)
}

func readAllLimitedAndDigest(path string) (data []byte, dataSHA string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	data, err = io.ReadAll(io.LimitReader(f, int64(maxSkillMDBytes)+1))
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxSkillMDBytes {
		return nil, "", fmt.Errorf("SKILL.md too large (max %d bytes)", maxSkillMDBytes)
	}

	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
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

func isWindows() bool { return runtime.GOOS == goosWindows }
