package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flexigpt/llmtools-go"

	"github.com/flexigpt/agentskills-go/provider"
	"github.com/flexigpt/agentskills-go/runtime/internal/catalog"
	"github.com/flexigpt/agentskills-go/runtime/internal/session"
	"github.com/flexigpt/agentskills-go/runtime/spec"
)

type Runtime struct {
	// Immutable after New().
	providers map[string]provider.SkillProvider

	catalog  *catalog.Catalog
	sessions *session.Store

	supportsRunScript bool
}

type runtimeOptions struct {
	providers []provider.SkillProvider

	maxActivePerSession int
	sessionTTL          time.Duration
	maxSessions         int
}

type Option func(*runtimeOptions) error

func WithProvider(p provider.SkillProvider) Option {
	return func(o *runtimeOptions) error {
		o.providers = append(o.providers, p)
		return nil
	}
}

func WithMaxActivePerSession(n int) Option {
	return func(o *runtimeOptions) error {
		if n <= 0 {
			return errors.New("max active per session must be positive")
		}
		o.maxActivePerSession = n
		return nil
	}
}

func WithSessionTTL(ttl time.Duration) Option {
	return func(o *runtimeOptions) error {
		if ttl <= 0 {
			return errors.New("session TTL must be positive")
		}
		o.sessionTTL = ttl
		return nil
	}
}

func WithMaxSessions(maxSessions int) Option {
	return func(o *runtimeOptions) error {
		o.maxSessions = maxSessions
		return nil
	}
}

func anyProviderSupportsRunScript(values map[string]provider.SkillProvider) bool {
	for _, value := range values {
		if value.SupportsRunScript() {
			return true
		}
	}
	return false
}

type providerResolver struct {
	m map[string]provider.SkillProvider
}

func (r providerResolver) Provider(skillType string) (provider.SkillProvider, bool) {
	p, ok := r.m[skillType]
	return p, ok
}

func New(opts ...Option) (*Runtime, error) {
	cfg := runtimeOptions{
		maxActivePerSession: 8,
		sessionTTL:          24 * time.Hour,
		maxSessions:         4096,
	}

	for _, o := range opts {
		if o == nil {
			continue
		}
		if err := o(&cfg); err != nil {
			return nil, err
		}
	}

	// Build immutable providers map.
	providers := map[string]provider.SkillProvider{}
	for _, p := range cfg.providers {
		if p == nil {
			return nil, errors.New("nil provider")
		}
		t := p.Type()
		if t == "" || strings.TrimSpace(t) != t {
			return nil, errors.New("provider.Type() returned an invalid type")
		}
		if _, exists := providers[t]; exists {
			return nil, fmt.Errorf("duplicate provider type: %q", t)
		}
		providers[t] = p
	}
	supportsRunScript := anyProviderSupportsRunScript(providers)

	res := providerResolver{m: providers}
	cat := catalog.NewCatalog(res)

	st := session.NewStore(session.StoreConfig{
		TTL:                 cfg.sessionTTL,
		MaxSessions:         cfg.maxSessions,
		MaxActivePerSession: cfg.maxActivePerSession,
		Catalog:             cat,
		Providers:           res,
		SupportsRunScript:   supportsRunScript,
	})

	rt := &Runtime{
		providers:         providers,
		catalog:           cat,
		sessions:          st,
		supportsRunScript: supportsRunScript,
	}
	return rt, nil
}

// ProviderTypes returns the registered provider type keys (e.g. "fs").
func (r *Runtime) ProviderTypes() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.providers))
	for t := range r.providers {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func (r *Runtime) SupportsRunScript() bool {
	return r != nil && r.supportsRunScript
}

// AddSkill indexes and registers a skill into the runtime-owned catalog.
//
// IMPORTANT CONTRACT:
//   - This is a HOST/LIFECYCLE API.
//   - It accepts and returns only the user-provided skill definition (provider.SkillDef).
//   - Provider canonicalization/cleanup is internal only and MUST NOT be exposed via this API.
func (r *Runtime) AddSkill(ctx context.Context, def provider.SkillDef) (spec.SkillRecord, error) {
	if ctx == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil context", spec.ErrInvalidRuntimeArgument)
	}
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	if r == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidRuntimeArgument)
	}

	// Enforce "no cleanup is user-facing": do not silently trim.
	if strings.TrimSpace(def.Type) != def.Type ||
		strings.TrimSpace(def.Name) != def.Name ||
		strings.TrimSpace(def.Location) != def.Location {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: def fields must not contain leading/trailing whitespace",
			spec.ErrInvalidRuntimeArgument,
		)
	}

	return r.catalog.Add(ctx, def)
}

