package agentskills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/flexigpt/llmtools-go"

	"github.com/flexigpt/agentskills-go/internal/catalog"
	"github.com/flexigpt/agentskills-go/internal/session"
	"github.com/flexigpt/agentskills-go/spec"
)

type Runtime struct {
	logger *slog.Logger

	// Immutable after New().
	providers map[string]spec.SkillProvider

	catalog  *catalog.Catalog
	sessions *session.Store
}

type runtimeOptions struct {
	logger *slog.Logger

	providers       []spec.SkillProvider
	providersByType map[string]spec.SkillProvider

	maxActivePerSession int
	sessionTTL          time.Duration
	maxSessions         int
}

type Option func(*runtimeOptions) error

func WithLogger(l *slog.Logger) Option {
	return func(o *runtimeOptions) error {
		o.logger = l
		return nil
	}
}

func WithProvider(p spec.SkillProvider) Option {
	return func(o *runtimeOptions) error {
		o.providers = append(o.providers, p)
		return nil
	}
}

func WithProvidersByType(m map[string]spec.SkillProvider) Option {
	// Snapshot input map at option-creation time to prevent caller mutation affecting New().
	snap := maps.Clone(m)

	return func(o *runtimeOptions) error {
		if o.providersByType == nil {
			o.providersByType = map[string]spec.SkillProvider{}
		}
		maps.Copy(o.providersByType, snap)
		return nil
	}
}

func WithMaxActivePerSession(n int) Option {
	return func(o *runtimeOptions) error {
		o.maxActivePerSession = n
		return nil
	}
}

