package session

import (
	"errors"
	"testing"

	"github.com/flexigpt/agentskills-go/spec"
)

func TestTools_ClosedSessionShortCircuitsAllTools(t *testing.T) {
	t.Parallel()

	s := newSession(SessionConfig{
		ID:                  "closed-session",
		Catalog:             newMemCatalog(),
		Providers:           mapResolver{"t": &recordingProvider{typ: "t"}},
		MaxActivePerSession: 8,
		Touch:               func() {},
	})
	s.closed.Store(true)

	if _, err := s.toolLoad(t.Context(), spec.LoadArgs{}); !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("toolLoad on closed session: expected ErrSessionNotFound, got %v", err)
	}
	if _, err := s.toolUnload(t.Context(), spec.UnloadArgs{}); !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("toolUnload on closed session: expected ErrSessionNotFound, got %v", err)
	}
	if _, err := s.toolRead(t.Context(), spec.ReadResourceArgs{}); !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("toolRead on closed session: expected ErrSessionNotFound, got %v", err)
	}
	if _, err := s.toolRunScript(t.Context(), spec.RunScriptArgs{}); !errors.Is(err, spec.ErrSessionNotFound) {
		t.Fatalf("toolRunScript on closed session: expected ErrSessionNotFound, got %v", err)
	}
}
