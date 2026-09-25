package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/scratchpad"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/version"
)

type eventStreamTestService struct {
	sessionService
	batch protocol.SessionEventBatch
	err   error
}

func (service eventStreamTestService) Events(context.Context, string, string, int64) (protocol.SessionEventBatch, error) {
	return service.batch, service.err
}

func (eventStreamTestService) WaitEvents(ctx context.Context, _ string, _ string, _ int64) (protocol.SessionEventBatch, error) {
	return protocol.SessionEventBatch{}, ctx.Err()
}

func TestProjectedScratchpadEventPageUsesWireByteLimit(t *testing.T) {
	t.Parallel()
	record := scratchpad.Record{
		OwnerSessionID: "session_0123456789abcdef0123456789abcdef",
		Content:        strings.Repeat("x", scratchpad.MaxContentBytes), Revision: 1, UpdatedAt: time.Now().UTC(),
	}
	page := kitsession.EventPage{StreamID: "stream_test", FirstSequence: 1, LastSequence: 10}
	for sequence := int64(1); sequence <= 10; sequence++ {
		copy := record
		copy.Revision = sequence
		page.Events = append(page.Events, kitsession.Event{
			NewEvent: kitsession.NewEvent{SessionID: "session_child", Kind: kitsession.EventScratchpadChanged, Scratchpad: &copy},
			StreamID: page.StreamID, Sequence: sequence,
		})
	}
	projected := projectSessionEventPage(page)
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxSessionEventPageBytes || len(projected.Events) == 0 || len(projected.Events) >= len(page.Events) {
		t.Fatalf("projected events = %d bytes=%d", len(projected.Events), len(encoded))
	}
	if err := projected.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeScratchpadErrorPreservesCancellation(t *testing.T) {
	t.Parallel()
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		if normalized := normalizeScratchpadError(err); !errors.Is(normalized, err) {
			t.Fatalf("normalizeScratchpadError(%v) = %v", err, normalized)
		}
	}
}

func TestWriteSessionErrorProjectsStableScratchpadFailures(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   protocol.ScratchpadErrorCode
	}{
		{"invalid", scratchpad.ErrInvalidContent, http.StatusBadRequest, protocol.ScratchpadInvalidContent},
		{"too large", scratchpad.ErrContentTooLarge, http.StatusRequestEntityTooLarge, protocol.ScratchpadTooLarge},
		{"exhausted", scratchpad.ErrRevisionExhausted, http.StatusConflict, protocol.ScratchpadRevisionExhausted},
		{"migration", scratchpad.ErrMigrationRequired, http.StatusConflict, protocol.ScratchpadMigrationRequired},
		{"unsupported", scratchpad.ErrUnsupported, http.StatusConflict, protocol.ScratchpadUnsupported},
		{"unavailable", scratchpad.ErrUnavailable, http.StatusServiceUnavailable, protocol.ScratchpadUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeSessionError(recorder, test.err)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
			var apiError *APIError
			if err := decodeAPIError(recorder.Code, recorder.Body.Bytes()); !errors.As(err, &apiError) || apiError.Code != string(test.code) {
				t.Fatalf("decoded error = %#v", err)
			}
		})
	}
}

func TestRuntimeSessionServiceWorkspaceUnavailableWithoutService(t *testing.T) {
	t.Parallel()
	_, err := (runtimeSessionService{}).Workspace(t.Context(), "session_test")
	var workspaceErr *protocol.WorkspaceError
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != protocol.WorkspaceErrorUnavailable {
		t.Fatalf("workspace error = %v", err)
	}
}

func TestSessionEventStreamValidatesBeforeCommittingResponse(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	registerSessionRoutes(mux, eventStreamTestService{err: kitsession.ErrNotFound})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/session_missing/events/stream", nil)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNotFound, response.Body.String())
	}
	if strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content type = %q, want ordinary error response", response.Header().Get("Content-Type"))
	}
}

func TestSessionEventStreamWritesResynchronizationRecord(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	registerSessionRoutes(mux, eventStreamTestService{batch: protocol.SessionEventBatch{StreamID: "stream_00000000000000000000000000000002", ResyncRequired: true}})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/session_test/events/stream?stream=stream_00000000000000000000000000000001&after=4", nil)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", contentType)
	}
	if body := response.Body.String(); !strings.Contains(body, "event: session.resync\n") || !strings.Contains(body, `"resyncRequired":true`) {
		t.Fatalf("SSE body = %q", body)
	}
}

