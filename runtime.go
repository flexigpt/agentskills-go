package agentskills

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flexigpt/llmtools-go"
	"github.com/flexigpt/llmtools-go/fstool"
	"github.com/flexigpt/llmtools-go/shelltool"
	llmtoolsgoSpec "github.com/flexigpt/llmtools-go/spec"

	"github.com/flexigpt/agentskills-go/skilltool"
	"github.com/flexigpt/agentskills-go/spec"

	"github.com/flexigpt/agentskills-go/internal/pathutil"
	"github.com/flexigpt/agentskills-go/internal/promptxml"
	"github.com/flexigpt/agentskills-go/internal/sessionstore"
	"github.com/flexigpt/agentskills-go/internal/skill"
)

// Sanity: avoid unused imports in case we don't use XMLStruct helper.
var _ = xml.MarshalIndent

type skillEntry struct {
	mu  sync.RWMutex
	rec spec.SkillRecord
}

type Runtime struct {
	mu     sync.RWMutex
	logger *slog.Logger

	// Registry (name -> entry).
	skills map[string]*skillEntry

	// Sessions (internal state store).
	sessions *sessionstore.Store

	maxActivePerSession int
	shellEnabled        bool
	shellPolicy         shelltool.ShellCommandPolicy
}

type Option func(*Runtime) error

func WithLogger(l *slog.Logger) Option {
	return func(r *Runtime) error {
		r.logger = l
		return nil
	}
}

func WithMaxActivePerSession(n int) Option {
	return func(r *Runtime) error {
		r.maxActivePerSession = n
		return nil
	}
}

func WithSessionTTL(ttl time.Duration) Option {
	return func(r *Runtime) error {
		r.sessions.SetTTL(ttl)
		return nil
	}
}

func WithMaxSessions(maxSessions int) Option {
	return func(r *Runtime) error {
		r.sessions.SetMaxSessions(maxSessions)
		return nil
	}
}

// WithShell enables skills.run_script using llmtools-go shelltool, with the given policy.
func WithShell(policy shelltool.ShellCommandPolicy) Option {
	return func(r *Runtime) error {
		r.shellEnabled = true
		r.shellPolicy = policy
		return nil
	}
}

type Session struct {
	rt *Runtime
	id spec.SessionID
}

func (s *Session) ID() spec.SessionID { return s.id }

// Tools returns the skills tool specs (skills.load/unload/read/run_script).
func (s *Session) Tools() []llmtoolsgoSpec.Tool { return skilltool.Tools() }

// RegisterTools registers skills tools into an existing llmtools-go Registry.
func (s *Session) RegisterTools(reg *llmtools.Registry) error {
	if s == nil || s.rt == nil {
		return errors.New("nil session runtime")
	}
	return skilltool.Register(reg, s.rt, s.id)
}

// NewToolsRegistry returns a new llmtools-go Registry containing only the skills tools.
func (s *Session) NewToolsRegistry(opts ...llmtools.RegistryOption) (*llmtools.Registry, error) {
	if s == nil || s.rt == nil {
		return nil, errors.New("nil session runtime")
	}
	return skilltool.NewSkillsRegistry(s.rt, s.id, opts...)
}

// NewToolsBuiltinRegistry returns a new llmtools-go Registry containing builtins + skills tools.
func (s *Session) NewToolsBuiltinRegistry(opts ...llmtools.RegistryOption) (*llmtools.Registry, error) {
	if s == nil || s.rt == nil {
		return nil, errors.New("nil session runtime")
	}
	return skilltool.NewSkillsBuiltinRegistry(s.rt, s.id, opts...)
}

func New(opts ...Option) (*Runtime, error) {
	rt := &Runtime{
		logger:              slog.Default(),
		skills:              map[string]*skillEntry{},
		sessions:            sessionstore.New(),
		maxActivePerSession: 8, // sensible default (can override)
		shellEnabled:        false,
		shellPolicy:         shelltool.DefaultShellCommandPolicy,
	}
	for _, o := range opts {
		if o == nil {
			continue
		}
		if err := o(rt); err != nil {
			return nil, err
		}
	}
	if rt.logger == nil {
		rt.logger = slog.Default()
	}
	return rt, nil
}