// RemoveSkill removes a skill from the catalog (and prunes it from all sessions).
//
// IMPORTANT CONTRACT:
//   - This is a HOST/LIFECYCLE API.
//   - Removal is by the exact user-provided definition that was added.
//   - No canonicalization-based matching is performed (to avoid internal cleanup becoming user-facing).
func (r *Runtime) RemoveSkill(ctx context.Context, def provider.SkillDef) (spec.SkillRecord, error) {
	if ctx == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil context", spec.ErrInvalidRuntimeArgument)
	}
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	if r == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidRuntimeArgument)
	}

	if strings.TrimSpace(def.Type) != def.Type ||
		strings.TrimSpace(def.Name) != def.Name ||
		strings.TrimSpace(def.Location) != def.Location {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: def fields must not contain leading/trailing whitespace",
			spec.ErrInvalidRuntimeArgument,
		)
	}

	rec, canonKey, ok := r.catalog.Remove(def)
	if !ok {
		return spec.SkillRecord{}, spec.ErrSkillNotFound
	}

	// Prune using canonical/internal key.
	r.sessions.PruneSkill(canonKey)
	return rec, nil
}

// ListSkills lists skills for HOST/LIFECYCLE usage.
//
// IMPORTANT CONTRACT:
//   - Returns only user-provided skill definitions in SkillRecord.Def.
//   - Filters (NamePrefix/LocationPrefix) apply to user-provided Def fields (not LLM-facing names).
func (r *Runtime) ListSkills(ctx context.Context, filter *SkillListFilter) ([]spec.SkillRecord, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", spec.ErrInvalidRuntimeArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidRuntimeArgument)
	}

	cfg := normalizeSkillListFilter(filter)

	// Validate activity/session constraints early.
	switch cfg.Activity {
	case spec.SkillActivityAny, spec.SkillActivityInactive:
		// OK.
	case spec.SkillActivityActive:
		if cfg.SessionID == "" {
			return nil, fmt.Errorf("%w: activity=active requires sessionID", spec.ErrInvalidRuntimeArgument)
		}
	default:
		return nil, fmt.Errorf("%w: invalid activity %q", spec.ErrInvalidRuntimeArgument, cfg.Activity)
	}

	entries := r.catalog.ListUserEntries(toCatalogUserFilter(&cfg))

	// No session => no active skills exist; "inactive" behaves like "all".
	if cfg.SessionID == "" {
		out := make([]spec.SkillRecord, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.Record)
		}
		return out, nil
	}

	// Session-scoped filtering.
	s, ok := r.sessions.Get(string(cfg.SessionID))
	if !ok {
		return nil, spec.ErrSessionNotFound
	}
	keys, err := s.ActiveKeys(ctx)
	if err != nil {
		return nil, err
	}
	activeSet := make(map[provider.ProviderSkillKey]struct{}, len(keys))
	for _, k := range keys {
		activeSet[k] = struct{}{}
	}

	if cfg.Activity == spec.SkillActivityAny {
		out := make([]spec.SkillRecord, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.Record)
		}
		return out, nil
	}

	out := make([]spec.SkillRecord, 0, len(entries))
	for _, e := range entries {
		_, isActive := activeSet[e.Key]
		switch cfg.Activity {
		case spec.SkillActivityActive:
			if isActive {
				out = append(out, e.Record)
			}
		case spec.SkillActivityInactive:
			if !isActive {
				out = append(out, e.Record)
			}
		default:
			// Any already handled.
		}
	}
	return out, nil
}

type newSessionOptions struct {
	// If >0 overrides runtime/store default.
	maxActivePerSession int

	// Optional initial active set (HOST/LIFECYCLE definitions).
	activeDefs []provider.SkillDef

	allowedDefs           []provider.SkillDef
	allowedDefsConfigured bool
}

// SessionOption configures Runtime.NewSession.
type SessionOption func(*newSessionOptions) error

// WithSessionAllowedSkills restricts all session operations to these exact
// host/lifecycle definitions. Calling it with an empty slice creates a session
// with no allowed skills. Omitting it leaves the session unrestricted.
func WithSessionAllowedSkills(defs []provider.SkillDef) SessionOption {
	snap := append([]provider.SkillDef(nil), defs...)
	return func(o *newSessionOptions) error {
		o.allowedDefs = snap
		o.allowedDefsConfigured = true
		return nil
	}
}

