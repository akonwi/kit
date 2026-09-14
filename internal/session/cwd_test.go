package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/systemprompt"
)

func TestChangeCWDToolMovesRelativeCodingToolScopeDuringRun(t *testing.T) {
	processCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	destination := filepath.Join(root, "nested")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &cwdToolProviders{target: "nested"}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_11111111111111111111111111111111", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "move and inspect"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Session.CWD != destination {
		t.Fatalf("session cwd = %q, want %q", snapshot.Session.CWD, destination)
	}
	if len(snapshot.Boundaries) != 0 {
		t.Fatalf("model-initiated cwd change echoed boundaries = %+v", snapshot.Boundaries)
	}
	var changeText, bashText string
	for _, message := range snapshot.Messages {
		if message.Role != "tool" {
			continue
		}
		switch message.ToolName {
		case session.ChangeCWDToolName:
			changeText = transcriptText(message.Content)
		case "bash":
			bashText = transcriptText(message.Content)
		}
	}
	if changeText != "Changed session cwd to "+destination {
		t.Fatalf("change_cwd result = %q", changeText)
	}
	if bashText != destination {
		t.Fatalf("bash pwd = %q, want %q", bashText, destination)
	}
	if current, err := os.Getwd(); err != nil || current != processCWD {
		t.Fatalf("process cwd = %q, error = %v; want %q", current, err, processCWD)
	}
}

func TestRunningDirectBashKeepsAdmittedCWDWhileSessionMoves(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "nested")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CWD: root, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := manager.StartBash(t.Context(), record.ID, "bash_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "pwd; sleep 0.1", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ChangeCWD(t.Context(), record.ID, "nested"); err != nil {
		t.Fatal(err)
	}
	settled, err := waitForBash(t, manager, record.ID, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.CWD != root || strings.TrimSpace(settled.Output) != root {
		t.Fatalf("admitted bash moved with session cwd: %+v", settled)
	}
}

