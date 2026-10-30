//go:build darwin || linux

package session

import (
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/saveattachment"
	"github.com/akonwi/kit/internal/subagent"
)

// Images client-hosted tools return to a child are kept under its owning
// session, so the child's save_attachment must read that session's
// attachments, resolving paths against the child's cwd.
func TestChildRuntimeBindsSaveAttachmentToTheOwner(t *testing.T) {
	store, err := attachment.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewChildRuntimeFactory(
		&mcpStubProviders{},
		fixedBundleBuilder{bundle: RuntimeBundle{Prompt: promptResult("child")}},
		t.TempDir(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	var requestedOwner, requestedCWD string
	factory.SaveAttachment = func(ownerSessionID string, currentCWD func() string) (droids.AnyTool, error) {
		requestedOwner, requestedCWD = ownerSessionID, currentCWD()
		return saveattachment.New(saveattachment.Options{SessionID: ownerSessionID, CWD: currentCWD, Store: store})
	}

	// Opening fails at model resolution, after the child's tools are built.
	_, _ = factory.Open(t.Context(), subagent.Conversation{
		ID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OwnerSessionID: "session_42", CWD: cwd,
		Model: "anthropic/claude", Agent: subagent.Definition{Name: "reviewer", Description: "reviews"},
	})

	if requestedOwner != "session_42" || requestedCWD != cwd {
		t.Fatalf("save_attachment bound to owner %q cwd %q, want session_42 and %q", requestedOwner, requestedCWD, cwd)
	}
}
