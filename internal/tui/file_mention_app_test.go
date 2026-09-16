package tui

import (
	"context"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
)

func TestAcceptsIndexedFilesRejectsStaleSessionCWDAndGeneration(t *testing.T) {
	t.Parallel()
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	state := appState{
		session: protocol.SessionInfo{ID: sessionID, CWD: "/workspace"},
		bound:   fakeSession{id: sessionID},
		indexedFiles: indexedFileSource{
			SessionID: sessionID, CWD: "/workspace", generation: 3,
		},
	}
	if !state.acceptsIndexedFiles(sessionID, "/workspace", 3) {
		t.Fatal("current file index was rejected")
	}
	if state.acceptsIndexedFiles(sessionID, "/other", 3) {
		t.Fatal("stale cwd was accepted")
	}
	if state.acceptsIndexedFiles(sessionID, "/workspace", 2) {
		t.Fatal("stale generation was accepted")
	}
	state.session.ID = "session_abcdef0123456789abcdef0123456789"
	if state.acceptsIndexedFiles(sessionID, "/workspace", 3) {
		t.Fatal("stale session was accepted")
	}
}

type refreshingFileIndexSession struct {
	fakeSession
	regularCalls int
	refreshCalls int
}

func (s *refreshingFileIndexSession) FileIndex(context.Context) (protocol.SessionFileIndex, error) {
	s.regularCalls++
	return protocol.SessionFileIndex{}, nil
}

func (s *refreshingFileIndexSession) RefreshFileIndex(context.Context) (protocol.SessionFileIndex, error) {
	s.refreshCalls++
	return protocol.SessionFileIndex{}, nil
}

func TestIndexedFileSourceExplicitRefreshUsesForcedSessionOperation(t *testing.T) {
	t.Parallel()
	session := &refreshingFileIndexSession{}
	if _, err := requestSessionFileIndex(context.Background(), session, false); err != nil {
		t.Fatal(err)
	}
	if _, err := requestSessionFileIndex(context.Background(), session, true); err != nil {
		t.Fatal(err)
	}
	if session.regularCalls != 1 || session.refreshCalls != 1 {
		t.Fatalf("index operations = regular:%d refresh:%d", session.regularCalls, session.refreshCalls)
	}
}

func TestIndexedFileSourceSimultaneousConsumersShareOneLoad(t *testing.T) {
	t.Parallel()
	now := time.Unix(100, 0)
	source := indexedFileSource{SessionID: "session_1", CWD: "/repo", Loading: true}
	if source.shouldLoad("session_1", "/repo", false, now) {
		t.Fatal("a second consumer would start an independent index load")
	}
	if !source.shouldLoad("session_1", "/repo", true, now) {
		t.Fatal("explicit refresh did not supersede the shared load")
	}
	source.Loading = false
	source.Entries = []protocol.FileIndexEntry{}
	source.indexedAt = now
	if source.shouldLoad("session_1", "/repo", false, now.Add(time.Minute)) {
		t.Fatal("fresh shared index would be loaded again")
	}
}

func TestIndexedFileSourceSupersessionCancelsAndRejectsOlderLoad(t *testing.T) {
	t.Parallel()
	var source indexedFileSource
	first, firstGeneration := source.begin("session_1", "/repo", context.Background())
	_, secondGeneration := source.begin("session_1", "/repo", context.Background())
	select {
	case <-first.Done():
	default:
		t.Fatal("superseded index load was not canceled")
	}
	if secondGeneration <= firstGeneration || source.generation != secondGeneration || !source.Loading {
		t.Fatalf("supersession state = generation:%d first:%d second:%d loading:%v", source.generation, firstGeneration, secondGeneration, source.Loading)
	}
}

func TestIndexedFileSourceResetCancelsAndClearsSessionState(t *testing.T) {
	t.Parallel()
	var source indexedFileSource
	ctx, _ := source.begin("session_1", "/repo", context.Background())
	source.Entries = []protocol.FileIndexEntry{{Path: "main.go"}}
	source.Truncated = true
	source.reset()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("reset did not cancel index load")
	}
	if source.SessionID != "" || source.CWD != "" || source.Entries != nil || source.Loading || source.Truncated {
		t.Fatalf("reset source = %+v", source)
	}
}
