package cli

import (
	"bytes"
	"context"
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
	created      protocol.SessionInfo
	outcome      protocol.PromptOutcome
	createInput  protocol.CreateSessionInput
	runSessionID string
	runPrompt    string
	onRun        func()
	aborted      bool
}

func (c *fakeSessionClient) CreateSession(
	_ context.Context,
	input protocol.CreateSessionInput,
) (protocol.SessionInfo, error) {
	c.createInput = input
	return c.created, nil
}

func (c *fakeSessionClient) ListSessions(context.Context, string) ([]protocol.SessionInfo, error) {
	return c.sessions, nil
}

func (c *fakeSessionClient) Attach(_ context.Context, sessionID string) (sessionclient.Session, error) {
	c.runSessionID = sessionID
	return &fakeBoundSession{server: c}, nil
}

type fakeBoundSession struct {
	server *fakeSessionClient
}

func (c *fakeBoundSession) ID() string { return c.server.runSessionID }

func (c *fakeBoundSession) StartPrompt(
	_ context.Context,
	prompt string,
) (sessionclient.Run, error) {
	c.server.runPrompt = prompt
	return &fakeRun{server: c.server}, nil
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
