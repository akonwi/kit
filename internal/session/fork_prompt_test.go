package session_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

type forkPromptHarness struct {
	manager     *session.Manager
	providers   *authorityProviders
	attachments *attachment.Filesystem
	parent      session.SessionRecord
}

func newForkPromptHarness(t *testing.T) forkPromptHarness {
	t.Helper()
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	attachments, err := attachment.NewFilesystem(filepath.Join(root, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")), session.WithAttachmentStore(attachments))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	parent, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Parent", Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	return forkPromptHarness{manager: manager, providers: providers, attachments: attachments, parent: parent}
}

func (h forkPromptHarness) providerCalls() int {
	h.providers.mu.Lock()
	defer h.providers.mu.Unlock()
	return len(h.providers.requests)
}

func (h forkPromptHarness) lastUserMessage(t *testing.T) droids.UserMessage {
	t.Helper()
	h.providers.mu.Lock()
	defer h.providers.mu.Unlock()
	request := h.providers.requests[len(h.providers.requests)-1]
	user, ok := request.Messages[len(request.Messages)-1].(droids.UserMessage)
	if !ok {
		t.Fatalf("last provider message = %#v, want user message", request.Messages[len(request.Messages)-1])
	}
	return user
}

// waitForSettledUserMessages waits for sessionID to settle and returns the text
// of its user messages in transcript order.
func waitForSettledUserMessages(t *testing.T, manager *session.Manager, sessionID string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.Snapshot(t.Context(), sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.ActiveRunID == "" {
			var texts []string
			for _, message := range snapshot.Messages {
				if message.Role != "user" {
					continue
				}
				for _, content := range message.Content {
					if content.Text != "" {
						texts = append(texts, content.Text)
					}
				}
			}
			return texts
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %s did not settle", sessionID)
	return nil
}

// assertOnlyParentSession requires the parent to be the only listed session.
func assertOnlyParentSession(t *testing.T, manager *session.Manager, parentID string) {
	t.Helper()
	sessions, err := manager.List(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != parentID {
		t.Fatalf("sessions = %+v, want only the parent", sessions)
	}
}

func forkNotices(t *testing.T, manager *session.Manager, parentID string) []string {
	t.Helper()
	snapshot, err := manager.Snapshot(t.Context(), parentID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, boundary := range snapshot.Boundaries {
		ids = append(ids, boundary.ID)
	}
	return ids
}

func TestForkAdmitsFirstPromptOnlyOnChild(t *testing.T) {
	t.Parallel()
	h := newForkPromptHarness(t)
	forked, err := h.manager.Fork(t.Context(), h.parent.ID, session.ForkInput{Prompt: &session.PromptInput{Text: "explore the other approach"}})
	if err != nil {
		t.Fatal(err)
	}
	childID := forked.Session.ID
	if forked.FirstTurnErr != nil {
		t.Fatalf("first turn error = %v, want none", forked.FirstTurnErr)
	}
	if childID == "" || childID == h.parent.ID || forked.Session.ParentSessionID != h.parent.ID {
		t.Fatalf("forked session = %+v", forked.Session)
	}
	if got, want := waitForSettledUserMessages(t, h.manager, childID), []string{"explore the other approach"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("child user messages = %q, want %q", got, want)
	}
	if got := waitForSettledUserMessages(t, h.manager, h.parent.ID); len(got) != 0 {
		t.Fatalf("parent user messages = %q, want none", got)
	}
	if calls := h.providerCalls(); calls != 1 {
		t.Fatalf("provider calls = %d, want 1", calls)
	}
	if got, want := forkNotices(t, h.manager, h.parent.ID), []string{"session-fork:" + childID}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("parent fork notices = %q, want %q", got, want)
	}
}

func TestForkPromptResolvesInheritedAttachments(t *testing.T) {
	t.Parallel()
	h := newForkPromptHarness(t)
	staged, err := h.attachments.Put(t.Context(), attachment.PutInput{SessionID: h.parent.ID, Filename: "notes.txt", MediaType: "text/plain", Content: strings.NewReader("alpha"), MaxBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	forked, err := h.manager.Fork(t.Context(), h.parent.ID, session.ForkInput{Prompt: &session.PromptInput{Text: "inspect", AttachmentIDs: []string{staged.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	waitForSettledUserMessages(t, h.manager, forked.Session.ID)
	user := h.lastUserMessage(t)
	if len(user.Content) != 2 {
		t.Fatalf("child prompt content = %#v, want text and attachment", user.Content)
	}
	for index, want := range []string{"inspect", "--- attachment: \"notes.txt\" ---\nalpha"} {
		text, ok := user.Content[index].(droids.TextInput)
		if !ok || !strings.Contains(text.Text, want) {
			t.Fatalf("content[%d] = %#v, want %q", index, user.Content[index], want)
		}
	}
}

func TestForkPromptRejectsInvalidInputWithoutPublishingChild(t *testing.T) {
	t.Parallel()
	h := newForkPromptHarness(t)
	tests := []struct {
		name   string
		prompt session.PromptInput
	}{
		{name: "annotations", prompt: session.PromptInput{Text: "review", AnnotationIDs: []uint64{1}}},
		{name: "empty", prompt: session.PromptInput{Text: "   "}},
		{name: "missing attachment", prompt: session.PromptInput{Text: "inspect", AttachmentIDs: []string{"attachment_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}},
	}
	for _, test := range tests {
		prompt := test.prompt
		if _, err := h.manager.Fork(t.Context(), h.parent.ID, session.ForkInput{Prompt: &prompt}); !errors.Is(err, session.ErrInvalidInput) {
			t.Fatalf("%s: Fork() error = %v, want invalid input", test.name, err)
		}
	}
	assertOnlyParentSession(t, h.manager, h.parent.ID)
	if got := forkNotices(t, h.manager, h.parent.ID); len(got) != 0 {
		t.Fatalf("parent fork notices = %q, want none", got)
	}
	if calls := h.providerCalls(); calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

// failingForkTouchRepository fails prompt activity writes for every session
// except the parent, so a fork publishes before its first prompt fails.
type failingForkTouchRepository struct {
	session.Repository
	parentID string
}

func (r *failingForkTouchRepository) TouchSession(ctx context.Context, sessionID string, at time.Time) error {
	if r.parentID != "" && sessionID != r.parentID {
		return errSessionActivityWrite
	}
	return r.Repository.TouchSession(ctx, sessionID, at)
}

func TestForkPromptAdmissionFailureReportsFirstTurnErrorOnPublishedFork(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository := &failingForkTouchRepository{Repository: store}
	providers := &authorityProviders{}
	manager, err := session.NewManager(repository, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	parent, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	repository.parentID = parent.ID
	forked, err := manager.Fork(t.Context(), parent.ID, session.ForkInput{Prompt: &session.PromptInput{Text: "first prompt"}})
	if err != nil {
		t.Fatalf("Fork() error = %v, want a published fork", err)
	}
	if !errors.Is(forked.FirstTurnErr, errSessionActivityWrite) {
		t.Fatalf("first turn error = %v, want activity write failure", forked.FirstTurnErr)
	}
	sessions, err := manager.List(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	var child session.SessionRecord
	for _, record := range sessions {
		if record.ID != parent.ID {
			child = record
		}
	}
	if len(sessions) != 2 || child.ID != forked.Session.ID || child.ParentSessionID != parent.ID {
		t.Fatalf("sessions = %+v, want the parent and its published fork", sessions)
	}
	if got := waitForSettledUserMessages(t, manager, child.ID); len(got) != 0 {
		t.Fatalf("child user messages = %q, want none", got)
	}
	if got, want := forkNotices(t, manager, parent.ID), []string{"session-fork:" + child.ID}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("parent fork notices = %q, want %q", got, want)
	}
	providers.mu.Lock()
	calls := len(providers.requests)
	providers.mu.Unlock()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestForkOfRunningSessionCopiesItsLatestSettledBoundary(t *testing.T) {
	t.Parallel()
	h := newForkPromptHarness(t)
	if _, err := h.manager.StartPromptInput(t.Context(), h.parent.ID, session.PromptInput{Text: "settled work"}); err != nil {
		t.Fatal(err)
	}
	waitForSettledUserMessages(t, h.manager, h.parent.ID)
	release := make(chan struct{})
	started := make(chan struct{})
	h.providers.mu.Lock()
	h.providers.block, h.providers.started = release, started
	h.providers.mu.Unlock()
	if _, err := h.manager.StartPromptInput(t.Context(), h.parent.ID, session.PromptInput{Text: "in flight"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("parent turn did not start")
	}

	forked, err := h.manager.Fork(t.Context(), h.parent.ID, session.ForkInput{})
	if err != nil {
		t.Fatalf("Fork() of running session error = %v", err)
	}
	if got, want := waitForSettledUserMessages(t, h.manager, forked.Session.ID), []string{"settled work"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("child user messages = %q, want %q", got, want)
	}
	snapshot, err := h.manager.Snapshot(t.Context(), h.parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveRunID == "" {
		t.Fatal("parent turn ended during the fork, want it still running")
	}

	close(release)
	if got, want := waitForSettledUserMessages(t, h.manager, h.parent.ID), []string{"settled work", "in flight"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("parent user messages = %q, want %q", got, want)
	}
}

func TestForkDuringExplicitCompactionCopiesTheUncompactedBoundary(t *testing.T) {
	t.Parallel()
	h := newForkPromptHarness(t)
	if _, err := h.manager.StartPromptInput(t.Context(), h.parent.ID, session.PromptInput{Text: "settled work"}); err != nil {
		t.Fatal(err)
	}
	waitForSettledUserMessages(t, h.manager, h.parent.ID)
	release := make(chan struct{})
	started := make(chan struct{})
	h.providers.mu.Lock()
	h.providers.block, h.providers.started = release, started
	h.providers.mu.Unlock()
	compacted := make(chan error, 1)
	go func() {
		_, err := h.manager.CompactSession(context.Background(), h.parent.ID, "compact_during_fork")
		compacted <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("compaction summary did not start")
	}

	type forkOutcome struct {
		result session.ForkResult
		err    error
	}
	forkDone := make(chan forkOutcome, 1)
	go func() {
		result, err := h.manager.Fork(t.Context(), h.parent.ID, session.ForkInput{})
		forkDone <- forkOutcome{result, err}
	}()
	var forked session.ForkResult
	select {
	case outcome := <-forkDone:
		if outcome.err != nil {
			t.Fatalf("Fork() during compaction error = %v", outcome.err)
		}
		forked = outcome.result
	case <-time.After(2 * time.Second):
		close(release)
		<-compacted
		t.Fatal("Fork() waited for compaction to finish")
	}
	if got, want := waitForSettledUserMessages(t, h.manager, forked.Session.ID), []string{"settled work"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("child user messages = %q, want %q", got, want)
	}

	close(release)
	if err := <-compacted; err != nil {
		t.Fatalf("CompactSession() error = %v", err)
	}
}
