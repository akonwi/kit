package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(filepath.Join(paths.Home, "AGENTS.md"), []byte("daemon-global-guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("daemon-project-guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectSkillDirectory := filepath.Join(workspace, ".agents", "skills", "project-check")
	if err := os.MkdirAll(projectSkillDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	projectSkillPath := filepath.Join(projectSkillDirectory, "SKILL.md")
	if err := os.WriteFile(projectSkillPath, []byte("---\nname: project-check\ndescription: Initial project skill\n---\nProject instructions.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectSkillLocation, err := filepath.EvalSymlinks(projectSkillPath)
	if err != nil {
		t.Fatal(err)
	}
	projectPromptDirectory := filepath.Join(workspace, ".agents", "prompts")
	if err := os.MkdirAll(projectPromptDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	projectPromptPath := filepath.Join(projectPromptDirectory, "summarize.md")
	if err := os.WriteFile(projectPromptPath, []byte("---\ndescription: Summarize a topic\n---\nSummarize $1 with $@."), 0o600); err != nil {
		t.Fatal(err)
	}
	projectPromptLocation, err := filepath.EvalSymlinks(projectPromptPath)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := identifier.New("session_")
	if err != nil {
		t.Fatalf("identifier.New() error = %v", err)
	}
	createInput := protocol.CreateSessionInput{
		ID: sessionID, CWD: workspace, Model: "test/echo", Name: "API test",
	}
	created, err := client.CreateSession(context.Background(), createInput)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	retried, err := client.CreateSession(context.Background(), createInput)
	if err != nil || retried.ID != created.ID {
		t.Fatalf("retry CreateSession() = %+v, %v; want %q", retried, err, created.ID)
	}
	renamed, err := client.RenameSession(context.Background(), created.ID, " Renamed API session ")
	if err != nil || renamed.ID != created.ID || renamed.Name != "Renamed API session" {
		t.Fatalf("RenameSession() = %+v, %v", renamed, err)
	}
	deleteID, err := identifier.New("session_")
	if err != nil {
		t.Fatal(err)
	}
	deleteCandidate, err := client.CreateSession(context.Background(), protocol.CreateSessionInput{
		ID: deleteID, CWD: workspace, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteSession(context.Background(), deleteCandidate.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	listedAfterDelete, err := client.ListSessions(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range listedAfterDelete {
		if listed.ID == deleteCandidate.ID {
			t.Fatalf("deleted session remained listed: %+v", listedAfterDelete)
		}
	}
	runID, err := identifier.New("run_")
	if err != nil {
		t.Fatalf("identifier.New() error = %v", err)
	}
	reservation, err := client.StartPrompt(context.Background(), created.ID, "hello")
	if err != nil {
		t.Fatalf("StartPrompt() error = %v", err)
	}
	if reservation.RunID == runID || reservation.RunID != reservation.TurnID {
		t.Fatalf("reservation = %+v", reservation)
	}
	runID = reservation.RunID
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
	if snapshot.ContextTokens <= 0 || snapshot.ContextWindow != 128_000 {
		t.Fatalf("snapshot context = %d/%d", snapshot.ContextTokens, snapshot.ContextWindow)
	}
	if snapshot.Usage.Input != 40_000 || snapshot.Usage.Output != 24_000 || snapshot.Usage.CacheRead != 10_000 ||
		snapshot.Usage.CacheWrite != 2_000 || snapshot.Usage.Reasoning != 5_000 || snapshot.Usage.TotalTokens != 64_000 ||
		math.Abs(snapshot.Usage.Cost.Total-0.095) > 1e-9 {
		t.Fatalf("snapshot cumulative usage = %+v", snapshot.Usage)
	}
	if len(snapshot.PromptCommands) != 1 || snapshot.PromptCommands[0] != (protocol.PromptCommand{
		Name: "summarize", Description: "Summarize a topic", Source: "project", Location: projectPromptLocation,
	}) {
		t.Fatalf("snapshot prompt commands = %#v", snapshot.PromptCommands)
	}
	providers.mu.Lock()
	providerRequest := providers.requests[0]
	providers.mu.Unlock()
	for _, expected := range []string{"kit-customization", "project-check", "Initial project skill", projectSkillLocation, "daemon-global-guidance", "daemon-project-guidance"} {
		if !strings.Contains(providerRequest.SystemPrompt, expected) {
			t.Fatalf("daemon provider prompt does not contain %q:\n%s", expected, providerRequest.SystemPrompt)
		}
	}
	eventBatch, err := client.GetSessionEvents(context.Background(), created.ID, "", 0)
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}
	wantEventKinds := []protocol.SessionEventKind{
		protocol.SessionEventRunStarted,
		protocol.SessionEventUserMessage,
		protocol.SessionEventAssistantStarted,
		protocol.SessionEventAssistantCompleted,
		protocol.SessionEventUsageUpdated,
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
	if eventBatch.Events[3].Kind != protocol.SessionEventAssistantCompleted || eventBatch.Events[4].Usage == nil ||
		*eventBatch.Events[4].Usage != snapshot.Usage || eventBatch.Events[5].Status != protocol.RunStatusCompleted {
		t.Errorf("terminal session events = %+v", eventBatch.Events[3:])
	}

	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("daemon-reloaded-guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectSkillPath, []byte("---\nname: project-check\ndescription: Reloaded project skill\n---\nUpdated instructions.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidSkillDirectory := filepath.Join(workspace, ".agents", "skills", "invalid-skill")
	if err := os.MkdirAll(invalidSkillDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidSkillDirectory, "SKILL.md"), []byte("---\nname: invalid-skill\n---\nMissing description.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded, err := client.ReloadSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("ReloadSession() error = %v", err)
	}
	if reloaded.SessionID != created.ID || reloaded.EventStreamID == snapshot.EventStreamID || len(reloaded.Sources) < 4 {
		t.Fatalf("reload result = %+v", reloaded)
	}
	foundSkillDiagnostic := false
	for _, diagnostic := range reloaded.Diagnostics {
		foundSkillDiagnostic = foundSkillDiagnostic || diagnostic.Code == "skills.invalid_definition" && diagnostic.Source.Kind == protocol.PromptSectionSkillCatalog
	}
	if !foundSkillDiagnostic {
		t.Fatalf("reload omitted skill diagnostics: %+v", reloaded.Diagnostics)
	}
	afterReload, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterReload.EventStreamID != reloaded.EventStreamID || len(afterReload.Messages) != len(snapshot.Messages) ||
		afterReload.Messages[0].ID != snapshot.Messages[0].ID || afterReload.Usage != snapshot.Usage {
		t.Fatalf("snapshot after reload = %+v", afterReload)
	}

	block := make(chan struct{})
	requestStarted := make(chan struct{})
	providers.mu.Lock()
	providers.block = block
	providers.requestStarted = requestStarted
	providers.mu.Unlock()
	activeRunID, err := identifier.New("run_")
	if err != nil {
		t.Fatalf("identifier.New() error = %v", err)
	}
	activeReservation, err := client.StartPrompt(context.Background(), created.ID, "block")
	if err != nil {
		t.Fatalf("StartPrompt() blocking run error = %v", err)
	}
	activeRunID = activeReservation.RunID
	<-requestStarted
	active, err := client.GetRun(context.Background(), created.ID, activeRunID)
	if err != nil || active.Status != protocol.RunStatusRunning {
		t.Fatalf("active run = %+v, %v", active, err)
	}
	if _, err := client.ReloadSession(context.Background(), created.ID); err == nil {
		t.Fatal("ReloadSession() accepted an active session")
	} else {
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusConflict {
			t.Fatalf("active ReloadSession() error = %v", err)
		}
	}
	providers.mu.Lock()
	reloadedProviderPrompt := providers.requests[len(providers.requests)-1].SystemPrompt
	providers.mu.Unlock()
	if !strings.Contains(reloadedProviderPrompt, "Reloaded project skill") || strings.Contains(reloadedProviderPrompt, "Initial project skill") {
		t.Fatalf("reloaded provider skill catalog:\n%s", reloadedProviderPrompt)
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

	commandReservation, err := client.StartPromptCommand(context.Background(), created.ID, protocol.PromptCommandInput{
		Name: "summarize", Args: `"auth module" carefully`,
	})
	if err != nil {
		t.Fatalf("StartPromptCommand() error = %v", err)
	}
	commandDeadline := time.Now().Add(5 * time.Second)
	for {
		commandRun, runErr := client.GetRun(context.Background(), created.ID, commandReservation.RunID)
		if runErr != nil {
			t.Fatal(runErr)
		}
		if commandRun.Status == protocol.RunStatusCompleted {
			break
		}
		if time.Now().After(commandDeadline) {
			t.Fatalf("prompt command did not complete: %+v", commandRun)
		}
		time.Sleep(10 * time.Millisecond)
	}
	commandSnapshot, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	commandUser := commandSnapshot.Messages[len(commandSnapshot.Messages)-2]
	if commandUser.Role != "user" || commandUser.TextContent() != "Summarize auth module with auth module carefully." {
		t.Fatalf("expanded prompt command message = %+v", commandUser)
	}

	bashID, err := identifier.New("bash_")
	if err != nil {
		t.Fatalf("identifier.New() bash error = %v", err)
	}
	bash, err := client.StartBash(context.Background(), created.ID, protocol.BashExecutionInput{
		ExecutionID: bashID, Command: "printf api-bash",
	})
	if err != nil || bash.Status != protocol.BashExecutionRunning {
		t.Fatalf("StartBash() = %+v, %v", bash, err)
	}
	bashDeadline := time.Now().Add(5 * time.Second)
	for bash.Status == protocol.BashExecutionRunning {
		bash, err = client.GetBash(context.Background(), created.ID, bashID)
		if err != nil {
			t.Fatalf("GetBash() error = %v", err)
		}
		if time.Now().After(bashDeadline) {
			t.Fatalf("bash did not complete: %+v", bash)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bash.Status != protocol.BashExecutionCompleted || bash.Output != "api-bash" {
		t.Fatalf("completed bash = %+v", bash)
	}
	bashSnapshot, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil || len(bashSnapshot.PendingBoundaries) != 1 || bashSnapshot.PendingBoundaries[0].ID != bashID {
		t.Fatalf("bash snapshot = %+v, %v", bashSnapshot, err)
	}
	sessions, err := client.ListSessions(context.Background(), workspace)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != created.ID {
		t.Fatalf("sessions = %+v", sessions)
	}

	temporaryID, err := identifier.New("session_")
	if err != nil {
		t.Fatal(err)
	}
	temporary, err := client.CreateSession(context.Background(), protocol.CreateSessionInput{
		ID: temporaryID, CWD: workspace, Model: "test/echo", Temporary: true,
	})
	if err != nil || temporary.ID != temporaryID {
		t.Fatalf("CreateSession(temporary) = %+v, %v", temporary, err)
	}
	sessions, err = client.ListSessions(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range sessions {
		if listed.ID == temporaryID {
			t.Fatalf("temporary session appeared in saved directory: %+v", sessions)
		}
	}
	temporaryOutcome, err := client.RunPrompt(context.Background(), temporaryID, "temporary")
	if err != nil || temporaryOutcome.Status != protocol.RunStatusCompleted {
		t.Fatalf("RunPrompt(temporary) = %+v, %v", temporaryOutcome, err)
	}
	if matches, err := filepath.Glob(filepath.Join(paths.Droids, temporaryID+".db*")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary droid files = %v, %v", matches, err)
	}
	if err := client.DeleteSession(context.Background(), temporaryID); err == nil {
		t.Fatal("DeleteSession archived a temporary session")
	} else {
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusBadRequest {
			t.Fatalf("DeleteSession(temporary) error = %v", err)
		}
	}
	if err := client.DisposeTemporarySession(context.Background(), temporaryID); err != nil {
		t.Fatalf("DisposeTemporarySession() = %v", err)
	}
	if err := client.DisposeTemporarySession(context.Background(), created.ID); err == nil {
		t.Fatal("DisposeTemporarySession disposed a persisted session")
	} else {
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusBadRequest {
			t.Fatalf("DisposeTemporarySession(persisted) error = %v", err)
		}
	}
	if _, err := client.GetSessionSnapshot(context.Background(), temporaryID); err == nil {
		t.Fatal("disposed temporary session remained addressable")
	} else {
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotFound {
			t.Fatalf("disposed temporary snapshot error = %v", err)
		}
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
	mu             sync.Mutex
	calls          int
	block          <-chan struct{}
	requests       []droids.Request
	requestStarted chan struct{}
}

func (p *daemonEchoProviders) ID() string             { return "test" }
func (p *daemonEchoProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *daemonEchoProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *daemonEchoProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}

func (p *daemonEchoProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "echo" || id == "test/echo"
}

func (p *daemonEchoProviders) RefreshModels(context.Context) error { return nil }

func (p *daemonEchoProviders) Stream(
	ctx context.Context,
	_ droids.Model,
	request droids.Request,
) droids.Stream {
	p.mu.Lock()
	p.calls++
	p.requests = append(p.requests, request)
	text := fmt.Sprintf("reply %d", p.calls)
	block := p.block
	requestStarted := p.requestStarted
	p.mu.Unlock()
	if requestStarted != nil {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
	}
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
		Content: []droids.AssistantContent{droids.TextContent{Text: text}},
		Usage: droids.Usage{
			Input: 40_000, Output: 24_000, CacheRead: 10_000, CacheWrite: 2_000,
			Reasoning: 5_000, TotalTokens: 64_000,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	return &daemonEchoStream{final: final}
}

func (p *daemonEchoProviders) model() droids.Model {
	return droids.Model{
		ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
		Cost: droids.Cost{Input: 1, Output: 2, CacheRead: 0.5, CacheWrite: 1},
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
