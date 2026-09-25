package tui

import (
	"context"
	"fmt"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestWorkspaceFilePickerRevealTracksLongKeyboardSelection(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	entries := make([]protocol.FileIndexEntry, 30)
	for index := range entries {
		entries[index] = protocol.FileIndexEntry{Path: fmt.Sprintf("file-%02d", index)}
	}
	source := indexedPickerSource(entries...)
	controller.Selection = workspaceFileKey{WorkspaceID: "workspace_1", Path: "file-25"}
	state := appState{workspaceFilePicker: controller, indexedFiles: source}
	state.requestWorkspaceFilePickerReveal()
	if !state.workspaceFilePickerRevealPending || state.workspaceFilePickerRevealOffset != 20 {
		t.Fatalf("long-list reveal = pending:%v offset:%d", state.workspaceFilePickerRevealPending, state.workspaceFilePickerRevealOffset)
	}
}

func TestWorkspaceCWDMetadataClosesPickerAndResetsIndexedSource(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	pickerContext, cancel := context.WithCancel(context.Background())
	indexContext, indexCancel := context.WithCancel(context.Background())
	state := appState{
		session: protocol.SessionInfo{ID: "session_1", CWD: "/repo"}, workspaceID: "workspace_1",
		workspaceFilePicker: controller, workspaceFilePickerContext: pickerContext, workspaceFilePickerCancel: cancel,
		indexedFiles:     indexedFileSource{SessionID: "session_1", CWD: "/repo", Entries: []protocol.FileIndexEntry{{Path: "old.go"}}, cancel: indexCancel},
		metadataStreamID: "stream_1", metadataSequence: 1,
	}
	state.applySessionMetadataEvents([]protocol.SessionEvent{{
		StreamID: "stream_1", Sequence: 2, SessionID: "session_1", Kind: protocol.SessionEventSessionCWDChanged,
		Workspace: &protocol.WorkspaceRef{SessionID: "session_1", WorkspaceID: "workspace_2", CWD: "/other", State: protocol.WorkspaceReady},
	}})
	if state.workspaceFilePicker.Open || state.workspaceID != "workspace_2" || state.session.CWD != "/other" {
		t.Fatalf("cwd invalidation = open:%v workspace:%q cwd:%q", state.workspaceFilePicker.Open, state.workspaceID, state.session.CWD)
	}
	if state.indexedFiles.SessionID != "" || state.indexedFiles.Entries != nil {
		t.Fatalf("cwd index source was not reset: %+v", state.indexedFiles)
	}
	select {
	case <-pickerContext.Done():
	default:
		t.Fatal("cwd invalidation did not cancel picker workspace lookup")
	}
	select {
	case <-indexContext.Done():
	default:
		t.Fatal("cwd invalidation did not cancel indexed-file loading")
	}
}

func TestWorkspaceFileActivationRejectsOldWorkspaceOrIndexAssociation(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	row := workspaceFilePickerRow{Kind: workspaceFilePickerEntryRow, Key: workspaceFileKey{WorkspaceID: "workspace_1", Path: "old.go"}, Entry: protocol.FileIndexEntry{Path: "old.go"}}
	state := appState{
		session: protocol.SessionInfo{ID: "session_1", CWD: "/repo"}, workspaceID: "workspace_2",
		workspaceFilePicker: controller, indexedFiles: indexedPickerSource(protocol.FileIndexEntry{Path: "old.go"}),
	}
	if state.workspaceFilePickerActivationCurrent(row) {
		t.Fatal("old workspace descriptor passed activation guard")
	}
	state.workspaceID = "workspace_1"
	if !state.workspaceFilePickerActivationCurrent(row) {
		t.Fatal("current workspace descriptor failed activation guard")
	}
	state.indexedFiles.CWD = "/other"
	if state.workspaceFilePickerActivationCurrent(row) {
		t.Fatal("stale index cwd passed activation guard")
	}
}

func TestWorkspaceFilePickerOpensRegularFileAndIgnoresDirectory(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	state := appState{
		session: protocol.SessionInfo{ID: "session_1", CWD: "/repo"}, workspaceID: "workspace_1",
		workspaceFilePicker: controller, indexedFiles: indexedPickerSource(
			protocol.FileIndexEntry{Path: "docs/", IsDir: true}, protocol.FileIndexEntry{Path: "main.go"},
		),
	}
	directory := protocol.FileIndexEntry{Path: "docs/", IsDir: true}
	if err := state.openWorkspaceFilePickerEntry(directory); err != nil {
		t.Fatal(err)
	}
	if len(state.workspace.Panes()) != 0 || !state.workspaceFilePicker.Open {
		t.Fatalf("directory activation changed workspace: panes=%+v open=%v", state.workspace.Panes(), state.workspaceFilePicker.Open)
	}
	if err := state.openWorkspaceFilePickerEntry(protocol.FileIndexEntry{Path: "main.go"}); err != nil {
		t.Fatal(err)
	}
	panes := state.workspace.Panes()
	if len(panes) != 1 || panes[0].WorkspaceID != "workspace_1" || panes[0].Path != "main.go" || state.workspaceFilePicker.Open {
		t.Fatalf("file activation = panes:%+v open:%v", panes, state.workspaceFilePicker.Open)
	}
}

func TestWorkspaceFilePickerCloseCancelsLookupAndRejectsLateGeneration(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	ctx, cancel := context.WithCancel(context.Background())
	generation := controller.Generation
	state := appState{workspaceFilePicker: controller, workspaceFilePickerContext: ctx, workspaceFilePickerCancel: cancel}
	state.closeWorkspaceFilePicker()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("close did not cancel workspace lookup")
	}
	if state.workspaceFilePicker.Open || state.workspaceFilePicker.Generation == generation {
		t.Fatalf("closed picker = %+v", state.workspaceFilePicker)
	}
}
