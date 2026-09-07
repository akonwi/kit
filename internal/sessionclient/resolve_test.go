package sessionclient

import (
	"context"
	"errors"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestResolveSessionPrefersExactAndAcceptsUniqueShortID(t *testing.T) {
	t.Parallel()
	server := &resolveServer{sessions: []protocol.SessionInfo{
		{ID: "session_0123456789abcdef0123456789abcdef"},
		{ID: "session_fedcba9876543210fedcba9876543210"},
	}}
	exact, err := ResolveSession(context.Background(), server, server.sessions[1].ID)
	if err != nil || exact.ID != server.sessions[1].ID {
		t.Fatalf("exact ResolveSession() = %+v, %v", exact, err)
	}
	short, err := ResolveSession(context.Background(), server, "01234567")
	if err != nil || short.ID != server.sessions[0].ID {
		t.Fatalf("short ResolveSession() = %+v, %v", short, err)
	}
	prefixed, err := ResolveSession(context.Background(), server, "session_fedcba")
	if err != nil || prefixed.ID != server.sessions[1].ID {
		t.Fatalf("prefixed ResolveSession() = %+v, %v", prefixed, err)
	}
}

func TestResolveSessionRejectsMissingAndAmbiguousSelectors(t *testing.T) {
	t.Parallel()
	server := &resolveServer{sessions: []protocol.SessionInfo{
		{ID: "session_0123456789abcdef0123456789abcdef"},
		{ID: "session_0123fedcba9876540123fedcba987654"},
	}}
	if _, err := ResolveSession(context.Background(), server, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing ResolveSession() error = %v", err)
	}
	if _, err := ResolveSession(context.Background(), server, "0123"); !errors.Is(err, ErrSessionAmbiguous) {
		t.Fatalf("ambiguous ResolveSession() error = %v", err)
	}
}

type resolveServer struct{ sessions []protocol.SessionInfo }

func (s *resolveServer) CreateSession(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	panic("unexpected CreateSession")
}
func (s *resolveServer) RenameSession(context.Context, string, string) (protocol.SessionInfo, error) {
	panic("unexpected RenameSession")
}
func (s *resolveServer) DeleteSession(context.Context, string) error {
	panic("unexpected DeleteSession")
}
func (s *resolveServer) DisposeTemporarySession(context.Context, string) error {
	panic("unexpected DisposeTemporarySession")
}
func (s *resolveServer) ListSessions(context.Context, string) ([]protocol.SessionInfo, error) {
	return s.sessions, nil
}
func (s *resolveServer) Attach(context.Context, string) (Session, error) {
	return nil, errors.New("unexpected Attach")
}
