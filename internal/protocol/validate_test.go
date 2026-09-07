package protocol

import (
	"strings"
	"testing"
	"time"
)

func TestCreateSessionInputValidateOptionalCanonicalID(t *testing.T) {
	t.Parallel()

	if err := (CreateSessionInput{}).Validate(); err != nil {
		t.Fatalf("Validate() generated-id request error = %v", err)
	}
	if err := (CreateSessionInput{ID: "session_0123456789abcdef0123456789abcdef"}).Validate(); err != nil {
		t.Fatalf("Validate() canonical id error = %v", err)
	}
	if err := (CreateSessionInput{ID: "session_not-canonical"}).Validate(); err == nil {
		t.Fatal("Validate() accepted a non-canonical session id")
	}
	if err := (CreateSessionInput{Temporary: true}).Validate(); err == nil {
		t.Fatal("Validate() accepted a temporary session without a client-selected id")
	}
	if err := (CreateSessionInput{ID: "session_0123456789abcdef0123456789abcdef", Temporary: true}).Validate(); err != nil {
		t.Fatalf("Validate() temporary session error = %v", err)
	}
}

func TestPromptInputValidate(t *testing.T) {
	t.Parallel()
	if err := (PromptInput{Text: "hello"}).Validate(); err != nil {
		t.Fatalf("Validate() prompt error = %v", err)
	}
	for _, text := range []string{"", "   ", "bad\x00prompt", strings.Repeat("<", (128<<10)+1)} {
		if err := (PromptInput{Text: text}).Validate(); err == nil {
			t.Fatalf("Validate() accepted prompt of %d bytes", len(text))
		}
	}
}

func TestRenameSessionInputValidate(t *testing.T) {
	t.Parallel()

	if err := (RenameSessionInput{Name: " Renamed session "}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for _, name := range []string{"", "   ", "bad\x00name", strings.Repeat("a", 257)} {
		if err := (RenameSessionInput{Name: name}).Validate(); err == nil {
			t.Fatalf("Validate() accepted name %q", name)
		}
	}
}

func TestBashExecutionValidate(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	running := BashExecution{
		ID: "bash_1", SessionID: "session_1", Command: "git status",
		Status: BashExecutionRunning, StartedAt: now,
	}
	if err := running.Validate(); err != nil {
		t.Fatalf("Validate() running error = %v", err)
	}
	completed := running
	completed.Status = BashExecutionCompleted
	completed.CompletedAt = now
	exitCode := 0
	completed.ExitCode = &exitCode
	if err := completed.Validate(); err != nil {
		t.Fatalf("Validate() completed error = %v", err)
	}
	invalid := running
	invalid.Output = "premature"
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() accepted terminal output on a running execution")
	}
	invalid = completed
	invalid.Status = BashExecutionAborted
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() accepted aborted execution without an error")
	}
}

func TestRunInfoValidate(t *testing.T) {
	t.Parallel()

	valid := RunInfo{SessionID: "session", TurnID: "turn", RunID: "turn", Status: RunStatusRunning}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	terminal := valid
	terminal.Status = RunStatusAborted
	if err := terminal.Validate(); err == nil {
		t.Fatal("Validate() accepted aborted run without an error message")
	}
	terminal.ErrorMessage = "aborted"
	if err := terminal.Validate(); err != nil {
		t.Fatalf("Validate() rejected terminal run: %v", err)
	}
}

func TestPromptOutcomeValidate(t *testing.T) {
	t.Parallel()

	valid := PromptOutcome{
		SessionID: "session", TurnID: "turn", RunID: "turn",
		Status: RunStatusCompleted,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	invalid := valid
	invalid.Status = RunStatus("running")
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() accepted non-terminal status")
	}
	invalid = valid
	invalid.ErrorKind = ProviderErrorAuthentication
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() accepted an error kind on a completed outcome")
	}
	invalid.Status = RunStatusFailed
	invalid.ErrorMessage = "failed"
	invalid.ErrorKind = ProviderErrorKind("unknown")
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() accepted an unknown provider error kind")
	}
	invalid.ErrorKind = ProviderErrorAuthentication
	if err := invalid.Validate(); err != nil {
		t.Fatalf("Validate() rejected a known provider error kind: %v", err)
	}
}
