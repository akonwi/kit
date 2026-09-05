package protocol

import (
	"testing"
	"time"
)

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

	valid := RunInfo{SessionID: "session", TurnID: "turn", RunID: "run", Status: RunStatusRunning}
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
		SessionID: "session", TurnID: "turn", RunID: "run",
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