func TestSessionEventCursorPrefersLastEventID(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "/v1/sessions/session_test/events/stream?stream=stream_00000000000000000000000000000001&after=2", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Last-Event-ID", "stream_00000000000000000000000000000002:42")
	streamID, after, err := sessionEventCursor(request)
	if err != nil {
		t.Fatal(err)
	}
	if streamID != "stream_00000000000000000000000000000002" || after != 42 {
		t.Fatalf("cursor = %q:%d, want canonical stream cursor at 42", streamID, after)
	}
}

func TestSessionEventCursorRejectsMissingLastEventIDSequence(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "/v1/sessions/session_test/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Last-Event-ID", "stream_test:")
	if _, _, err := sessionEventCursor(request); err == nil {
		t.Fatal("sessionEventCursor accepted an empty Last-Event-ID sequence")
	}
}

func TestSessionEventCursorRejectsMalformedStreamIdentity(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/session_test/events/stream", nil)
	request.Header.Set("Last-Event-ID", "stream_bad:4")
	if _, _, err := sessionEventCursor(request); err == nil {
		t.Fatal("sessionEventCursor accepted a malformed stream identity")
	}
}

func TestProjectSessionSanitizesLegacyUnsafeName(t *testing.T) {
	t.Parallel()

	projected := projectSession(kitsession.SessionRecord{
		ID: "session_test", CWD: "/repo", Name: "bad\nname",
		ModelProvider: "test", ModelID: "echo", ConfigurationRevision: 1,
	})
	if projected.Name != "" {
		t.Fatalf("projected legacy session = %+v", projected)
	}
}

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

