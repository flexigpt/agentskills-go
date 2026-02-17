package agentskills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
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

func WithProviders(m map[string]spec.SkillProvider) Option {
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
func (r *Runtime) AddSkill(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	if ctx == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	if r == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
	}
	return r.catalog.Add(ctx, key)
}

// RemoveSkill removes a skill from the catalog (and prunes it from all sessions).
func (r *Runtime) RemoveSkill(ctx context.Context, key spec.SkillKey) (spec.SkillRecord, error) {
	if ctx == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return spec.SkillRecord{}, err
	}
	if r == nil {
		return spec.SkillRecord{}, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
	}
	rec, ok := r.catalog.Remove(key)
	if !ok {
		return spec.SkillRecord{}, spec.ErrSkillNotFound
	}
	r.sessions.PruneSkill(key)
	return rec, nil
}

func (r *Runtime) ListSkills(ctx context.Context, filter *SkillFilter) ([]spec.SkillRecord, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", spec.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: nil runtime receiver", spec.ErrInvalidArgument)
	}

	cfg := normalizeSkillsPromptFilter(filter)

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

	records := r.catalog.ListRecords(toCatalogFilter(&cfg))

	// No session => no active skills exist; "inactive" behaves like "all".
	if cfg.SessionID == "" {
		return records, nil
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
	activeSet := make(map[spec.SkillKey]struct{}, len(keys))
	for _, k := range keys {
		activeSet[k] = struct{}{}
	}

	if cfg.Activity == SkillActivityAny {
		return records, nil
	}

	out := make([]spec.SkillRecord, 0, len(records))
	for _, rec := range records {
		_, isActive := activeSet[rec.Key]
		switch cfg.Activity {
		case SkillActivityActive:
			if isActive {
				out = append(out, rec)
			}
		case SkillActivityInactive:
			if !isActive {
				out = append(out, rec)
			}
		default:
			// We filtered Any records above.
		}
	}
	return out, nil
}

type newSessionOptions struct {
	// If >0 overrides runtime/store default.
	maxActivePerSession int

	// Optional initial active set.
	activeKeys []spec.SkillKey
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

// WithSessionActiveKeys sets the initial active skill keys for the new session.
// These are activated during session creation (no post-creation host activation API).
func WithSessionActiveKeys(keys []spec.SkillKey) SessionOption {
	snap := append([]spec.SkillKey(nil), keys...)
	return func(o *newSessionOptions) error {
		o.activeKeys = snap
		return nil
	}
}

// NewSession creates a new session. Session configuration (including an initial active set)
// must be provided via SessionOption(s).
func (r *Runtime) NewSession(ctx context.Context, opts ...SessionOption) (spec.SessionID, []spec.SkillHandle, error) {
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

	id, active, err := r.sessions.NewSession(ctx, session.NewSessionParams{
		MaxActivePerSession: cfg.maxActivePerSession,
		ActiveKeys:          cfg.activeKeys,
	})
	if err != nil {
		return "", nil, err
	}

	return spec.SessionID(id), active, nil
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
