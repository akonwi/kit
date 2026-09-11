package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestAcceptsFileIndexRejectsStaleSessionCWDAndGeneration(t *testing.T) {
	t.Parallel()
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	state := appState{
		session: protocol.SessionInfo{ID: sessionID, CWD: "/workspace"},
		bound:   fakeSession{id: sessionID},
		fileMention: fileMentionController{
			Open: true, cwd: "/workspace", generation: 3,
		},
	}
	if !state.acceptsFileIndex(sessionID, "/workspace", 3) {
		t.Fatal("current file index was rejected")
	}
	if state.acceptsFileIndex(sessionID, "/other", 3) {
		t.Fatal("stale cwd was accepted")
	}
	if state.acceptsFileIndex(sessionID, "/workspace", 2) {
		t.Fatal("stale generation was accepted")
	}
	state.session.ID = "session_abcdef0123456789abcdef0123456789"
	if state.acceptsFileIndex(sessionID, "/workspace", 3) {
		t.Fatal("stale session was accepted")
	}
}