func TestRuntimeSessionServiceRejectsUnavailableModelProvider(t *testing.T) {
	service := runtimeSessionService{availableProviders: func(context.Context) []string { return []string{"available"} }}
	if _, err := service.Configure(t.Context(), "session_test", protocol.ConfigureSessionInput{
		ExpectedRevision: 1, Model: "missing/model",
	}); !errors.Is(err, kitsession.ErrInvalidInput) {
		t.Fatalf("Configure() unavailable provider error = %v", err)
	}
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

func TestLocalSessionClientProjectsSubagentDefinitions(t *testing.T) {
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Agents, "scout.md"), []byte("---\nname: scout\ndescription: inspects repositories\n---\nInspect carefully.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, RunOptions{
			Paths: paths, Providers: &daemonEchoProviders{},
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
	sessionID := "session_77777777777777777777777777777777"
	created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{
		ID: sessionID, CWD: t.TempDir(), Model: "test/echo", ThinkingLevel: "off",
	})
	if err != nil || created.ID != sessionID {
		t.Fatalf("CreateSession() = %#v, %v", created, err)
	}
	listed, err := client.Subagent(t.Context(), sessionID, protocol.SubagentOperationInput{Action: protocol.SubagentListAgents})
	if err != nil || len(listed.Definitions) != 1 || listed.Definitions[0].Name != "scout" {
		t.Fatalf("Subagent(list_agents) = %#v, %v", listed, err)
	}
	eventSnapshot, err := client.GetSessionSnapshot(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	streamContext, stopStream := context.WithCancel(context.Background())
	defer stopStream()
	body, err := observerStream(client, streamContext, sessionID, eventSnapshot.EventStreamID, eventSnapshot.EventCursor)
	if err != nil {
		t.Fatal(err)
	}
	changedEvents := make(chan protocol.SessionEvent, 1)
	go scanSubagentChangedEvent(body, changedEvents)
	started, err := client.Subagent(t.Context(), sessionID, protocol.SubagentOperationInput{
		Action: protocol.SubagentStart, Agent: "scout", Message: "inspect from another client",
	})
	if err != nil || started.Task == nil || started.Conversation == nil {
		t.Fatalf("Subagent(start) = %#v, %v", started, err)
	}
	if started.Conversation.Model != "test/echo" || started.Conversation.ThinkingLevel != "off" {
		t.Fatalf("started subagent configuration = %#v", started.Conversation)
	}
	select {
	case event := <-changedEvents:
		if event.SubagentConversationID != started.Conversation.ID || event.SubagentTaskID != started.Task.ID {
			t.Fatalf("subagent changed event = %#v", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("attached observer did not receive subagent lifecycle event")
	}
	stopStream()
	observer := NewClient(paths)
	var observed protocol.SubagentOperationResult
	deadline := time.Now().Add(5 * time.Second)
	for {
		observed, err = observer.Subagent(t.Context(), sessionID, protocol.SubagentOperationInput{Action: protocol.SubagentListAgents})
		if err == nil && len(observed.Conversations) == 1 && observed.Conversations[0].State == "idle" && observed.Conversations[0].LastResultSummary != "" && len(observed.Conversations[0].Tasks) == 1 && observed.Conversations[0].Tasks[0].State == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("observer did not see completed task: %#v, %v", observed, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if observed.Conversations[0].Tasks[0].ID != started.Task.ID || observed.Conversations[0].LastResultSummary == "" ||
		observed.Conversations[0].Model != "test/echo" || observed.Conversations[0].ThinkingLevel != "off" {
		t.Fatalf("observer roster = %#v", observed.Conversations)
	}
	childEvents, err := observer.GetSubagentEvents(t.Context(), sessionID, observed.Conversations[0].ID, "", 0)
	if err != nil || childEvents.StreamID == "" || len(childEvents.Events) == 0 {
		t.Fatalf("child live events = %#v, %v", childEvents, err)
	}
	transcript, err := observer.GetSubagentTranscript(t.Context(), sessionID, observed.Conversations[0].ID)
	if err != nil || len(transcript.Messages) != 2 || transcript.Messages[0].Role != "user" || transcript.Messages[1].Role != "assistant" {
		t.Fatalf("child transcript = %#v, %v", transcript, err)
	}
	snapshot, err := client.GetSessionSnapshot(t.Context(), sessionID)
	if err != nil || len(snapshot.SubagentDefinitions) != 1 || snapshot.SubagentDefinitions[0].Name != "scout" ||
		len(snapshot.SubagentConversations) != 1 || snapshot.SubagentConversations[0].ThinkingLevel != "off" {
		t.Fatalf("snapshot subagents = definitions:%#v conversations:%#v, %v", snapshot.SubagentDefinitions, snapshot.SubagentConversations, err)
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
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

	catalog, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(catalog.Models) != 2 || catalog.Models[0].ID != "test/echo" || !catalog.Models[0].Available ||
		catalog.Models[0].ContextWindow != 128_000 || len(catalog.Models[0].ThinkingLevels) != 1 || catalog.Models[0].ThinkingLevels[0] != protocol.ThinkingOff {
		t.Fatalf("model catalog = %+v", catalog)
	}

	workspace := t.TempDir()
	gitInit := exec.Command("git", "-C", workspace, "init", "-b", "main")
	if output, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
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
	if err := os.WriteFile(projectPromptPath, []byte("---\ndescription: Summarize a topic\nargument-hint: <topic>\n---\nSummarize $1 with $@."), 0o600); err != nil {
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
		ID: sessionID, CWD: workspace, Model: "test/echo", ThinkingLevel: "off", Name: "API test",
	}
	created, err := client.CreateSession(context.Background(), createInput)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	scratch, err := client.GetScratchpad(context.Background(), created.ID)
	if err != nil || scratch.OwnerSessionID != created.ID || scratch.Content != "" || scratch.Revision != 1 {
		t.Fatalf("GetScratchpad() = %+v, %v", scratch, err)
	}
	scratchSnapshot, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil || scratchSnapshot.Scratchpad == nil || *scratchSnapshot.Scratchpad != scratch {
		t.Fatalf("initial scratchpad snapshot = %+v, %v", scratchSnapshot.Scratchpad, err)
	}
	canceledScratchContext, cancelScratch := context.WithCancel(context.Background())
	cancelScratch()
	if _, err := client.UpdateScratchpad(canceledScratchContext, created.ID, protocol.UpdateScratchpadInput{
		ExpectedRevision: scratch.Revision, Content: "canceled",
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled UpdateScratchpad() error = %v", err)
	}
	updatedScratch, err := client.UpdateScratchpad(context.Background(), created.ID, protocol.UpdateScratchpadInput{
		ExpectedRevision: scratch.Revision, Content: "# Shared notes\n",
	})
	if err != nil || updatedScratch.OwnerSessionID != created.ID || updatedScratch.Content != "# Shared notes\n" || updatedScratch.Revision != 2 {
		t.Fatalf("UpdateScratchpad() = %+v, %v", updatedScratch, err)
	}
	scratchEvents, err := client.GetSessionEvents(context.Background(), created.ID, scratchSnapshot.EventStreamID, scratchSnapshot.EventCursor)
	if err != nil || len(scratchEvents.Events) != 1 || scratchEvents.Events[0].Kind != protocol.SessionEventScratchpadChanged ||
		scratchEvents.Events[0].Scratchpad == nil || *scratchEvents.Events[0].Scratchpad != updatedScratch {
		t.Fatalf("scratchpad events = %+v, %v", scratchEvents, err)
	}
	updatedSnapshot, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil || updatedSnapshot.Scratchpad == nil || *updatedSnapshot.Scratchpad != updatedScratch {
		t.Fatalf("updated scratchpad snapshot = %+v, %v", updatedSnapshot.Scratchpad, err)
	}
	if _, err := client.UpdateScratchpad(context.Background(), created.ID, protocol.UpdateScratchpadInput{
		ExpectedRevision: scratch.Revision, Content: "stale",
	}); err == nil {
		t.Fatal("UpdateScratchpad() accepted a stale revision")
	} else {
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusConflict ||
			apiError.Code != string(protocol.ScratchpadRevisionConflict) || apiError.CurrentScratchpad == nil || *apiError.CurrentScratchpad != updatedScratch {
			t.Fatalf("stale UpdateScratchpad() error = %#v", err)
		}
	}
	vcsStatus, err := client.GetSessionVCSStatus(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSessionVCSStatus() error = %v", err)
	}
	if vcsStatus.SessionID != created.ID || vcsStatus.CWD != workspace || vcsStatus.Status == nil ||
		vcsStatus.Status.Head.Kind != protocol.VCSHeadUnborn || vcsStatus.Status.Head.Name != "main" || !vcsStatus.Status.Dirty {
		t.Fatalf("GetSessionVCSStatus() = %+v", vcsStatus)
	}
	indexed, err := client.GetSessionFileIndex(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSessionFileIndex() error = %v", err)
	}
	if indexed.SessionID != created.ID || indexed.CWD != workspace {
		t.Fatalf("GetSessionFileIndex() identity = %+v", indexed)
	}
	foundPrompt := false
	for _, entry := range indexed.Entries {
		if entry.Path == ".agents/prompts/summarize.md" {
			foundPrompt = true
			break
		}
	}
	if !foundPrompt {
		t.Fatalf("GetSessionFileIndex() did not contain project prompt: %+v", indexed.Entries)
	}
	if err := os.WriteFile(filepath.Join(workspace, "after-index.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	cached, err := client.GetSessionFileIndex(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range cached.Entries {
		if entry.Path == "after-index.txt" {
			t.Fatal("ordinary file-index read unexpectedly bypassed the daemon cache")
		}
	}
	refreshed, err := client.RefreshSessionFileIndex(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundRefreshed := false
	for _, entry := range refreshed.Entries {
		foundRefreshed = foundRefreshed || entry.Path == "after-index.txt"
	}
	if !foundRefreshed {
		t.Fatalf("forced file-index refresh did not include new file: %+v", refreshed.Entries)
	}
	retried, err := client.CreateSession(context.Background(), createInput)
	if err != nil || retried.ID != created.ID {
		t.Fatalf("retry CreateSession() = %+v, %v; want %q", retried, err, created.ID)
	}
	renameBaseline, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := client.RenameSession(context.Background(), created.ID, " Renamed API session ")
	if err != nil || renamed.ID != created.ID || renamed.Name != "Renamed API session" {
		t.Fatalf("RenameSession() = %+v, %v", renamed, err)
	}
	renameEvents, err := client.GetSessionEvents(context.Background(), created.ID, renameBaseline.EventStreamID, renameBaseline.EventCursor)
	if err != nil || len(renameEvents.Events) != 1 || renameEvents.Events[0].Kind != protocol.SessionEventSessionRenamed || renameEvents.Events[0].SessionName != renamed.Name {
		t.Fatalf("rename events = %+v, %v", renameEvents, err)
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
		Name: "summarize", Description: "Summarize a topic", ArgumentHint: "<topic>", Source: "project", Location: projectPromptLocation,
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
	if reloaded.SessionID != created.ID || reloaded.EventStreamID != snapshot.EventStreamID || len(reloaded.Sources) < 4 {
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
	if _, err := client.ReloadSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ReloadSession() during active session: %v", err)
	}
	if _, err := client.ConfigureSession(context.Background(), created.ID, protocol.ConfigureSessionInput{
		ExpectedRevision: 1, Model: "test/echo-alt",
	}); err == nil {
		t.Fatal("ConfigureSession() accepted an active session")
	} else {
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusConflict {
			t.Fatalf("active ConfigureSession() error = %v", err)
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

	messagePage, err := client.GetMessagePage(context.Background(), created.ID, protocol.MessagePageQuery{Limit: 2, Roles: []string{"user"}})
	if err != nil || len(messagePage.Messages) != 2 || !messagePage.HasMore || messagePage.Messages[0].TextContent() != "Summarize auth module with auth module carefully." {
		t.Fatalf("GetMessagePage() = %+v, %v", messagePage, err)
	}
	if messagePage.NextCursor != strconv.FormatInt(messagePage.Messages[len(messagePage.Messages)-1].Sequence, 10) {
		t.Fatalf("GetMessagePage() cursor = %q, oldest = %d", messagePage.NextCursor, messagePage.Messages[len(messagePage.Messages)-1].Sequence)
	}
	for _, message := range messagePage.Messages {
		if message.Role != "user" {
			t.Fatalf("GetMessagePage() role = %q, want user", message.Role)
		}
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

	canceledConfigureContext, cancelConfigure := context.WithCancel(context.Background())
	cancelConfigure()
	if _, err := client.ConfigureSession(canceledConfigureContext, created.ID, protocol.ConfigureSessionInput{
		ExpectedRevision: bashSnapshot.Session.ConfigurationRevision, Model: "test/echo-alt",
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ConfigureSession() error = %v", err)
	}
	afterCanceledConfigure, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil || afterCanceledConfigure.Session.ConfigurationRevision != bashSnapshot.Session.ConfigurationRevision || afterCanceledConfigure.Session.Model != bashSnapshot.Session.Model {
		t.Fatalf("session after canceled configuration = %+v, %v", afterCanceledConfigure.Session, err)
	}
	configured, err := client.ConfigureSession(context.Background(), created.ID, protocol.ConfigureSessionInput{
		ExpectedRevision: bashSnapshot.Session.ConfigurationRevision, Model: "test/echo-alt",
	})
	if err != nil {
		t.Fatalf("ConfigureSession() error = %v", err)
	}
	if configured.Session.Model != "test/echo-alt" || configured.Session.ThinkingLevel != "off" ||
		configured.Session.ConfigurationRevision != bashSnapshot.Session.ConfigurationRevision+1 || configured.EventStreamID == bashSnapshot.EventStreamID {
		t.Fatalf("configuration result = %+v", configured)
	}
	if _, err := client.ConfigureSession(context.Background(), created.ID, protocol.ConfigureSessionInput{
		ExpectedRevision: bashSnapshot.Session.ConfigurationRevision, Model: "test/echo",
	}); err == nil {
		t.Fatal("ConfigureSession() accepted a stale revision")
	} else {
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusConflict {
			t.Fatalf("stale ConfigureSession() error = %v", err)
		}
	}
	compactInput := protocol.CompactSessionInput{OperationID: "compact_daemon_api_test"}
	compacted, err := client.CompactSession(context.Background(), created.ID, compactInput)
	if err != nil {
		t.Fatalf("CompactSession() error = %v", err)
	}
	if !compacted.Compacted || compacted.CheckpointID == "" {
		t.Fatalf("user compaction was not forced: %+v", compacted)
	}
	replayedCompact, err := client.CompactSession(context.Background(), created.ID, compactInput)
	if err != nil || replayedCompact != compacted {
		t.Fatalf("replayed CompactSession() = %+v, %v; first=%+v", replayedCompact, err, compacted)
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
	if _, err := client.GetScratchpad(context.Background(), temporaryID); err == nil {
		t.Fatal("GetScratchpad(temporary) succeeded")
	} else {
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusConflict || apiError.Code != string(protocol.ScratchpadUnsupported) {
			t.Fatalf("GetScratchpad(temporary) error = %#v", err)
		}
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

	restartContext, cancelRestart := context.WithCancel(context.Background())
	defer cancelRestart()
	restartResult := make(chan error, 1)
	go func() {
		restartResult <- Run(restartContext, RunOptions{
			Paths: paths, Providers: providers,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	restartProbeContext, cancelRestartProbe := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRestartProbe()
	for {
		if _, _, err := client.Probe(restartProbeContext); err == nil {
			break
		}
		select {
		case <-restartProbeContext.Done():
			t.Fatal("restarted daemon did not become ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	reopened, err := client.GetSessionSnapshot(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Session.Model != "test/echo-alt" || reopened.Session.ThinkingLevel != "off" || reopened.Session.ConfigurationRevision != configured.Session.ConfigurationRevision {
		t.Fatalf("reopened protocol configuration = %+v", reopened.Session)
	}
	if _, err := client.RunPrompt(context.Background(), created.ID, "after daemon restart"); err != nil {
		t.Fatal(err)
	}
	restartStopContext, cancelRestartStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRestartStop()
	if err := NewManager(paths).Stop(restartStopContext); err != nil {
		t.Fatalf("stop restarted daemon: %v", err)
	}
	select {
	case err := <-restartResult:
		if err != nil {
			t.Fatalf("restarted Run() error = %v", err)
		}
	case <-restartStopContext.Done():
		t.Fatal("restarted daemon did not stop")
	}
}

func observerStream(client *Client, ctx context.Context, sessionID, streamID string, after int64) (io.ReadCloser, error) {
	return client.StreamSessionEvents(ctx, sessionID, streamID, after)
}

func scanSubagentChangedEvent(body io.ReadCloser, found chan<- protocol.SessionEvent) {
	defer body.Close()
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var batch protocol.SessionEventBatch
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &batch) != nil {
			continue
		}
		for _, event := range batch.Events {
			if event.Kind == protocol.SessionEventSubagentChanged {
				select {
				case found <- event:
				default:
				}
				return
			}
		}
	}
}

type daemonEchoProviders struct {
	mu             sync.Mutex
	calls          int
	block          <-chan struct{}
	requests       []droids.Request
	requestStarted chan struct{}
}

func (p *daemonEchoProviders) ID() string { return "test" }
func (p *daemonEchoProviders) Models() []droids.Model {
	alternate := p.model()
	alternate.ID, alternate.Name = "echo-alt", "Echo Alternate"
	return []droids.Model{p.model(), alternate}
}
func (p *daemonEchoProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *daemonEchoProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}

func (p *daemonEchoProviders) Model(id string) (droids.Model, bool) {
	for _, model := range p.Models() {
		if id == model.ID || id == model.Provider+"/"+model.ID {
			return model, true
		}
	}
	return droids.Model{}, false
}

func (p *daemonEchoProviders) RefreshModels(context.Context) error { return nil }

func (p *daemonEchoProviders) Stream(
	ctx context.Context,
	model droids.Model,
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
				Provider: "test", Model: model.ID, StopReason: droids.StopReasonAborted,
				Timestamp: time.Now().UnixMilli(),
			}
			return &daemonEchoStream{final: final}
		}
	}
	final := droids.AssistantMessage{
		Provider: "test", Model: model.ID, StopReason: droids.StopReasonStop,
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
		ID: "echo", Name: "Echo", Provider: "test", API: droids.ModelAPIOpenAIResponses,
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

func TestProjectSessionEventPageCarriesProviderRetryLifecycle(t *testing.T) {
	t.Parallel()

	retryAt := time.Date(2026, time.January, 2, 3, 4, 5, 6, time.UTC)
	page := kitsession.EventPage{
		StreamID: "stream_test", FirstSequence: 1, LastSequence: 2,
		Events: []kitsession.Event{
			{StreamID: "stream_test", Sequence: 1, NewEvent: kitsession.NewEvent{
				SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test",
				Kind: kitsession.EventProviderRetryScheduled, ProviderRetry: &kitsession.ProviderRetry{Count: 1, RetryAt: retryAt},
			}},
			{StreamID: "stream_test", Sequence: 2, NewEvent: kitsession.NewEvent{
				SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test",
				Kind: kitsession.EventProviderRetryStarted, ProviderRetry: &kitsession.ProviderRetry{Count: 1},
			}},
		},
	}
	projected := projectSessionEventPage(page)
	if err := projected.Validate(); err != nil {
		t.Fatalf("projected page Validate() error = %v", err)
	}
	if got := projected.Events[0].ProviderRetry; got == nil || got.Count != 1 || got.RetryAt != retryAt.Format(time.RFC3339Nano) {
		t.Fatalf("scheduled provider retry = %+v", got)
	}
	if got := projected.Events[1].ProviderRetry; got == nil || got.Count != 1 || got.RetryAt != "" {
		t.Fatalf("started provider retry = %+v", got)
	}
}