func (r *Runtime) NewSession(ctx context.Context) (spec.SessionID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s := r.sessions.NewSession()
	return spec.SessionID(s.ID), nil
}

func (r *Runtime) CloseSession(ctx context.Context, id spec.SessionID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(string(id)) == "" {
		return nil
	}
	r.sessions.Delete(string(id))
	return nil
}

// Session returns a convenience wrapper bound to a session ID,
// including tool registration via package /tools.
func (r *Runtime) Session(id spec.SessionID) *Session {
	return &Session{rt: r, id: id}
}

// AvailableSkillsPromptXML builds <available_skills> XML for system prompts.
// If includeLocation=false, location elements are omitted (tool-only agents).
func (r *Runtime) AvailableSkillsPromptXML(includeLocation bool) (string, error) {
	skills := r.ListSkills()
	return promptxml.AvailableSkillsXML(skills, includeLocation)
}

// ActiveSkillsPromptXML builds <active_skills> XML containing active SKILL.md bodies in load order.
func (r *Runtime) ActiveSkillsPromptXML(ctx context.Context, sessionID spec.SessionID) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	sess, err := r.mustGetSession(sessionID)
	if err != nil {
		return "", err
	}

	sess.Mu.Lock()
	active := append([]string(nil), sess.ActiveSkills...)
	sess.Mu.Unlock()

	records := make([]spec.SkillRecord, 0, len(active))
	for _, name := range active {
		rec, err := r.ensureBodyLoaded(ctx, name)
		if err != nil {
			return "", err
		}
		records = append(records, rec)
	}
	return promptxml.ActiveSkillsXML(records)
}

// AvailableSkillsXMLStruct - if you want to embed XML structs directly.
func (r *Runtime) AvailableSkillsXMLStruct(includeLocation bool) (any, error) {
	skills := r.ListSkills()
	return promptxml.AvailableSkillsStruct(skills, includeLocation), nil
}