func WithSessionTTL(ttl time.Duration) Option {
	return func(o *runtimeOptions) error {
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

type providerResolver struct {
	m map[string]spec.SkillProvider
}

func (r providerResolver) Provider(skillType string) (spec.SkillProvider, bool) {
	p, ok := r.m[skillType]
	return p, ok
}

func New(opts ...Option) (*Runtime, error) {
	cfg := runtimeOptions{
		logger:              slog.Default(),
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

	if cfg.logger == nil {
		cfg.logger = slog.Default()
	}

	// Build immutable providers map.
	providers := map[string]spec.SkillProvider{}
	for _, p := range cfg.providers {
		if p == nil {
			return nil, errors.New("nil provider")
		}
		t := p.Type()
		if t == "" {
			return nil, errors.New("provider.Type() returned empty string")
		}
		if _, exists := providers[t]; exists {
			return nil, fmt.Errorf("duplicate provider type: %q", t)
		}
		providers[t] = p
	}
	for t, p := range cfg.providersByType {
		if p == nil {
			return nil, fmt.Errorf("nil provider for type: %q", t)
		}
		if t == "" {
			return nil, errors.New("empty provider type key")
		}
		if _, exists := providers[t]; exists {
			return nil, fmt.Errorf("duplicate provider type: %q", t)
		}
		if p.Type() != t {
			return nil, fmt.Errorf("provider type mismatch: map key=%q provider.Type()=%q", t, p.Type())
		}
		providers[t] = p
	}

	res := providerResolver{m: providers}
	cat := catalog.New(res)

	st := session.NewStore(session.StoreConfig{
		TTL:                 cfg.sessionTTL,
		MaxSessions:         cfg.maxSessions,
		MaxActivePerSession: cfg.maxActivePerSession,
		Catalog:             cat,
		Providers:           res,
	})

	rt := &Runtime{
		logger:    cfg.logger,
		providers: providers,
		catalog:   cat,
		sessions:  st,
	}
	return rt, nil
}

// ProviderTypes returns the registered provider type keys (e.g. "fs").
func (r *Runtime) ProviderTypes() []string {
	out := make([]string, 0, len(r.providers))
	for t := range r.providers {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// AddSkill indexes and registers a skill into the runtime-owned catalog.
//
// IMPORTANT CONTRACT:
//   - This is a HOST/LIFECYCLE API.
//   - It accepts and returns only the user-provided skill definition (spec.SkillDef).
//   - Provider canonicalization/cleanup is internal only and MUST NOT be exposed via this API.
func (r *Runtime) AddSkill(ctx context.Context, def spec.SkillDef) (spec.SkillRecord, error) {
	if ctx == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	if r == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
	}

	// Enforce "no cleanup is user-facing": do not silently trim.
	if strings.TrimSpace(def.Type) != def.Type ||
		strings.TrimSpace(def.Name) != def.Name ||
		strings.TrimSpace(def.Location) != def.Location {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: def fields must not contain leading/trailing whitespace",
			spec.ErrInvalidArgument,
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
func (r *Runtime) RemoveSkill(ctx context.Context, def spec.SkillDef) (spec.SkillRecord, error) {
	if ctx == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	if r == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
	}

	if strings.TrimSpace(def.Type) != def.Type ||
		strings.TrimSpace(def.Name) != def.Name ||
		strings.TrimSpace(def.Location) != def.Location {
		return spec.SkillRecord{}, fmt.Errorf(
			"%w: def fields must not contain leading/trailing whitespace",
			spec.ErrInvalidArgument,
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
		return nil, fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
	}

	cfg := normalizeSkillListFilter(filter)

	// Validate activity/session constraints early.
	switch cfg.Activity {
	case SkillActivityAny, SkillActivityInactive:
		// OK.
	case SkillActivityActive:
		if cfg.SessionID == "" {
			return nil, fmt.Errorf("%w: activity=active requires sessionID", spec.ErrInvalidArgument)
		}
	default:
		return nil, fmt.Errorf("%w: invalid activity %q", spec.ErrInvalidArgument, cfg.Activity)
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
	activeSet := make(map[spec.ProviderSkillKey]struct{}, len(keys))
	for _, k := range keys {
		activeSet[k] = struct{}{}
	}

	if cfg.Activity == SkillActivityAny {
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
		case SkillActivityActive:
			if isActive {
				out = append(out, e.Record)
			}
		case SkillActivityInactive:
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
	activeDefs []spec.SkillDef
}

// SessionOption configures Runtime.NewSession.
type SessionOption func(*newSessionOptions) error

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
func WithSessionActiveSkills(defs []spec.SkillDef) SessionOption {
	snap := append([]spec.SkillDef(nil), defs...)
	return func(o *newSessionOptions) error {
		o.activeDefs = snap
		return nil
	}
}

// NewSession creates a new session.
//
// IMPORTANT CONTRACT:
//   - This is a HOST/LIFECYCLE API.
//   - It accepts and returns skill definitions (spec.SkillDef), never LLM handles.
func (r *Runtime) NewSession(ctx context.Context, opts ...SessionOption) (spec.SessionID, []spec.SkillDef, error) {
	if ctx == nil {
		return "", nil, fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}

	if r == nil {
		return "", nil, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
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

	// Resolve host defs -> canonical/internal keys (in order), without exposing canonicalization.
	var activeKeys []spec.ProviderSkillKey
	if len(cfg.activeDefs) > 0 {
		seen := map[spec.SkillDef]struct{}{}
		activeKeys = make([]spec.ProviderSkillKey, 0, len(cfg.activeDefs))
		for _, d := range cfg.activeDefs {
			if _, dup := seen[d]; dup {
				return "", nil, fmt.Errorf("%w: duplicate active skill def: %+v", spec.ErrInvalidArgument, d)
			}
			seen[d] = struct{}{}

			k, ok := r.catalog.ResolveDef(d)
			if !ok {
				return "", nil, fmt.Errorf("%w: unknown skill def: %+v", spec.ErrSkillNotFound, d)
			}
			activeKeys = append(activeKeys, k)
		}
	}

	id, _, err := r.sessions.NewSession(ctx, session.NewSessionParams{
		MaxActivePerSession: cfg.maxActivePerSession,
		ActiveKeys:          activeKeys,
	})
	if err != nil {
		return "", nil, err
	}

	// Return exactly what the host provided (no computed handles / no canonicalization leakage).
	if len(cfg.activeDefs) == 0 {
		return spec.SessionID(id), nil, nil
	}
	return spec.SessionID(id), append([]spec.SkillDef(nil), cfg.activeDefs...), nil
}

func (r *Runtime) CloseSession(ctx context.Context, sid spec.SessionID) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
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
		return nil, fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
	}
	s, ok := r.sessions.Get(string(sid))
	if !ok {
		return nil, spec.ErrSessionNotFound
	}
	return s.NewRegistry(opts...)
}