func TestChangeCWDPersistsBeforePublicationAndReloadUsesDestination(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	for _, directory := range []string{first, second} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	contextPath := filepath.Join(second, "AGENTS.md")
	if err := os.WriteFile(contextPath, []byte("destination guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	resolver := systemprompt.WorktreeRootResolverFunc(func(context.Context, string) (string, error) { return "", nil })
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := session.NewManager(store, &authorityProviders{}, newContextRuntimeBundleBuilder(t, home, resolver), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_22222222222222222222222222222222", CWD: first, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	mutationID := "cwd_99999999999999999999999999999999"
	changed, err := manager.ChangeCWDWithID(t.Context(), record.ID, mutationID, "../second")
	if err != nil {
		t.Fatal(err)
	}
	if !changed.Changed || changed.PreviousCWD != first || changed.CWD != second || changed.Session.CWD != second {
		t.Fatalf("change result = %+v", changed)
	}
	replayed, err := manager.ChangeCWDWithID(t.Context(), record.ID, mutationID, "../second")
	if err != nil || replayed.CWD != second || replayed.Session.CWD != second {
		t.Fatalf("replayed relative cwd change = %+v, error = %v", replayed, err)
	}
	persisted, err := store.GetSession(t.Context(), record.ID)
	if err != nil || persisted.CWD != second {
		t.Fatalf("persisted session = %+v, error = %v", persisted, err)
	}
	afterChange, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterChange.Boundaries) != 1 || afterChange.Boundaries[0].Kind != "cwd" || afterChange.Boundaries[0].Source != "user" ||
		!strings.Contains(transcriptText(afterChange.Boundaries[0].Content), first) || !strings.Contains(transcriptText(afterChange.Boundaries[0].Content), second) {
		t.Fatalf("user cwd boundary = %+v", afterChange.Boundaries)
	}
	execution, err := manager.StartBash(t.Context(), record.ID, "bash_55555555555555555555555555555555", "pwd", true)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := waitForBash(t, manager, record.ID, execution.ID)
	if err != nil || settled.CWD != second || strings.TrimSpace(settled.Output) != second {
		t.Fatalf("direct bash after cwd change = %+v, error = %v", settled, err)
	}
	reloaded, err := manager.ReloadSession(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.EventStreamID != before.EventStreamID {
		t.Fatal("live reload replaced the event stream")
	}
	canonicalContextPath, err := filepath.EvalSymlinks(contextPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, source := range reloaded.Sources {
		found = found || source.Kind == systemprompt.SectionContext && source.Path == canonicalContextPath
	}
	if !found {
		t.Fatalf("reload sources = %#v, want %q", reloaded.Sources, canonicalContextPath)
	}
}

func TestChangeCWDRejectsInvalidTargetsWithoutMoving(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_66666666666666666666666666666666", CWD: root, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"missing", "file.txt"} {
		if _, err := manager.ChangeCWD(t.Context(), record.ID, target); !errors.Is(err, session.ErrInvalidInput) {
			t.Fatalf("ChangeCWD(%q) error = %v", target, err)
		}
	}
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil || snapshot.Session.CWD != root {
		t.Fatalf("snapshot after invalid targets = %+v, error = %v", snapshot, err)
	}
}

func TestTemporaryNoOpCWDIntentAdvancesActivityOnce(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := session.NewManager(
		store, &authorityProviders{}, staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_77777777777777777777777777777777", CWD: root, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	mutationID := "cwd_no_change"
	first, err := manager.ChangeCWDWithID(t.Context(), record.ID, mutationID, ".")
	if err != nil {
		t.Fatal(err)
	}
	if first.Changed || !first.Session.UpdatedAt.After(record.UpdatedAt) {
		t.Fatalf("no-op cwd result = %+v, want one newer activity timestamp", first)
	}
	time.Sleep(time.Millisecond)
	replayed, err := manager.ChangeCWDWithID(t.Context(), record.ID, mutationID, ".")
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Session.UpdatedAt.Equal(first.Session.UpdatedAt) {
		t.Fatalf("replayed no-op cwd changed activity from %v to %v", first.Session.UpdatedAt, replayed.Session.UpdatedAt)
	}
}

func TestConcurrentSessionsKeepIndependentWorkspaceScopes(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	type fixture struct {
		id          string
		initial     string
		destination string
	}
	fixtures := []fixture{
		{id: "session_77777777777777777777777777777777", initial: filepath.Join(root, "one"), destination: filepath.Join(root, "one", "next")},
		{id: "session_88888888888888888888888888888888", initial: filepath.Join(root, "two"), destination: filepath.Join(root, "two", "next")},
	}
	for _, fixture := range fixtures {
		if err := os.MkdirAll(fixture.destination, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Create(t.Context(), session.CreateInput{ID: fixture.id, CWD: fixture.initial, Model: "test/echo", Temporary: true}); err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	errorsBySession := make(chan error, len(fixtures))
	for _, fixture := range fixtures {
		fixture := fixture
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := manager.ChangeCWD(t.Context(), fixture.id, "next")
			if err == nil && result.Session.CWD != fixture.destination {
				err = fmt.Errorf("cwd = %q, want %q", result.Session.CWD, fixture.destination)
			}
			errorsBySession <- err
		}()
	}
	group.Wait()
	close(errorsBySession)
	for err := range errorsBySession {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range fixtures {
		snapshot, err := manager.Snapshot(t.Context(), fixture.id)
		if err != nil || snapshot.Session.CWD != fixture.destination {
			t.Fatalf("session %q snapshot = %+v, error = %v", fixture.id, snapshot, err)
		}
	}
}

func TestChangeCWDFailureLeavesSessionAndToolsAtPreviousScope(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "nested")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository := &failingCWDRepository{Repository: store}
	manager, err := session.NewManager(repository, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_33333333333333333333333333333333", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	repository.fail = true
	if _, err := manager.ChangeCWD(t.Context(), record.ID, "nested"); !errors.Is(err, errCWDWrite) {
		t.Fatalf("ChangeCWD() error = %v", err)
	}
	repository.fail = false
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Session.CWD != root {
		t.Fatalf("session cwd = %q after failure", snapshot.Session.CWD)
	}
	execution, err := manager.StartBash(t.Context(), record.ID, "bash_44444444444444444444444444444444", "pwd", true)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := waitForBash(t, manager, record.ID, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.CWD != root || strings.TrimSpace(settled.Output) != root {
		t.Fatalf("bash after failed cwd change = %+v", settled)
	}
}

var errAmbiguousCWDCommit = errors.New("simulated ambiguous cwd commit")

type ambiguousCWDRepository struct {
	session.Repository
	failAfterCommit bool
}

func (repository *ambiguousCWDRepository) ApplySessionCWDMutation(ctx context.Context, mutation session.CWDMutation) (session.SessionRecord, session.CWDMutation, error) {
	record, applied, err := repository.Repository.ApplySessionCWDMutation(ctx, mutation)
	if err == nil && repository.failAfterCommit {
		repository.failAfterCommit = false
		return session.SessionRecord{}, session.CWDMutation{}, errAmbiguousCWDCommit
	}
	return record, applied, err
}

func TestChangeCWDReconcilesAmbiguousCommitBeforePublishing(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "nested")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repository := &ambiguousCWDRepository{Repository: store, failAfterCommit: true}
	manager, err := session.NewManager(
		repository, &authorityProviders{}, staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_44444444444444444444444444444444", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.ChangeCWDWithID(t.Context(), record.ID, "cwd_ambiguous", "nested")
	if err != nil {
		t.Fatalf("ChangeCWDWithID() failed after committed mutation: %v", err)
	}
	if result.CWD != destination || result.Session.CWD != destination {
		t.Fatalf("change result = %+v, want %q", result, destination)
	}
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Session.CWD != destination {
		t.Fatalf("snapshot cwd = %q, want %q", snapshot.Session.CWD, destination)
	}
}

func waitForBash(t *testing.T, manager *session.Manager, sessionID, executionID string) (session.BashExecution, error) {
	t.Helper()
	for {
		execution, err := manager.GetBash(t.Context(), sessionID, executionID)
		if err != nil || execution.Status != session.BashExecutionRunning {
			return execution, err
		}
		select {
		case <-t.Context().Done():
			return session.BashExecution{}, t.Context().Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func transcriptText(content []session.TranscriptContent) string {
	var values []string
	for _, block := range content {
		if block.Kind == session.TranscriptContentText {
			values = append(values, block.Text)
		}
	}
	return strings.Join(values, "\n")
}

type cwdToolProviders struct {
	mu     sync.Mutex
	calls  int
	target string
}

func (p *cwdToolProviders) ID() string             { return "test" }
func (p *cwdToolProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *cwdToolProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "echo" || id == "test/echo"
}
func (p *cwdToolProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (*cwdToolProviders) RefreshModels(context.Context) error { return nil }
func (*cwdToolProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *cwdToolProviders) Stream(context.Context, droids.Model, droids.Request) droids.Stream {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		arguments, _ := json.Marshal(map[string]string{"path": p.target})
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{
				droids.ToolCall{ID: "call_change_cwd", Name: session.ChangeCWDToolName, Arguments: arguments},
				droids.ToolCall{ID: "call_bash_pwd", Name: "bash", Arguments: []byte(`{"command":"pwd"}`)},
			},
		}}
	}
	return &authorityStream{message: droids.AssistantMessage{
		Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "done"}},
	}}
}
func (*cwdToolProviders) model() droids.Model {
	return droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
}

var errCWDWrite = errors.New("simulated cwd persistence failure")

type failingCWDRepository struct {
	session.Repository
	fail bool
}

func (repository *failingCWDRepository) ApplySessionCWDMutation(ctx context.Context, mutation session.CWDMutation) (session.SessionRecord, session.CWDMutation, error) {
	if repository.fail {
		return session.SessionRecord{}, session.CWDMutation{}, errCWDWrite
	}
	return repository.Repository.ApplySessionCWDMutation(ctx, mutation)
}