// WithSessionMaxActivePerSession overrides the max active skills for this session only.
// If n <= 0, it is ignored (defaults apply).
func WithSessionMaxActivePerSession(n int) SessionOption {
	return func(o *newSessionOptions) error {
		o.maxActivePerSession = n
		return nil
	}
}

// WithSessionActiveSkills sets the initial active skills for the new session (host/lifecycle defs).
// These are activated during session creation.
func WithSessionActiveSkills(defs []provider.SkillDef) SessionOption {
	snap := append([]provider.SkillDef(nil), defs...)
	return func(o *newSessionOptions) error {
		o.activeDefs = snap
		return nil
	}
}

// NewSession creates a new session.
//
// IMPORTANT CONTRACT:
//   - This is a HOST/LIFECYCLE API.
//   - It accepts and returns skill definitions (provider.SkillDef), never LLM handles.
func (r *Runtime) NewSession(ctx context.Context, opts ...SessionOption) (spec.SessionID, []provider.SkillDef, error) {
	if ctx == nil {
		return "", nil, fmt.Errorf("%w: nil context", spec.ErrInvalidRuntimeArgument)
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}

	if r == nil {
		return "", nil, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidRuntimeArgument)
	}

	cfg := newSessionOptions{}
	for _, o := range opts {
		if o == nil {
			continue
		}
		if err := o(&cfg); err != nil {
			return "", nil, err
		}
	}

	var allowedKeys map[provider.ProviderSkillKey]struct{}
	if cfg.allowedDefsConfigured {
		keys, err := r.resolveSessionSkillDefs(cfg.allowedDefs, "allowed")
		if err != nil {
			return "", nil, err
		}
		allowedKeys = make(map[provider.ProviderSkillKey]struct{}, len(keys))
		for _, key := range keys {
			allowedKeys[key] = struct{}{}
		}
	}

	activeKeys, err := r.resolveSessionSkillDefs(cfg.activeDefs, "active")
	if err != nil {
		return "", nil, err
	}
	id, _, err := r.sessions.NewSession(ctx, session.NewSessionParams{
		MaxActivePerSession: cfg.maxActivePerSession,
		ActiveKeys:          activeKeys,
		AllowedKeys:         allowedKeys,
	})
	if err != nil {
		return "", nil, err
	}

	// Return exactly what the host provided (no computed handles / no canonicalization leakage).
	if len(cfg.activeDefs) == 0 {
		return spec.SessionID(id), nil, nil
	}
	return spec.SessionID(id), append([]provider.SkillDef(nil), cfg.activeDefs...), nil
}

func (r *Runtime) CloseSession(ctx context.Context, sid spec.SessionID) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", spec.ErrInvalidRuntimeArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidRuntimeArgument)
	}
	if sid == "" {
		return nil
	}
	r.sessions.Delete(string(sid))
	return nil
}

func (r *Runtime) NewSessionRegistry(
	ctx context.Context,
	sid spec.SessionID,
	opts ...llmtools.RegistryOption,
) (*llmtools.Registry, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", spec.ErrInvalidRuntimeArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidRuntimeArgument)
	}
	s, ok := r.sessions.Get(string(sid))
	if !ok {
		return nil, spec.ErrSessionNotFound
	}
	return s.NewRegistry(opts...)
}

func (r *Runtime) resolveSessionSkillDefs(
	defs []provider.SkillDef,
	category string,
) ([]provider.ProviderSkillKey, error) {
	seen := make(map[provider.SkillDef]struct{}, len(defs))
	keys := make([]provider.ProviderSkillKey, 0, len(defs))
	for _, def := range defs {
		if _, duplicate := seen[def]; duplicate {
			return nil, fmt.Errorf(
				"%w: duplicate %s skill def: %+v",
				spec.ErrInvalidRuntimeArgument,
				category,
				def,
			)
		}
		seen[def] = struct{}{}
		key, found := r.catalog.ResolveDef(def)
		if !found {
			return nil, fmt.Errorf(
				"%w: unknown %s skill def: %+v",
				spec.ErrSkillNotFound,
				category,
				def,
			)
		}
		keys = append(keys, key)
	}
	return keys, nil
}
