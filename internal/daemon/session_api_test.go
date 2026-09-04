package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/version"
)

func protocolContentText(message protocol.TranscriptMessage, kind protocol.TranscriptContentKind) string {
	var result string
	for _, block := range message.Content {
		if block.Kind != kind {
			continue
		}
		if result != "" {
			result += "\n"
		}
		result += block.Text
	}
	return result
}

func TestProjectProviderErrorKind(t *testing.T) {
	t.Parallel()
	if got := projectProviderErrorKind(kitsession.ProviderErrorAuthentication); got != protocol.ProviderErrorAuthentication {
		t.Fatalf("authentication projection = %q", got)
	}
	if got := projectProviderErrorKind(kitsession.ProviderErrorKind("unknown")); got != "" {
		t.Fatalf("unknown projection = %q", got)
	}
}

func TestSessionClientRejectsIncompatibleRegistryBeforeRequest(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Paths.Ensure() error = %v", err)
	}
	token, err := newToken()
	if err != nil {
		t.Fatalf("newToken() error = %v", err)
	}
	if err := writePrivateFile(paths.ServerToken, []byte(token+"\n")); err != nil {
		t.Fatalf("write token: %v", err)
	}
	registry := Registry{
		RegistryVersion: version.LocalRegistryVersion,
		ProtocolVersion: version.SessionProtocolVersion - 1,
		KitVersion:      version.Version,
		PID:             1, InstanceID: "old", URL: "http://127.0.0.1:1", StartedAt: time.Now(),
	}
	if err := writeRegistry(paths, registry); err != nil {
		t.Fatalf("writeRegistry() error = %v", err)
	}
	if _, err := NewClient(paths).ListSessions(context.Background(), ""); !errors.Is(err, ErrIncompatibleDaemon) {
		t.Fatalf("ListSessions() error = %v, want ErrIncompatibleDaemon", err)
	}
}