func (r *Runtime) AddSkillDir(ctx context.Context, dir string) (spec.SkillRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	rec, err := skill.IndexSkillDir(ctx, dir)
	if err != nil {
		return spec.SkillRecord{}, errors.Join(spec.ErrInvalidSkillDir, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.skills[rec.Name]; ok {
		return spec.SkillRecord{}, spec.ErrSkillAlreadyExists
	}
	r.skills[rec.Name] = &skillEntry{rec: rec}
	return rec, nil
}

func (r *Runtime) RemoveSkill(name string) (spec.SkillRecord, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return spec.SkillRecord{}, spec.ErrSkillNotFound
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.skills[n]
	if !ok {
		return spec.SkillRecord{}, spec.ErrSkillNotFound
	}
	delete(r.skills, n)

	e.mu.RLock()
	rec := e.rec
	e.mu.RUnlock()
	return rec, nil
}

func (r *Runtime) ListSkills() []spec.SkillRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]spec.SkillRecord, 0, len(r.skills))
	for _, e := range r.skills {
		e.mu.RLock()
		out = append(out, e.rec)
		e.mu.RUnlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Load implements skills.load behavior.
// Default mode is "replace" (per spec).
func (r *Runtime) Load(
	ctx context.Context,
	sessionID spec.SessionID,
	args spec.LoadArgs,
) (spec.LoadResult, error) {
	if err := ctx.Err(); err != nil {
		return spec.LoadResult{}, err
	}

	mode := args.Mode
	if strings.TrimSpace(string(mode)) == "" {
		mode = spec.LoadModeReplace
	}
	if mode != spec.LoadModeReplace && mode != spec.LoadModeAdd {
		return spec.LoadResult{}, errors.New("mode must be 'replace' or 'add'")
	}
	if len(args.Names) == 0 {
		return spec.LoadResult{}, errors.New("names is required")
	}

	req := make([]string, 0, len(args.Names))
	seen := map[string]struct{}{}
	for _, n := range args.Names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		req = append(req, n)
	}
	if len(req) == 0 {
		return spec.LoadResult{}, errors.New("names is required")
	}

	r.mu.RLock()
	for _, n := range req {
		if _, ok := r.skills[n]; !ok {
			r.mu.RUnlock()
			return spec.LoadResult{}, errors.Join(spec.ErrSkillNotFound, fmt.Errorf("unknown skill: %s", n))
		}
	}
	r.mu.RUnlock()

	sess, err := r.mustGetSession(sessionID)
	if err != nil {
		return spec.LoadResult{}, err
	}

	sess.Mu.Lock()
	defer sess.Mu.Unlock()

	var next []string
	switch mode {
	case spec.LoadModeReplace:
		next = append([]string(nil), req...)
	case spec.LoadModeAdd:
		reqSet := map[string]struct{}{}
		for _, n := range req {
			reqSet[n] = struct{}{}
		}
		keep := make([]string, 0, len(sess.ActiveSkills))
		for _, n := range sess.ActiveSkills {
			if _, isReq := reqSet[n]; !isReq {
				keep = append(keep, n)
			}
		}
		keep = append(keep, req...)
		next = slices.Clone(keep)
	}

	if r.maxActivePerSession > 0 && len(next) > r.maxActivePerSession {
		return spec.LoadResult{}, fmt.Errorf("too many active skills (%d > %d)", len(next), r.maxActivePerSession)
	}

	// Cache bodies for prompt injection (progressive disclosure).
	for _, name := range next {
		if _, err := r.ensureBodyLoaded(ctx, name); err != nil {
			return spec.LoadResult{}, err
		}
	}

	sess.ActiveSkills = next

	refs := make([]spec.SkillRef, 0, len(next))
	for _, name := range next {
		r.mu.RLock()
		e := r.skills[name]
		r.mu.RUnlock()

		e.mu.RLock()
		rec := e.rec
		e.mu.RUnlock()

		refs = append(refs, spec.SkillRef{
			Name:       rec.Name,
			Location:   rec.Location,
			RootDir:    rec.RootDir,
			Digest:     rec.Digest,
			Properties: rec.Properties,
		})
	}

	return spec.LoadResult{ActiveSkills: refs}, nil
}

func (r *Runtime) Unload(
	ctx context.Context,
	sessionID spec.SessionID,
	args spec.UnloadArgs,
) (spec.UnloadResult, error) {
	if err := ctx.Err(); err != nil {
		return spec.UnloadResult{}, err
	}
	if !args.All && len(args.Names) == 0 {
		return spec.UnloadResult{}, errors.New("names is required unless all=true")
	}

	sess, err := r.mustGetSession(sessionID)
	if err != nil {
		return spec.UnloadResult{}, err
	}

	sess.Mu.Lock()
	defer sess.Mu.Unlock()

	if args.All {
		sess.ActiveSkills = nil
		return spec.UnloadResult{ActiveSkills: nil}, nil
	}

	rm := map[string]struct{}{}
	for _, n := range args.Names {
		n = strings.TrimSpace(n)
		if n != "" {
			rm[n] = struct{}{}
		}
	}

	next := make([]string, 0, len(sess.ActiveSkills))
	for _, n := range sess.ActiveSkills {
		if _, ok := rm[n]; !ok {
			next = append(next, n)
		}
	}
	sess.ActiveSkills = next

	refs := make([]spec.SkillRef, 0, len(next))
	for _, name := range next {
		r.mu.RLock()
		e := r.skills[name]
		r.mu.RUnlock()
		if e == nil {
			continue
		}
		e.mu.RLock()
		rec := e.rec
		e.mu.RUnlock()

		refs = append(refs, spec.SkillRef{
			Name:       rec.Name,
			Location:   rec.Location,
			RootDir:    rec.RootDir,
			Digest:     rec.Digest,
			Properties: rec.Properties,
		})
	}

	return spec.UnloadResult{ActiveSkills: refs}, nil
}

