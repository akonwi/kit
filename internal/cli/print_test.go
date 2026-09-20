package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

func TestExecutePrintResumesLatestCWDSession(t *testing.T) {
	t.Parallel()

	client := &fakeSessionClient{
		sessions: []protocol.SessionInfo{{ID: "session-1", Model: "test/echo"}},
		outcome: protocol.PromptOutcome{
			SessionID: "session-1", Status: protocol.RunStatusCompleted, Text: "hello",
		},
	}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{
		CWD: "/workspace", Prompt: "say hello",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if client.runSessionID != "session-1" || client.runPrompt != "say hello" {
		t.Fatalf("run = %q/%q", client.runSessionID, client.runPrompt)
	}
	if stdout.String() != "hello" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestExecutePrintSkipsSessionsWithoutAvailableProvider(t *testing.T) {
	t.Parallel()

	client := &fakeSessionClient{
		sessions: []protocol.SessionInfo{
			{ID: "session-unavailable", Model: "missing/model"},
			{ID: "session-available", Model: "test/echo"},
		},
		outcome: protocol.PromptOutcome{SessionID: "session-available", Status: protocol.RunStatusCompleted},
	}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{
		CWD: "/workspace", AvailableProviders: map[string]bool{"test": true}, Prompt: "go",
	}, &stdout, &stderr)
	if code != 0 || client.runSessionID != "session-available" {
		t.Fatalf("exit = %d session = %q stderr = %q", code, client.runSessionID, stderr.String())
	}
}

func TestExecutePrintRejectsUnavailableProviderBeforeCreation(t *testing.T) {
	t.Parallel()
	client := &fakeSessionClient{}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{
		CWD: "/workspace", Model: "missing/model", NewSession: true,
		AvailableProviders: map[string]bool{"test": true}, Prompt: "go",
	}, &stdout, &stderr)
	if code != 1 || client.createInput.ID != "" || !bytes.Contains(stderr.Bytes(), []byte("not authenticated")) {
		t.Fatalf("exit = %d create = %+v stderr = %q", code, client.createInput, stderr.String())
	}
}

func TestExecutePrintCreatesSessionFromAvailableCatalogWithoutDefault(t *testing.T) {
	t.Parallel()
	client := &fakeSessionClient{
		models:  protocol.ModelCatalog{Models: []protocol.ModelCapability{{ID: "test/echo", Provider: "test", Available: true}}},
		created: protocol.SessionInfo{ID: "session-new", Model: "test/echo", ThinkingLevel: "off"},
		outcome: protocol.PromptOutcome{SessionID: "session-new", Status: protocol.RunStatusCompleted},
	}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{
		CWD: "/workspace", NewSession: true, AvailableProviders: map[string]bool{"test": true}, Prompt: "go",
	}, &stdout, &stderr)
	if code != 0 || client.createInput.Model != "test/echo" || client.createInput.ThinkingLevel != "" {
		t.Fatalf("exit = %d create = %+v stderr = %q", code, client.createInput, stderr.String())
	}
}

func TestExecutePrintReplacesStaleImplicitDefaultFromSameProvider(t *testing.T) {
	t.Parallel()
	client := &fakeSessionClient{
		models:  protocol.ModelCatalog{Models: []protocol.ModelCapability{{ID: "test/current", Provider: "test", Available: true}}},
		created: protocol.SessionInfo{ID: "session-new", Model: "test/current", ThinkingLevel: "off"},
		outcome: protocol.PromptOutcome{SessionID: "session-new", Status: protocol.RunStatusCompleted},
	}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{
		CWD: "/workspace", DefaultModel: "test/deprecated", NewSession: true,
		AvailableProviders: map[string]bool{"test": true}, Prompt: "go",
	}, &stdout, &stderr)
	if code != 0 || client.createInput.Model != "test/current" {
		t.Fatalf("exit = %d create = %+v stderr = %q", code, client.createInput, stderr.String())
	}
}

