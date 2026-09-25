package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/protocol"
	kitserver "github.com/akonwi/kit/internal/server"
	"github.com/akonwi/kit/internal/sessionclient"
)

func TestBoundLocalAndHTTPClientsShareReloadSemantics(t *testing.T) {
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	serverContext, stopServer := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- kitserver.Run(serverContext, kitserver.RunOptions{
			Paths: paths, Providers: reloadProviders{},
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	t.Cleanup(func() {
		stopServer()
		select {
		case err := <-serverDone:
			if err != nil {
				t.Errorf("daemon shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	transport := kitserver.NewClient(paths)
	probeContext, cancelProbe := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelProbe()
	for {
		if _, _, err := transport.Probe(probeContext); err == nil {
			break
		}
		select {
		case <-probeContext.Done():
			t.Fatal("daemon did not become ready")
		case <-time.After(10 * time.Millisecond):
		}
	}

	workspace := t.TempDir()
	contextPath := filepath.Join(workspace, "AGENTS.md")
	if err := os.WriteFile(contextPath, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewLocalServer(paths)
	created, err := server.CreateSession(t.Context(), protocol.CreateSessionInput{
		ID: "session_cccccccccccccccccccccccccccccccc", CWD: workspace, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := server.Attach(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bound.(sessionclient.ScratchpadSession); ok {
		t.Fatal("temporary session exposed scratchpad capability")
	}
	persistent, err := server.CreateSession(t.Context(), protocol.CreateSessionInput{
		ID: "session_dddddddddddddddddddddddddddddddd", CWD: workspace, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	persistentBound, err := server.Attach(t.Context(), persistent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := persistentBound.(sessionclient.ScratchpadSession); !ok {
		t.Fatal("persistent session omitted scratchpad capability")
	}
	before, err := bound.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(contextPath, []byte("via-http"), 0o600); err != nil {
		t.Fatal(err)
	}
	httpResult, err := transport.ReloadSession(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if httpResult.EventStreamID != before.EventStreamID || !reloadHasContextSource(httpResult, contextPath) {
		t.Fatalf("HTTP reload result = %+v", httpResult)
	}

	if err := os.WriteFile(contextPath, []byte("via-bound-client"), 0o600); err != nil {
		t.Fatal(err)
	}
	boundResult, err := bound.Reload(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if boundResult.EventStreamID != httpResult.EventStreamID || !reloadHasContextSource(boundResult, contextPath) {
		t.Fatalf("bound reload result = %+v", boundResult)
	}
	after, err := bound.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.EventStreamID != boundResult.EventStreamID || len(after.Messages) != len(before.Messages) {
		t.Fatalf("snapshot after bound reload = %+v", after)
	}

	nested := filepath.Join(workspace, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := bound.ChangeCWD(t.Context(), " "); err == nil {
		t.Fatal("bound cwd change accepted an empty target")
	}
	if _, err := bound.ChangeCWD(t.Context(), "missing"); err == nil {
		t.Fatal("bound cwd change accepted a missing target")
	}
	mutationID := "cwd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	changedByHTTP, err := transport.ChangeSessionCWDWithID(t.Context(), created.ID, mutationID, "nested")
	if err != nil || changedByHTTP.CWD != nested {
		t.Fatalf("HTTP cwd change = %+v, error = %v", changedByHTTP, err)
	}
	replayedHTTP, err := transport.ChangeSessionCWDWithID(t.Context(), created.ID, mutationID, "nested")
	if err != nil || replayedHTTP.CWD != nested {
		t.Fatalf("replayed HTTP cwd change = %+v, error = %v", replayedHTTP, err)
	}
	changedByBound, err := bound.ChangeCWD(t.Context(), "..")
	if err != nil || changedByBound.CWD != workspace {
		t.Fatalf("bound cwd change = %+v, error = %v", changedByBound, err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := bound.Reload(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled bound reload error = %v", err)
	}
	if _, err := bound.ChangeCWD(canceled, "nested"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled bound cwd change error = %v", err)
	}
}

func reloadHasContextSource(result protocol.ReloadSessionResult, path string) bool {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	for _, source := range result.Sources {
		if source.Kind == protocol.PromptSectionContext && source.Path == canonical {
			return true
		}
	}
	return false
}

type reloadProviders struct{}

func (reloadProviders) ID() string { return "test" }
func (reloadProviders) Models() []droids.Model {
	return []droids.Model{{
		ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
	}}
}
func (p reloadProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, errors.New("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (p reloadProviders) Model(id string) (droids.Model, bool) {
	return p.Models()[0], id == "echo" || id == "test/echo"
}
func (reloadProviders) RefreshModels(context.Context) error { return nil }
func (reloadProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (reloadProviders) Stream(context.Context, droids.Model, droids.Request) droids.Stream {
	return reloadStream{}
}

type reloadStream struct{}

func (reloadStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	message := droids.AssistantMessage{
		Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "ok"}},
	}
	events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: "test", Model: "echo"}}
	events <- droids.StreamDone{Message: message}
	close(events)
	return events
}
func (reloadStream) Result() droids.AssistantMessage {
	return droids.AssistantMessage{Provider: "test", Model: "echo", StopReason: droids.StopReasonStop}
}