func (r *Runtime) Read(
	ctx context.Context,
	sessionID spec.SessionID,
	args spec.ReadArgs,
) ([]llmtoolsgoSpec.ToolStoreOutputUnion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return nil, errors.New("path is required")
	}

	sess, err := r.mustGetSession(sessionID)
	if err != nil {
		return nil, err
	}

	sess.Mu.Lock()
	active := append([]string(nil), sess.ActiveSkills...)
	sess.Mu.Unlock()

	if len(active) == 0 {
		return nil, spec.ErrNoActiveSkills
	}

	target := strings.TrimSpace(args.Skill)
	if target == "" {
		target = active[len(active)-1]
	} else if !contains(active, target) {
		// Enforce progressive disclosure: can only read from active skills.
		return nil, spec.ErrNoActiveSkills
	}

	r.mu.RLock()
	e := r.skills[target]
	r.mu.RUnlock()
	if e == nil {
		return nil, spec.ErrSkillNotFound
	}

	e.mu.RLock()
	root := e.rec.RootDir
	e.mu.RUnlock()

	abs, err := pathutil.JoinUnderRoot(root, args.Path)
	if err != nil {
		return nil, err
	}

	enc := strings.TrimSpace(string(args.Encoding))
	if enc == "" {
		enc = string(spec.ReadEncodingText)
	}

	return fstool.ReadFile(ctx, fstool.ReadFileArgs{
		Path:     abs,
		Encoding: enc,
	})
}

func (r *Runtime) RunScript(
	ctx context.Context,
	sessionID spec.SessionID,
	args spec.RunScriptArgs,
) (spec.RunScriptResult, error) {
	if err := ctx.Err(); err != nil {
		return spec.RunScriptResult{}, err
	}
	if !r.shellEnabled {
		return spec.RunScriptResult{}, spec.ErrRunScriptUnsupported
	}
	if strings.TrimSpace(args.Path) == "" {
		return spec.RunScriptResult{}, errors.New("path is required")
	}

	sess, err := r.mustGetSession(sessionID)
	if err != nil {
		return spec.RunScriptResult{}, err
	}

	sess.Mu.Lock()
	active := append([]string(nil), sess.ActiveSkills...)
	sess.Mu.Unlock()

	if len(active) == 0 {
		return spec.RunScriptResult{}, spec.ErrNoActiveSkills
	}

	target := strings.TrimSpace(args.Skill)
	if target == "" {
		target = active[len(active)-1]
	} else if !contains(active, target) {
		return spec.RunScriptResult{}, spec.ErrNoActiveSkills
	}

	r.mu.RLock()
	e := r.skills[target]
	r.mu.RUnlock()
	if e == nil {
		return spec.RunScriptResult{}, spec.ErrSkillNotFound
	}

	e.mu.RLock()
	root := e.rec.RootDir
	e.mu.RUnlock()

	// Enforce scripts/ constraint (relative path must be under scripts/).
	if !pathutil.RelIsUnderDir(args.Path, "scripts") {
		return spec.RunScriptResult{}, fmt.Errorf("script path must be under scripts/: %s", args.Path)
	}

	scriptAbs, err := pathutil.JoinUnderRoot(root, args.Path)
	if err != nil {
		return spec.RunScriptResult{}, err
	}

	workdirRel := strings.TrimSpace(args.Workdir)
	if workdirRel == "" {
		workdirRel = "."
	}
	workdirAbs, err := pathutil.JoinUnderRoot(root, workdirRel)
	if err != nil {
		return spec.RunScriptResult{}, err
	}

	// Per session+skill shell binding (tool instance + shell session ID).
	b := sess.ShellBindingForSkill(target)
	if b.Tool == nil {
		st, err := shelltool.NewShellTool(
			shelltool.WithShellAllowedWorkdirRoots([]string{root}),
			shelltool.WithShellCommandPolicy(r.shellPolicy),
			// Keep shelltool session store small; we only need one per skill binding.
			shelltool.WithShellMaxSessions(8),
			shelltool.WithShellSessionTTL(30*time.Minute),
		)
		if err != nil {
			return spec.RunScriptResult{}, err
		}
		b.Tool = st
	}

	shellName, cmd := buildScriptCommand(scriptAbs, args.Args)

	resp, err := b.Tool.Run(ctx, shelltool.ShellCommandArgs{
		Commands:  []string{cmd},
		Workdir:   workdirAbs,
		Env:       args.Env,
		Shell:     shellName,
		SessionID: b.ShellSessionID,
	})
	if err != nil {
		return spec.RunScriptResult{}, err
	}
	if resp != nil && strings.TrimSpace(resp.SessionID) != "" {
		b.ShellSessionID = resp.SessionID
	}

	// Convert to spec output (single-command).
	out := spec.RunScriptResult{
		Path: args.Path,
	}
	if resp != nil && len(resp.Results) > 0 {
		out.ExitCode = resp.Results[0].ExitCode
		out.Stdout = resp.Results[0].Stdout
		out.Stderr = resp.Results[0].Stderr
		out.TimedOut = resp.Results[0].TimedOut
		out.DurationMS = resp.Results[0].DurationMS
	}
	return out, nil
}