func TestExecutePrintCreatesSessionForModel(t *testing.T) {
	t.Parallel()

	client := &fakeSessionClient{
		created: protocol.SessionInfo{ID: "session-new", Model: "test/echo"},
		outcome: protocol.PromptOutcome{
			SessionID: "session-new", Status: protocol.RunStatusCompleted, Text: "created",
		},
	}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{
		CWD: "/workspace", Model: "test/echo", Prompt: "go",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if client.createInput.Model != "test/echo" || client.createInput.CWD != "/workspace" {
		t.Fatalf("create input = %+v", client.createInput)
	}
	if client.runSessionID != "session-new" {
		t.Fatalf("run session = %q", client.runSessionID)
	}
}

func TestExecutePrintResolvesShortSessionID(t *testing.T) {
	t.Parallel()

	const sessionID = "session_0123456789abcdef0123456789abcdef"
	client := &fakeSessionClient{
		sessions: []protocol.SessionInfo{{ID: sessionID, Model: "test/echo"}},
		outcome:  protocol.PromptOutcome{SessionID: sessionID, Status: protocol.RunStatusCompleted, Text: "continued"},
	}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{
		SessionID: "01234567", CWD: "/workspace", Prompt: "continue",
	}, &stdout, &stderr)
	if code != 0 || client.runSessionID != sessionID {
		t.Fatalf("exit = %d session = %q stderr = %q", code, client.runSessionID, stderr.String())
	}
}

func TestExecutePrintDisposesTemporarySession(t *testing.T) {
	t.Parallel()

	client := &fakeSessionClient{
		created: protocol.SessionInfo{ID: "session_temporary", Model: "test/echo"},
		outcome: protocol.PromptOutcome{SessionID: "session_temporary", Status: protocol.RunStatusCompleted, Text: "temporary"},
	}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{
		CWD: "/workspace", Model: "test/echo", Temporary: true, Prompt: "go",
	}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit = %d stderr = %q", code, stderr.String())
	}
	if !client.createInput.Temporary || client.createInput.ID == "" || client.deleted != client.createInput.ID {
		t.Fatalf("temporary create = %+v deleted = %q", client.createInput, client.deleted)
	}
}