func TestLocalSessionClientRunsPersistedDroidsPrompt(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	providers := &daemonEchoProviders{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, RunOptions{
			Paths: paths, Providers: providers,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()

	client := NewClient(paths)
	probeContext, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	for {
		if _, _, err := client.Probe(probeContext); err == nil {
			break
		}
		select {
		case <-probeContext.Done():
			t.Fatal("daemon did not become ready")
		case <-time.After(10 * time.Millisecond):
		}
	}

	workspace := t.TempDir()
	created, err := client.CreateSession(context.Background(), protocol.CreateSessionInput{
		CWD: workspace, Model: "test/echo", Name: "API test",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	runID, err := identifier.New("run_")
	if err != nil {
		t.Fatalf("identifier.New() error = %v", err)
	}
	reservation, err := client.StartPrompt(context.Background(), created.ID, runID, "hello")
	if err != nil {
		t.Fatalf("StartPrompt() error = %v", err)
	}
	if reservation.RunID != runID {
		t.Fatalf("reservation = %+v", reservation)
	}
	var run protocol.RunInfo
	runDeadline := time.Now().Add(5 * time.Second)
	for {
		run, err = client.GetRun(context.Background(), created.ID, runID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if run.Status == protocol.RunStatusCompleted {
			break
		}
		if time.Now().After(runDeadline) {
			t.Fatalf("run did not complete: %+v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSessionSnapshot() error = %v", err)
	}
	if snapshot.Session.ID != created.ID || len(snapshot.Messages) != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Messages[0].Role != "user" ||
		protocolContentText(snapshot.Messages[0], protocol.TranscriptContentText) != "hello" ||
		snapshot.Messages[1].Role != "assistant" ||
		protocolContentText(snapshot.Messages[1], protocol.TranscriptContentText) != "reply 1" {
		t.Fatalf("snapshot messages = %+v", snapshot.Messages)
	}
	if snapshot.ContextTokens != 64_000 || snapshot.ContextWindow != 128_000 {
		t.Fatalf("snapshot context = %d/%d", snapshot.ContextTokens, snapshot.ContextWindow)
	}
	eventBatch, err := client.GetSessionEvents(context.Background(), created.ID, 0)
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}
	wantEventKinds := []protocol.SessionEventKind{
		protocol.SessionEventRunStarted,
		protocol.SessionEventUserMessage,
		protocol.SessionEventAssistantStarted,
		protocol.SessionEventAssistantCompleted,
		protocol.SessionEventRunFinished,
	}
	if len(eventBatch.Events) != len(wantEventKinds) {
		t.Fatalf("session event count = %d, want %d: %+v", len(eventBatch.Events), len(wantEventKinds), eventBatch.Events)
	}
	for index, want := range wantEventKinds {
		if eventBatch.Events[index].Kind != want {
			t.Errorf("session event %d kind = %q, want %q", index, eventBatch.Events[index].Kind, want)
		}
	}
	if eventBatch.Events[2].MessageID != snapshot.Messages[1].ID || eventBatch.Events[3].MessageID != snapshot.Messages[1].ID {
		t.Errorf("live assistant ids = %q/%q, snapshot id = %q", eventBatch.Events[2].MessageID, eventBatch.Events[3].MessageID, snapshot.Messages[1].ID)
	}
	if eventBatch.Events[3].Kind != protocol.SessionEventAssistantCompleted || eventBatch.Events[4].Status != protocol.RunStatusCompleted {
		t.Errorf("terminal session events = %+v", eventBatch.Events[3:])
	}

	block := make(chan struct{})
	providers.mu.Lock()
	providers.block = block
	providers.mu.Unlock()
	activeRunID, err := identifier.New("run_")
	if err != nil {
		t.Fatalf("identifier.New() error = %v", err)
	}
	if _, err := client.StartPrompt(context.Background(), created.ID, activeRunID, "block"); err != nil {
		t.Fatalf("StartPrompt() blocking run error = %v", err)
	}
	active, err := client.GetRun(context.Background(), created.ID, activeRunID)
	if err != nil || active.Status != protocol.RunStatusRunning {
		t.Fatalf("active run = %+v, %v", active, err)
	}
	activeSnapshot, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil || activeSnapshot.ActiveRunID != activeRunID || len(activeSnapshot.Messages) != 2 {
		t.Fatalf("active snapshot = %+v, %v", activeSnapshot, err)
	}
	if err := client.AbortSession(context.Background(), created.ID, activeRunID); err != nil {
		t.Fatalf("AbortSession() active run error = %v", err)
	}
	providers.mu.Lock()
	providers.block = nil
	providers.mu.Unlock()
	activeDeadline := time.Now().Add(5 * time.Second)
	for {
		active, err = client.GetRun(context.Background(), created.ID, activeRunID)
		if err != nil {
			t.Fatalf("GetRun() aborted active run error = %v", err)
		}
		if active.Status == protocol.RunStatusAborted {
			close(block)
			break
		}
		if time.Now().After(activeDeadline) {
			t.Fatalf("active run did not abort: %+v", active)
		}
		time.Sleep(10 * time.Millisecond)
	}

	abortedRunID, err := identifier.New("run_")
	if err != nil {
		t.Fatalf("identifier.New() error = %v", err)
	}
	if _, err := client.ReserveRun(context.Background(), created.ID, abortedRunID); err != nil {
		t.Fatalf("ReserveRun() for abort error = %v", err)
	}
	if err := client.AbortSession(context.Background(), created.ID, abortedRunID); err != nil {
		t.Fatalf("AbortSession() error = %v", err)
	}
	aborted, err := client.RunPrompt(context.Background(), created.ID, abortedRunID, "do not run")
	if err != nil {
		t.Fatalf("aborted RunPrompt() error = %v", err)
	}
	if aborted.Status != protocol.RunStatusAborted {
		t.Fatalf("aborted outcome = %+v", aborted)
	}
	sessions, err := client.ListSessions(context.Background(), workspace)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != created.ID {
		t.Fatalf("sessions = %+v", sessions)
	}

	stopContext, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := NewManager(paths).Stop(stopContext); err != nil {
		t.Fatalf("stop daemon: %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-stopContext.Done():
		t.Fatal("daemon did not stop")
	}
}

type daemonEchoProviders struct {
	mu    sync.Mutex
	calls int
	block <-chan struct{}
}

func (p *daemonEchoProviders) Models() []droids.Model { return []droids.Model{p.model()} }

func (p *daemonEchoProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "echo" || id == "test/echo"
}

func (p *daemonEchoProviders) RefreshModels(context.Context) error { return nil }

func (p *daemonEchoProviders) Stream(
	ctx context.Context,
	_ droids.Model,
	_ droids.Request,
) droids.Stream {
	p.mu.Lock()
	p.calls++
	text := fmt.Sprintf("reply %d", p.calls)
	block := p.block
	p.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			final := droids.AssistantMessage{
				Provider: "test", Model: "echo", StopReason: droids.StopReasonAborted,
				Timestamp: time.Now().UnixMilli(),
			}
			return &daemonEchoStream{final: final}
		}
	}
	final := droids.AssistantMessage{
		Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
		Content:   []droids.Content{droids.TextContent{Text: text}},
		Usage:     droids.Usage{TotalTokens: 64_000},
		Timestamp: time.Now().UnixMilli(),
	}
	return &daemonEchoStream{final: final}
}

func (p *daemonEchoProviders) model() droids.Model {
	return droids.Model{
		ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
	}
}

type daemonEchoStream struct {
	final droids.AssistantMessage
}

func (s *daemonEchoStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: "test", Model: "echo"}}
	events <- droids.StreamDone{Message: s.final}
	close(events)
	return events
}

func (s *daemonEchoStream) Result() droids.AssistantMessage { return s.final }
