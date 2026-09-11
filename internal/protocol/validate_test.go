package protocol

import (
	"strings"
	"testing"
	"time"
)

func TestCreateSessionInputValidateOptionalCanonicalID(t *testing.T) {
	t.Parallel()

	if err := (CreateSessionInput{Model: "test/model"}).Validate(); err != nil {
		t.Fatalf("Validate() generated-id request error = %v", err)
	}
	if err := (CreateSessionInput{ID: "session_0123456789abcdef0123456789abcdef", Model: "test/model"}).Validate(); err != nil {
		t.Fatalf("Validate() canonical id error = %v", err)
	}
	if err := (CreateSessionInput{ID: "session_not-canonical", Model: "test/model"}).Validate(); err == nil {
		t.Fatal("Validate() accepted a non-canonical session id")
	}
	if err := (CreateSessionInput{Temporary: true, Model: "test/model"}).Validate(); err == nil {
		t.Fatal("Validate() accepted a temporary session without a client-selected id")
	}
	if err := (CreateSessionInput{ID: "session_0123456789abcdef0123456789abcdef", Temporary: true, Model: "test/model"}).Validate(); err != nil {
		t.Fatalf("Validate() temporary session error = %v", err)
	}
	if err := (CreateSessionInput{Name: "bad\nname", Model: "test/model"}).Validate(); err == nil {
		t.Fatal("Validate() accepted a renderer-unsafe session name")
	}
}

func TestModelAndConfigurationValidation(t *testing.T) {
	catalog := ModelCatalog{Models: []ModelCapability{{
		ID: "test/model", Name: "Model", Provider: "test", API: "openai-responses",
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
		ThinkingLevels: []ThinkingLevel{ThinkingOff, ThinkingMedium, ThinkingHigh},
		Inputs:         []ModelInputKind{ModelInputText, ModelInputImage}, Available: true,
	}}}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("model catalog Validate() error = %v", err)
	}
	chatCatalog := catalog
	chatCatalog.Models = append([]ModelCapability(nil), catalog.Models...)
	chatCatalog.Models[0].API = "openai-chat-completions"
	if err := chatCatalog.Validate(); err != nil {
		t.Fatalf("Chat Completions model catalog Validate() error = %v", err)
	}
	if err := (ModelCatalog{}).Validate(); err != nil {
		t.Fatalf("empty model catalog Validate() error = %v", err)
	}
	invalidCatalog := catalog
	invalidCatalog.Models = append([]ModelCapability(nil), catalog.Models...)
	invalidCatalog.Models[0].Inputs = []ModelInputKind{"audio"}
	if err := invalidCatalog.Validate(); err == nil {
		t.Fatal("model catalog accepted an invalid input capability")
	}
	invalidCatalog.Models[0] = catalog.Models[0]
	invalidCatalog.Models[0].ID = "test/bad\nmodel"
	if err := invalidCatalog.Validate(); err == nil {
		t.Fatal("model catalog accepted renderer-unsafe model identity")
	}
	level := ThinkingHigh
	input := ConfigureSessionInput{ExpectedRevision: 2, Model: "test/model", ThinkingLevel: &level}
	if err := input.Validate(); err != nil {
		t.Fatalf("configuration input Validate() error = %v", err)
	}
	badLevel := ThinkingLevel("extreme")
	input.ThinkingLevel = &badLevel
	if err := input.Validate(); err == nil {
		t.Fatal("configuration input accepted an invalid thinking level")
	}
	if err := (ConfigureSessionInput{ExpectedRevision: 2, Model: "model"}).Validate(); err == nil {
		t.Fatal("configuration input accepted a model alias")
	}
	if err := (CompactSessionInput{OperationID: "compact_test"}).Validate(); err != nil {
		t.Fatalf("compaction input Validate() error = %v", err)
	}
	if err := (CompactSessionInput{OperationID: "bad\noperation"}).Validate(); err == nil {
		t.Fatal("compaction input accepted control text")
	}
	if err := (CompactSessionResult{OperationID: "compact_test", Compacted: true, EventStreamID: "stream_0123456789abcdef0123456789abcdef"}).Validate(); err == nil {
		t.Fatal("compacted result accepted a missing checkpoint")
	}
	compactionResult := CompactSessionResult{OperationID: "compact_other", EventStreamID: "stream_0123456789abcdef0123456789abcdef"}
	if err := compactionResult.ValidateApplied(CompactSessionInput{OperationID: "compact_test"}); err == nil {
		t.Fatal("compaction result accepted mismatched operation identity")
	}
	configured := ConfigureSessionResult{
		Session: SessionInfo{
			ID: "session_0123456789abcdef0123456789abcdef", CWD: "/tmp", Model: "test/other", ThinkingLevel: "high", ConfigurationRevision: 3,
			CreatedAt: "2026-01-02T03:04:05Z", UpdatedAt: "2026-01-02T03:04:05Z",
		},
		EventStreamID: "stream_0123456789abcdef0123456789abcdef",
	}
	level = ThinkingMedium
	if err := configured.ValidateApplied(ConfigureSessionInput{ExpectedRevision: 2, Model: "test/model", ThinkingLevel: &level}); err == nil {
		t.Fatal("configuration result accepted mismatched applied values")
	}
}