func TestExecutePrintKeepsFailuresOffStdout(t *testing.T) {
	t.Parallel()

	client := &fakeSessionClient{
		sessions: []protocol.SessionInfo{{ID: "session-1", Model: "test/echo"}},
		outcome:  protocol.PromptOutcome{SessionID: "session-1", Status: protocol.RunStatusFailed, ErrorMessage: "provider failed"},
	}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{CWD: "/workspace", Prompt: "go"}, &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 || !bytes.Contains(stderr.Bytes(), []byte("provider failed")) {
		t.Fatalf("exit = %d stdout = %q stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestExecutePrintDisposesAmbiguousTemporaryCreate(t *testing.T) {
	t.Parallel()

	client := &fakeSessionClient{createErr: errors.New("response lost")}
	var stdout, stderr bytes.Buffer
	code := executePrint(context.Background(), client, printOptions{
		CWD: "/workspace", Model: "test/echo", Temporary: true, Prompt: "go",
	}, &stdout, &stderr)
	if code != 1 || client.createInput.ID == "" || client.deleted != client.createInput.ID {
		t.Fatalf("exit = %d create = %+v disposed = %q stderr = %q", code, client.createInput, client.deleted, stderr.String())
	}
}

func TestExecutePrintAbortsCanceledForegroundRun(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeSessionClient{
		sessions: []protocol.SessionInfo{{ID: "session-1", Model: "test/echo"}},
		onRun:    cancel,
	}
	var stdout, stderr bytes.Buffer
	code := executePrint(ctx, client, printOptions{
		CWD: "/workspace", Prompt: "wait",
	}, &stdout, &stderr)
	if code != 130 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !client.aborted {
		t.Fatal("foreground cancellation did not abort bound session")
	}
}

type fakeSessionClient struct {
	sessions     []protocol.SessionInfo
	models       protocol.ModelCatalog
	created      protocol.SessionInfo
	outcome      protocol.PromptOutcome
	createInput  protocol.CreateSessionInput
	createErr    error
	runSessionID string
	runPrompt    string
	onRun        func()
	aborted      bool
	deleted      string
}

func (c *fakeSessionClient) CreateSession(
	_ context.Context,
	input protocol.CreateSessionInput,
) (protocol.SessionInfo, error) {
	c.createInput = input
	return c.created, c.createErr
}

func (c *fakeSessionClient) ForkSession(context.Context, string, protocol.ForkSessionInput) (protocol.SessionInfo, error) {
	panic("unexpected ForkSession")
}

func (c *fakeSessionClient) RenameSession(context.Context, string, string) (protocol.SessionInfo, error) {
	panic("unexpected RenameSession")
}

func (c *fakeSessionClient) DeleteSession(context.Context, string) error {
	panic("unexpected DeleteSession")
}

func (c *fakeSessionClient) DisposeTemporarySession(_ context.Context, sessionID string) error {
	c.deleted = sessionID
	return nil
}

func (c *fakeSessionClient) ListSessions(context.Context, string) ([]protocol.SessionInfo, error) {
	return c.sessions, nil
}

func (c *fakeSessionClient) Models(context.Context) (protocol.ModelCatalog, error) {
	if c.models.Models == nil {
		return protocol.ModelCatalog{Models: []protocol.ModelCapability{{ID: "test/echo", Provider: "test", Available: true}}}, nil
	}
	return c.models, nil
}

func (c *fakeSessionClient) Attach(_ context.Context, sessionID string) (sessionclient.Session, error) {
	c.runSessionID = sessionID
	return &fakeBoundSession{server: c}, nil
}

type fakeBoundSession struct {
	server *fakeSessionClient
}

func (c *fakeBoundSession) ID() string { return c.server.runSessionID }

func (c *fakeBoundSession) Subagent(context.Context, protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error) {
	panic("unexpected Subagent")
}

func (c *fakeBoundSession) SubagentTranscript(context.Context, string) (protocol.SubagentTranscript, error) {
	panic("unexpected SubagentTranscript")
}

func (c *fakeBoundSession) Snapshot(context.Context) (protocol.SessionSnapshot, error) {
	return protocol.SessionSnapshot{}, nil
}

func (c *fakeBoundSession) FileIndex(context.Context) (protocol.SessionFileIndex, error) {
	return protocol.SessionFileIndex{SessionID: c.ID(), CWD: "/tmp", Entries: []protocol.FileIndexEntry{}}, nil
}

func (c *fakeBoundSession) VCSStatus(context.Context) (protocol.SessionVCSStatus, error) {
	return protocol.SessionVCSStatus{}, nil
}

func (c *fakeBoundSession) ChangeCWD(context.Context, string) (protocol.SessionInfo, error) {
	return protocol.SessionInfo{}, errors.New("unexpected cwd change")
}

func (c *fakeBoundSession) Reload(context.Context) (protocol.ReloadSessionResult, error) {
	panic("unexpected Reload")
}

func (c *fakeBoundSession) Configure(context.Context, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
	panic("unexpected Configure")
}

func (c *fakeBoundSession) Compact(context.Context, protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
	panic("unexpected Compact")
}

func (c *fakeBoundSession) Run(context.Context, string) (protocol.RunInfo, error) {
	return protocol.RunInfo{}, nil
}

func (c *fakeBoundSession) Stream(context.Context, string) (sessionclient.EventStream, error) {
	panic("unexpected Stream")
}

func (c *fakeBoundSession) Abort(context.Context, string) error { return nil }

func (c *fakeBoundSession) AbortBash(context.Context, string) error { return nil }

func (c *fakeBoundSession) Bash(context.Context, string) (sessionclient.BashExecution, error) {
	panic("unexpected Bash")
}

func (c *fakeBoundSession) StartBash(context.Context, string, string, bool) (sessionclient.BashExecution, error) {
	panic("unexpected StartBash")
}

func (c *fakeBoundSession) StartPrompt(
	_ context.Context,
	prompt string,
) (sessionclient.Run, error) {
	c.server.runPrompt = prompt
	return &fakeRun{server: c.server}, nil
}

func (c *fakeBoundSession) StartPromptCommand(context.Context, string, string) (sessionclient.Run, error) {
	panic("unexpected StartPromptCommand")
}

type fakeRun struct {
	server *fakeSessionClient
}

func (r *fakeRun) ID() string { return "run_test" }

func (r *fakeRun) Wait(ctx context.Context) (protocol.PromptOutcome, error) {
	if r.server.onRun != nil {
		r.server.onRun()
	}
	if err := ctx.Err(); err != nil {
		return protocol.PromptOutcome{}, err
	}
	return r.server.outcome, nil
}

func (r *fakeRun) Abort(context.Context) error {
	r.server.aborted = true
	return nil
}
