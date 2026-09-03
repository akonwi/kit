package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestSupportedAuthProvidersMatchDroidsProviders(t *testing.T) {
	t.Parallel()

	if len(authProviderOptions) != 3 {
		t.Fatalf("provider option count = %d, want 3", len(authProviderOptions))
	}
	for index, want := range []struct {
		id     string
		method string
	}{
		{id: "openai-codex", method: "ChatGPT plan · device code"},
		{id: "anthropic", method: "API key"},
		{id: "openai", method: "API key"},
	} {
		got := authProviderOptions[index]
		if got.ID != want.id || got.Method != want.method || got.DefaultModel == "" {
			t.Fatalf("provider option %d = %#v, want id %q, method %q, and a default model", index, got, want.id, want.method)
		}
	}
	filtered := filteredAuthProviders("anth")
	if len(filtered) != 1 || filtered[0].ID != "anthropic" {
		t.Fatalf("filtered providers = %#v", filtered)
	}
}

func TestAPIKeyProviderSelectionSubmitsObscuredCredential(t *testing.T) {
	t.Parallel()

	calls := make(chan apiKeyLoginCall, 1)
	application := uitest.New(app{Options: Options{
		Context: context.Background(), Server: &fakeServer{}, CWD: "/repo",
		APIKeyLogin: fakeAPIKeyLogin{calls: calls},
	}})
	application.Pump(80, 24)
	application.Enter()
	application.Pump(80, 24)
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	application.Pump(80, 24)
	application.Enter()
	application.Pump(80, 24)
	for _, character := range "anthropic-secret" {
		application.Key(string(character))
		application.Pump(80, 24)
	}
	application.Enter()

	select {
	case call := <-calls:
		if call.providerID != auth.AnthropicProviderID || call.apiKey != "anthropic-secret" {
			t.Fatalf("API-key login call = %#v", call)
		}
	case <-time.After(time.Second):
		t.Fatal("API-key login was not submitted")
	}
}

func TestBootstrapSessionResumesNewestUsableSession(t *testing.T) {
	t.Parallel()

	server := &fakeServer{sessions: []protocol.SessionInfo{
		{ID: "anthropic", CWD: "/repo", Model: "anthropic/claude-sonnet-4-6"},
		{ID: "codex", CWD: "/repo", Model: "openai-codex/gpt-5.6-sol"},
	}}
	info, bound, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "medium",
		func(model string) bool { return modelProvider(model) == "openai-codex" },
	)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "codex" || bound.ID() != "codex" {
		t.Fatalf("selected session = %q / %q, want codex", info.ID, bound.ID())
	}
	if server.created.Model != "" {
		t.Fatalf("created session = %+v, want none", server.created)
	}
}

func TestBootstrapSessionCreatesWhenNoUsableSessionExists(t *testing.T) {
	t.Parallel()

	server := &fakeServer{createdResult: protocol.SessionInfo{
		ID: "created", CWD: "/repo", Model: codexDefaultModel, ThinkingLevel: "high",
	}}
	info, bound, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "high",
		func(string) bool { return false },
	)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "created" || bound.ID() != "created" {
		t.Fatalf("selected session = %q / %q, want created", info.ID, bound.ID())
	}
	if server.created.Model != codexDefaultModel || server.created.ThinkingLevel != "high" {
		t.Fatalf("create input = %+v", server.created)
	}
}

type apiKeyLoginCall struct {
	providerID string
	apiKey     string
}

type fakeAPIKeyLogin struct{ calls chan<- apiKeyLoginCall }

func (f fakeAPIKeyLogin) Login(_ context.Context, providerID, apiKey string) error {
	f.calls <- apiKeyLoginCall{providerID: providerID, apiKey: apiKey}
	return errors.New("test login stopped")
}

type fakeServer struct {
	sessions      []protocol.SessionInfo
	created       protocol.CreateSessionInput
	createdResult protocol.SessionInfo
}

func (s *fakeServer) CreateSession(_ context.Context, input protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	s.created = input
	return s.createdResult, nil
}

func (s *fakeServer) ListSessions(context.Context, string) ([]protocol.SessionInfo, error) {
	return append([]protocol.SessionInfo(nil), s.sessions...), nil
}

func (s *fakeServer) Attach(_ context.Context, sessionID string) (sessionclient.Session, error) {
	return fakeSession{id: sessionID}, nil
}

type fakeSession struct{ id string }

func (s fakeSession) ID() string { return s.id }

func (fakeSession) Snapshot(context.Context) (protocol.SessionSnapshot, error) {
	return protocol.SessionSnapshot{}, nil
}

func (fakeSession) Run(context.Context, string) (protocol.RunInfo, error) {
	return protocol.RunInfo{}, nil
}

func (fakeSession) Abort(context.Context, string) error { return nil }

func (fakeSession) StartPrompt(context.Context, string) (sessionclient.Run, error) {
	panic("unexpected StartPrompt")
}
