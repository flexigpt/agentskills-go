package agentskills

import (
	"log/slog"
	"maps"
	"time"

	"github.com/flexigpt/agentskills-go/internal/catalog"
	"github.com/flexigpt/agentskills-go/spec"
)

func toCatalogFilter(f *SkillFilter) catalog.Filter {
	if f == nil {
		return catalog.Filter{}
	}
	return catalog.Filter{
		Types:      append([]string(nil), f.Types...),
		NamePrefix: f.NamePrefix,
		PathPrefix: f.PathPrefix,
	}
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
	return func(o *runtimeOptions) error {
		if o.providersByType == nil {
			o.providersByType = map[string]spec.SkillProvider{}
		}
		maps.Copy(o.providersByType, m)
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

// SkillFilter is an optional filter for listing/prompting skills.
type SkillFilter struct {
	// Types restricts to provider types (e.g. ["fs"]). Empty means "all".
	Types []string

	// NamePrefix restricts to LLM-visible names with this prefix.
	NamePrefix string

	// PathPrefix restricts to skills whose base path starts with this prefix.
	PathPrefix string
}