func TestSessionVCSStatusValidate(t *testing.T) {
	t.Parallel()
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	const oid = "0123456789abcdef0123456789abcdef01234567"
	valid := SessionVCSStatus{
		SessionID: sessionID, CWD: "/repo",
		Status: &VCSStatus{Root: "/repo", Head: VCSHead{Kind: VCSHeadBranch, Name: "main"}, Dirty: true},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	withoutRepository := SessionVCSStatus{SessionID: sessionID, CWD: "/tmp"}
	if err := withoutRepository.Validate(); err != nil {
		t.Fatalf("Validate(nil status) error = %v", err)
	}
	invalid := []SessionVCSStatus{
		{},
		{SessionID: sessionID, CWD: "relative"},
		{SessionID: sessionID, CWD: "/repo", Status: &VCSStatus{Root: "relative", Head: VCSHead{Kind: VCSHeadBranch, Name: "main"}}},
		{SessionID: sessionID, CWD: "/repo", Status: &VCSStatus{Root: "/repo", Head: VCSHead{Kind: VCSHeadBranch, Name: "bad\nbranch"}}},
		{SessionID: sessionID, CWD: "/repo", Status: &VCSStatus{Root: "/repo", Head: VCSHead{Kind: VCSHeadDetached, OID: "short"}}},
		{SessionID: sessionID, CWD: "/repo", Status: &VCSStatus{Root: "/repo", Head: VCSHead{Kind: VCSHeadDetached, Name: "main", OID: oid}}},
	}
	for index, result := range invalid {
		if err := result.Validate(); err == nil {
			t.Fatalf("invalid VCS status %d passed validation: %+v", index, result)
		}
	}
}

func TestChangeCWDInputValidate(t *testing.T) {
	mutationID := "cwd_0123456789abcdef0123456789abcdef"
	for _, input := range []ChangeCWDInput{{MutationID: mutationID, Path: "../other"}, {MutationID: mutationID, Path: "~"}, {MutationID: mutationID, Path: "/tmp/project"}} {
		if err := input.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v", input.Path, err)
		}
	}
	for _, input := range []ChangeCWDInput{{}, {MutationID: "cwd_bad", Path: "/tmp"}, {MutationID: mutationID, Path: "  "}, {MutationID: mutationID, Path: "bad\x00path"}, {MutationID: mutationID, Path: "bad\npath"}, {MutationID: mutationID, Path: "bad\u202epath"}} {
		if err := input.Validate(); err == nil {
			t.Fatalf("Validate(%q) succeeded", input.Path)
		}
	}
}

func TestReloadSessionResultValidate(t *testing.T) {
	t.Parallel()
	valid := validReloadResult()
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	tests := []ReloadSessionResult{
		{},
		func() ReloadSessionResult {
			value := validReloadResult()
			value.SessionID = "session_bad"
			return value
		}(),
		func() ReloadSessionResult {
			value := validReloadResult()
			value.EventStreamID = "stream_bad"
			return value
		}(),
		func() ReloadSessionResult { value := validReloadResult(); value.Sources = nil; return value }(),
		func() ReloadSessionResult {
			value := validReloadResult()
			value.Sources[0].Kind = "invalid"
			return value
		}(),
		func() ReloadSessionResult {
			value := validReloadResult()
			value.Sources[1].Path = "relative"
			return value
		}(),
		func() ReloadSessionResult {
			value := validReloadResult()
			value.Sources = append(value.Sources, value.Sources[0])
			return value
		}(),
		func() ReloadSessionResult {
			value := validReloadResult()
			value.Diagnostics[0].Severity = "error"
			return value
		}(),
	}
	for index, result := range tests {
		if err := result.Validate(); err == nil {
			t.Fatalf("invalid reload result %d passed validation: %#v", index, result)
		}
	}
}

func validReloadResult() ReloadSessionResult {
	return ReloadSessionResult{
		SessionID:     "session_0123456789abcdef0123456789abcdef",
		EventStreamID: "stream_0123456789abcdef0123456789abcdef",
		Sources: []PromptSource{
			{SectionID: "kit.core", ID: "kit.core", Kind: PromptSectionCore},
			{SectionID: "kit.context", ID: "context-file:id", Kind: PromptSectionContext, Path: "/repo/AGENTS.md"},
		},
		Diagnostics: []PromptDiagnostic{{
			Severity: "warning", Code: "context.unreadable", Message: "Could not read context",
			Source: PromptSource{SectionID: "kit.context", ID: "context-file:missing", Kind: PromptSectionContext, Path: "/repo/nested/AGENTS.md"},
		}},
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
	for _, name := range []string{"", "   ", "bad\x00name", "bad\nname", "bad\u202ename", strings.Repeat("a", 257)} {
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
