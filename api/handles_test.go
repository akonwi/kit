package kit

import (
	"context"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

type handleTransport struct {
	sessionTransport
	message             protocol.PromptInput
	promptCommand       protocol.PromptCommandInput
	promptCommandResult protocol.PromptSubmission
	abortedTurn         string
	bashAborted         string
}

func (t *handleTransport) SubmitPromptInput(_ context.Context, _ string, input protocol.PromptInput) (protocol.PromptSubmission, error) {
	t.message = input
	return protocol.PromptSubmission{Reservation: &protocol.TurnReservation{TurnID: "turn_test"}}, nil
}

func (t *handleTransport) SubmitPromptCommand(_ context.Context, _ string, input protocol.PromptCommandInput) (protocol.PromptSubmission, error) {
	t.promptCommand = input
	return t.promptCommandResult, nil
}

func (t *handleTransport) GetTurn(context.Context, string, string) (protocol.TurnInfo, error) {
	return protocol.TurnInfo{TurnID: "turn_test", Status: protocol.TurnStatusCompleted}, nil
}

func (t *handleTransport) GetSessionSnapshot(context.Context, string) (protocol.SessionSnapshot, error) {
	return protocol.SessionSnapshot{Messages: []protocol.TranscriptMessage{{
		TurnID: "turn_test", Role: "assistant", Content: []protocol.TranscriptContent{protocol.TextBlock("done")},
	}}}, nil
}

func (t *handleTransport) AbortSession(_ context.Context, _ string, turnID string) error {
	t.abortedTurn = turnID
	return nil
}

func (t *handleTransport) StartBash(_ context.Context, sessionID string, input protocol.BashExecutionInput) (protocol.BashExecution, error) {
	return protocol.BashExecution{ID: input.ExecutionID, SessionID: sessionID, Command: input.Command, Status: protocol.BashExecutionRunning}, nil
}

func (t *handleTransport) GetBash(_ context.Context, sessionID, executionID string) (protocol.BashExecution, error) {
	return protocol.BashExecution{ID: executionID, SessionID: sessionID, Status: protocol.BashExecutionCompleted}, nil
}

func (t *handleTransport) AbortBash(_ context.Context, _ string, executionID string) error {
	t.bashAborted = executionID
	return nil
}

func TestSessionMessageAndExecutionHandles(t *testing.T) {
	transport := &handleTransport{}
	session := newSession(nil, transport, "session_test", protocol.SessionSnapshot{})

	submission, err := session.SendMessage(t.Context(), Message{Text: "hello"})
	if err != nil || submission.Queued || submission.Turn == nil || submission.Turn.ID() != "turn_test" {
		t.Fatalf("submission = %+v, err = %v", submission, err)
	}
	if transport.message.Text != "hello" {
		t.Fatalf("message = %+v", transport.message)
	}

	transport.promptCommandResult = protocol.PromptSubmission{Queued: true, Queue: protocol.FollowUpQueue{Count: 1, Previews: []string{"queued"}}}
	commandSubmission, err := session.SubmitPromptCommand(t.Context(), "review", "--staged")
	if err != nil || !commandSubmission.Queued || commandSubmission.Turn != nil || commandSubmission.Queue.Count != 1 {
		t.Fatalf("command submission = %+v, err = %v", commandSubmission, err)
	}
	if transport.promptCommand.Name != "review" || transport.promptCommand.Args != "--staged" {
		t.Fatalf("prompt command = %+v", transport.promptCommand)
	}
	transport.promptCommandResult = protocol.PromptSubmission{Reservation: &protocol.TurnReservation{TurnID: "turn_command"}}
	commandSubmission, err = session.SubmitPromptCommand(t.Context(), "review", "")
	if err != nil || commandSubmission.Queued || commandSubmission.Turn == nil || commandSubmission.Turn.ID() != "turn_command" {
		t.Fatalf("started command submission = %+v, err = %v", commandSubmission, err)
	}

	outcome, err := submission.Turn.Wait(t.Context())
	if err != nil || outcome.Status != protocol.TurnStatusCompleted || outcome.Text != "done" {
		t.Fatalf("turn outcome = %+v, err = %v", outcome, err)
	}
	if err := submission.Turn.Abort(t.Context()); err != nil || transport.abortedTurn != "turn_test" {
		t.Fatalf("turn abort = %q, err = %v", transport.abortedTurn, err)
	}

	execution, err := session.RunBash(t.Context(), BashExecutionInput{ExecutionID: "bash_test", Command: "pwd"})
	if err != nil || execution.ID() != "bash_test" {
		t.Fatalf("execution = %+v, err = %v", execution, err)
	}
	state, err := execution.Wait(t.Context())
	if err != nil || state.Status != protocol.BashExecutionCompleted {
		t.Fatalf("bash state = %+v, err = %v", state, err)
	}
	if err := execution.Abort(t.Context()); err != nil || transport.bashAborted != "bash_test" {
		t.Fatalf("bash abort = %q, err = %v", transport.bashAborted, err)
	}
}
