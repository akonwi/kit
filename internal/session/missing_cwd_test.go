package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/promptcommands"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/systemprompt"
)

// missingCWDFixture is a persisted session whose working directory, which
// had project guidance, was removed while no runtime had it loaded.
type missingCWDFixture struct {
	store              *storage.Store
	builder            session.RuntimeBundleBuilder
	droids             string
	home               string
	removed            string
	destination        string
	destinationContext string
	sessionID          string
}

func newMissingCWDFixture(t *testing.T, sessionID string) missingCWDFixture {
	t.Helper()
	root := t.TempDir()
	fixture := missingCWDFixture{
		droids: filepath.Join(root, "droids"), home: filepath.Join(root, "home"),
		removed: filepath.Join(root, "removed"), destination: filepath.Join(root, "destination"),
		sessionID: sessionID,
	}
	for _, directory := range []string{fixture.home, fixture.removed, fixture.destination} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeContext(t, filepath.Join(fixture.home, "AGENTS.md"), "global guidance")
	writeContext(t, filepath.Join(fixture.removed, "AGENTS.md"), "removed guidance")
	fixture.destinationContext = filepath.Join(fixture.destination, "AGENTS.md")
	writeContext(t, fixture.destinationContext, "destination guidance")
	projectSkill := filepath.Join(fixture.destination, ".agents", "skills", "destination-check")
	if err := os.MkdirAll(projectSkill, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectSkill, "SKILL.md"), []byte("---\nname: destination-check\ndescription: Destination skill\n---\nCheck.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture.store = store
	paths := apphome.FromHome(fixture.home)
	skillLoader, err := skills.NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	promptLoader, err := promptcommands.NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	resolver := systemprompt.WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
		return "", systemprompt.ErrNoGitWorktree
	})
	fixture.builder, err = session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{
		Core: systemprompt.DefaultCore, SkillLoader: skillLoader, PromptCommandLoader: promptLoader,
		Context: &systemprompt.ContextBuilderOptions{Paths: paths, Resolver: resolver},
	})
	if err != nil {
		t.Fatal(err)
	}

	manager := fixture.manager(t, &authorityProviders{})
	if _, err := manager.Create(t.Context(), session.CreateInput{ID: sessionID, CWD: fixture.removed, Model: "test/echo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), sessionID, "before removal"); err != nil {
		t.Fatal(err)
	}
	manager.Close()
	if err := os.RemoveAll(fixture.removed); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f missingCWDFixture) manager(t *testing.T, providers droids.Providers) *session.Manager {
	t.Helper()
	manager, err := session.NewManager(f.store, providers, f.builder, session.WithDroidStoreDirectory(f.droids))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func TestSessionLoadsAfterItsWorkingDirectoryIsRemoved(t *testing.T) {
	fixture := newMissingCWDFixture(t, "session_31313131313131313131313131313131")
	manager := fixture.manager(t, &authorityProviders{})

	snapshot, err := manager.Snapshot(t.Context(), fixture.sessionID)
	if err != nil {
		t.Fatalf("Snapshot() of a session whose cwd was removed: %v", err)
	}
	if snapshot.Session.CWD != fixture.removed || len(snapshot.Messages) != 2 || transcriptText(snapshot.Messages[0].Content) != "before removal" {
		t.Fatalf("snapshot = cwd %q, messages %+v", snapshot.Session.CWD, snapshot.Messages)
	}
	metadata, err := manager.PromptMetadata(t.Context(), fixture.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := contextSourcePaths(metadata), []string{canonicalPath(t, filepath.Join(fixture.home, "AGENTS.md"))}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("context sources = %q, want only the global context %q", got, want)
	}
	if len(metadata.Diagnostics) != 1 || metadata.Diagnostics[0].Code != "session.cwd_unavailable" || metadata.Diagnostics[0].Source.Path != fixture.removed ||
		metadata.Diagnostics[0].Message != "The session's working directory does not exist, so project skills, context, prompt commands, agents, and MCP servers were not loaded" {
		t.Fatalf("prompt diagnostics = %#v", metadata.Diagnostics)
	}
	if _, err := manager.RunPrompt(t.Context(), fixture.sessionID, "after removal"); err != nil {
		t.Fatalf("RunPrompt() in a removed cwd: %v", err)
	}
	if _, err := manager.ReloadSession(t.Context(), fixture.sessionID); err != nil {
		t.Fatalf("ReloadSession() in a removed cwd: %v", err)
	}

	execution, err := manager.StartBash(t.Context(), fixture.sessionID, "bash_31313131313131313131313131313131", "pwd", true)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := waitForBash(t, manager, fixture.sessionID, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "the working directory " + fixture.removed + " does not exist; change the working directory"
	if settled.Status != session.BashExecutionFailed || settled.ErrorMessage != want {
		t.Fatalf("direct bash in a removed cwd = status %q, error %q; want %q", settled.Status, settled.ErrorMessage, want)
	}
	if _, err := os.Stat(fixture.removed); !os.IsNotExist(err) {
		t.Fatalf("removed cwd was recreated: %v", err)
	}
}

func TestUserMoveFromRemovedWorkingDirectoryRediscoversProject(t *testing.T) {
	fixture := newMissingCWDFixture(t, "session_32323232323232323232323232323232")
	manager := fixture.manager(t, &authorityProviders{})

	const mutationID = "cwd_32323232323232323232323232323232"
	changed, err := manager.ChangeCWDWithID(t.Context(), fixture.sessionID, mutationID, fixture.destination)
	if err != nil {
		t.Fatalf("ChangeCWDWithID() from a removed cwd: %v", err)
	}
	if !changed.Changed || !changed.Rediscovered || changed.PreviousCWD != fixture.removed || changed.CWD != fixture.destination {
		t.Fatalf("change result = %+v", changed)
	}
	replayed, err := manager.ChangeCWDWithID(t.Context(), fixture.sessionID, mutationID, fixture.destination)
	if err != nil || replayed.PreviousCWD != changed.PreviousCWD || replayed.CWD != changed.CWD || replayed.Changed != changed.Changed || !replayed.Rediscovered {
		t.Fatalf("replayed change = %+v, %v; want the original outcome %+v", replayed, err, changed)
	}
	metadata, err := manager.PromptMetadata(t.Context(), fixture.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{canonicalPath(t, filepath.Join(fixture.home, "AGENTS.md")), canonicalPath(t, fixture.destinationContext)}
	if got := contextSourcePaths(metadata); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("context sources after the move = %q, want %q", got, want)
	}
	if len(metadata.Diagnostics) != 0 {
		t.Fatalf("prompt diagnostics after the move = %#v", metadata.Diagnostics)
	}
	snapshot, err := manager.Snapshot(t.Context(), fixture.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	boundaries := boundaryRecords(snapshot)
	wantBoundary := "The user changed this session's working directory from " + fixture.removed + " to " + fixture.destination +
		". Relative filesystem operations now resolve from " + fixture.destination +
		". Session context, skills, and prompt commands were reloaded from " + fixture.destination + "."
	if len(boundaries) != 1 || !strings.Contains(transcriptText(boundaries[0].Content), wantBoundary) {
		t.Fatalf("cwd boundaries = %+v, want %q", boundaries, wantBoundary)
	}

	// Moving between existing directories keeps the configuration snapshot.
	again, err := manager.ChangeCWD(t.Context(), fixture.sessionID, fixture.home)
	if err != nil || !again.Changed || again.Rediscovered {
		t.Fatalf("ordinary move = %+v, %v", again, err)
	}
}

func TestModelMoveFromRemovedWorkingDirectoryRediscoversProject(t *testing.T) {
	fixture := newMissingCWDFixture(t, "session_33333333333333333333333333333333")
	manager := fixture.manager(t, &cwdToolProviders{target: fixture.destination})

	if _, err := manager.RunPrompt(t.Context(), fixture.sessionID, "move and inspect"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Snapshot(t.Context(), fixture.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	var changeText, bashText string
	for _, message := range snapshot.Messages {
		switch message.ToolName {
		case session.ChangeCWDToolName:
			changeText = transcriptText(message.Content)
		case "bash":
			bashText = transcriptText(message.Content)
		}
	}
	wantChange := "Changed session cwd to " + fixture.destination + ". Reloaded project context, skills, and prompt commands from " + fixture.destination + "."
	if changeText != wantChange {
		t.Fatalf("change_cwd result = %q, want %q", changeText, wantChange)
	}
	if bashText != fixture.destination {
		t.Fatalf("bash pwd = %q, want %q", bashText, fixture.destination)
	}
	metadata, err := manager.PromptMetadata(t.Context(), fixture.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{canonicalPath(t, filepath.Join(fixture.home, "AGENTS.md")), canonicalPath(t, fixture.destinationContext)}
	if got := contextSourcePaths(metadata); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("context sources after the model's move = %q, want %q", got, want)
	}
}