// Tools exposure (wrapper over /tools).
func (r *Runtime) Tools() []llmtoolsgoSpec.Tool { return skilltool.Tools() }

func (r *Runtime) ensureBodyLoaded(ctx context.Context, name string) (spec.SkillRecord, error) {
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}

	r.mu.RLock()
	e := r.skills[name]
	r.mu.RUnlock()
	if e == nil {
		return spec.SkillRecord{}, spec.ErrSkillNotFound
	}

	e.mu.RLock()
	rec := e.rec
	hasBody := strings.TrimSpace(rec.SkillMDBody) != ""
	loc := rec.Location
	e.mu.RUnlock()

	if hasBody {
		return rec, nil
	}

	body, err := skill.LoadSkillBody(ctx, loc)
	if err != nil {
		return spec.SkillRecord{}, err
	}

	e.mu.Lock()
	// If another goroutine loaded first, keep it.
	if strings.TrimSpace(e.rec.SkillMDBody) == "" {
		e.rec.SkillMDBody = body
	}
	rec = e.rec
	e.mu.Unlock()

	return rec, nil
}

func (r *Runtime) mustGetSession(id spec.SessionID) (*sessionstore.Session, error) {
	sid := strings.TrimSpace(string(id))
	if sid == "" {
		return nil, spec.ErrSessionNotFound
	}
	s, ok := r.sessions.Get(sid)
	if !ok {
		return nil, spec.ErrSessionNotFound
	}
	return s, nil
}

func contains(list []string, v string) bool {
	return slices.Contains(list, v)
}

// buildScriptCommand chooses a shell and builds a command string that works cross-platform.
func buildScriptCommand(scriptAbs string, args []string) (shellName shelltool.ShellName, commandString string) {
	ext := strings.ToLower(filepath.Ext(scriptAbs))

	// Windows: prefer pwsh/powershell if available.
	if pathutil.IsWindows() {
		if _, err := exec.LookPath("pwsh"); err == nil {
			return shelltool.ShellNamePwsh, pathutil.PowerShellInvoke(scriptAbs, args)
		}
		if _, err := exec.LookPath("powershell"); err == nil {
			// Shelltool treats "powershell" as either pwsh or powershell; we force powershell syntax anyway.
			return shelltool.ShellNamePowershell, pathutil.PowerShellInvoke(scriptAbs, args)
		}
		// Fallback to cmd: best-effort for .bat/.cmd.
		if ext == ".bat" || ext == ".cmd" {
			return shelltool.ShellNameCmd, pathutil.CmdInvoke(scriptAbs, args)
		}
		// If no PowerShell, and not a cmd script, still try cmd direct.
		return shelltool.ShellNameCmd, pathutil.CmdInvoke(scriptAbs, args)
	}

	// POSIX:.
	switch ext {
	case ".sh":
		return shelltool.ShellNameSh, pathutil.POSIXInvokeWithInterpreter("sh", scriptAbs, args)
	default:
		// Run directly; relies on executable bit/shebang.
		return shelltool.ShellNameSh, pathutil.POSIXInvoke(scriptAbs, args)
	}
}
