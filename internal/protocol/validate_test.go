package protocol

import "testing"

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
