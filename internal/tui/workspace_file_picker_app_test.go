package tui

import (
	"context"
	"fmt"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

type pickerWorkspaceSession struct {
	sessionclient.Session
	limits protocol.WorkspaceLimits
}

func (s *pickerWorkspaceSession) WorkspaceLimits() protocol.WorkspaceLimits { return s.limits }
func (*pickerWorkspaceSession) Workspace(context.Context) (protocol.WorkspaceRef, error) {
	return protocol.WorkspaceRef{}, nil
}
func (*pickerWorkspaceSession) ListDirectory(context.Context, protocol.ListDirectoryInput) (protocol.DirectoryPage, error) {
	return protocol.DirectoryPage{}, nil
}
func (*pickerWorkspaceSession) ReadWorkspaceFile(context.Context, protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	return protocol.WorkspaceFileRead{}, nil
}

func TestWorkspaceFilePickerConcurrencyUsesAdvertisedBounds(t *testing.T) {
	t.Parallel()
	active, pending := workspaceFilePickerConcurrency(protocol.WorkspaceLimits{MaxActiveRequests: 2, MaxPendingRequests: 5})
	if active != 2 || pending != 5 {
		t.Fatalf("advertised concurrency = active:%d pending:%d", active, pending)
	}
	active, pending = workspaceFilePickerConcurrency(protocol.WorkspaceLimits{MaxActiveRequests: 2, MaxPendingRequests: 0})
	if active != 2 || pending != 0 {
		t.Fatalf("zero-pending concurrency = active:%d pending:%d", active, pending)
	}
	active, pending = workspaceFilePickerConcurrency(protocol.WorkspaceLimits{})
	if active != protocol.MaxWorkspaceActiveRequests || pending != protocol.MaxWorkspacePendingRequests {
		t.Fatalf("zero-value concurrency = active:%d pending:%d", active, pending)
	}
	active, pending = workspaceFilePickerConcurrency(protocol.WorkspaceLimits{MaxActiveRequests: 1_000, MaxPendingRequests: 1_000})
	if active != protocol.MaxWorkspaceActiveRequests || pending != protocol.MaxWorkspacePendingRequests {
		t.Fatalf("clamped concurrency = active:%d pending:%d", active, pending)
	}
}

func TestWorkspaceFilePickerSupersededRequestCannotDriveStaleRecovery(t *testing.T) {
	t.Parallel()
	state := appState{workspaceFilePicker: readyFilePickerController()}
	key, stale := state.workspaceFilePicker.beginLoad("", "")
	_, current := state.workspaceFilePicker.beginLoad("", "")
	generation := state.workspaceFilePicker.Generation
	if state.workspaceFilePickerRequestCurrent(key, stale, generation) {
		t.Fatal("superseded request remained eligible for stale recovery")
	}
	if !state.workspaceFilePickerRequestCurrent(key, current, generation) {
		t.Fatal("current request was not eligible for completion")
	}
	state.workspaceFilePicker.Generation++
	if state.workspaceFilePickerRequestCurrent(key, current, generation) {
		t.Fatal("older invocation generation remained eligible")
	}
}

func TestWorkspaceFilePickerRevealTracksLongKeyboardSelection(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	entries := make([]protocol.WorkspaceDirectoryEntry, 30)
	for index := range entries {
		name := fmt.Sprintf("file-%02d", index)
		entries[index] = protocol.WorkspaceDirectoryEntry{Path: name, Name: name, Kind: protocol.WorkspaceEntryFile}
	}
	key, serial := controller.beginLoad("", "")
	controller.applyPage(key, serial, "", directoryPage("", "dir_long", "", entries...))
	controller.Selection = workspaceFileKey{WorkspaceID: "workspace_1", Path: "file-25"}
	controller.SelectionKind = workspaceFilePickerEntryRow
	state := appState{workspaceFilePicker: controller}
	state.requestWorkspaceFilePickerReveal()
	if !state.workspaceFilePickerRevealPending || state.workspaceFilePickerRevealOffset != 20 {
		t.Fatalf("long-list reveal = pending:%v offset:%d", state.workspaceFilePickerRevealPending, state.workspaceFilePickerRevealOffset)
	}
}

func TestWorkspaceFilePickerWorkspaceLookupErrorRetriesBeforeActivationGuard(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	controller.Workspace = protocol.WorkspaceRef{WorkspaceID: "pending"}
	controller.workspaceError = true
	refreshes := 0
	state := appState{workspaceID: "workspace_1", workspaceFilePicker: controller, filePickerRefreshHook: func() { refreshes++ }}
	state.activateWorkspaceFilePickerRow(workspaceFilePickerRow{
		Kind: workspaceFilePickerErrorRow, Key: workspaceFileKey{WorkspaceID: "pending"}, Text: "lookup failed",
	})
	if refreshes != 1 || !state.workspaceFilePicker.Open {
		t.Fatalf("workspace lookup retry = refreshes:%d open:%v", refreshes, state.workspaceFilePicker.Open)
	}
}

func TestWorkspaceCWDMetadataInvalidatesOpenFilePicker(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	pickerContext, cancel := context.WithCancel(context.Background())
	state := appState{
		session: protocol.SessionInfo{ID: "session_1", CWD: "/repo"}, workspaceID: "workspace_1",
		workspaceFilePicker: controller, workspaceFilePickerContext: pickerContext, workspaceFilePickerCancel: cancel,
		metadataStreamID: "stream_1", metadataSequence: 1,
	}
	state.applySessionMetadataEvents([]protocol.SessionEvent{{
		StreamID: "stream_1", Sequence: 2, SessionID: "session_1", Kind: protocol.SessionEventSessionCWDChanged,
		Workspace: &protocol.WorkspaceRef{SessionID: "session_1", WorkspaceID: "workspace_2", CWD: "/other", State: protocol.WorkspaceReady},
	}})
	if state.workspaceFilePicker.Open || state.workspaceID != "workspace_2" || state.session.CWD != "/other" {
		t.Fatalf("cwd invalidation = open:%v workspace:%q cwd:%q", state.workspaceFilePicker.Open, state.workspaceID, state.session.CWD)
	}
	select {
	case <-pickerContext.Done():
	default:
		t.Fatal("cwd invalidation did not cancel picker work")
	}
}

func TestOlderSnapshotCannotRegressWorkspaceAfterCWDMetadata(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	controller.setWorkspace(protocol.WorkspaceRef{WorkspaceID: "workspace_2", CWD: "/other", State: protocol.WorkspaceReady})
	state := appState{
		session: protocol.SessionInfo{ID: "session_1", Name: "current", CWD: "/other"}, workspaceID: "workspace_2",
		workspaceFilePicker: controller, metadataStreamID: "stream_1", metadataSequence: 2,
	}
	state.applySnapshot(protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "session_1", Name: "old", CWD: "/repo"}, EventStreamID: "stream_1", EventCursor: 1,
		Workspace: &protocol.WorkspaceRef{SessionID: "session_1", WorkspaceID: "workspace_1", CWD: "/repo", State: protocol.WorkspaceReady},
	})
	if state.workspaceID != "workspace_2" || state.session.CWD != "/other" || !state.workspaceFilePicker.Open {
		t.Fatalf("older snapshot regressed workspace: id=%q cwd=%q open=%v", state.workspaceID, state.session.CWD, state.workspaceFilePicker.Open)
	}
}

func TestDifferentStreamOrdinarySnapshotCannotRegressWorkspaceMetadata(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	controller.setWorkspace(protocol.WorkspaceRef{WorkspaceID: "workspace_2", CWD: "/other", State: protocol.WorkspaceReady})
	state := appState{
		session: protocol.SessionInfo{ID: "session_1", Name: "current", CWD: "/other"}, workspaceID: "workspace_2",
		workspaceFilePicker: controller, metadataStreamID: "stream_new", metadataSequence: 2,
	}
	state.applySnapshot(protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "session_1", Name: "old", CWD: "/repo"}, EventStreamID: "stream_old", EventCursor: 99,
		Workspace: &protocol.WorkspaceRef{SessionID: "session_1", WorkspaceID: "workspace_1", CWD: "/repo", State: protocol.WorkspaceReady},
	})
	if state.workspaceID != "workspace_2" || state.session.CWD != "/other" || !state.workspaceFilePicker.Open {
		t.Fatalf("different-stream snapshot regressed workspace: id=%q cwd=%q open=%v", state.workspaceID, state.session.CWD, state.workspaceFilePicker.Open)
	}
}

func TestWorkspaceFileActivationRejectsOldWorkspaceDescriptor(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	state := appState{workspaceID: "workspace_2", workspaceFilePicker: controller}
	row := workspaceFilePickerRow{Kind: workspaceFilePickerEntryRow,
		Key:   workspaceFileKey{WorkspaceID: "workspace_1", Path: "old.go"},
		Entry: protocol.WorkspaceDirectoryEntry{Path: "old.go", Name: "old.go", Kind: protocol.WorkspaceEntryFile},
	}
	if state.workspaceFilePickerActivationCurrent(row) {
		t.Fatal("old workspace descriptor passed activation guard")
	}
	state.workspaceID = "workspace_1"
	if !state.workspaceFilePickerActivationCurrent(row) {
		t.Fatal("current workspace descriptor failed activation guard")
	}
}

func TestWorkspaceFilePickerCloseDiscardsInflightCompletion(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, serial := controller.beginLoad("", "")
	job := workspaceFilePickerJob{key: key, serial: serial, generation: controller.Generation}
	state := appState{workspaceFilePicker: controller}
	state.closeWorkspaceFilePicker()
	state.completeWorkspaceFilePickerJob(job, directoryPage("", "dir_late", "", protocol.WorkspaceDirectoryEntry{Path: "late", Name: "late", Kind: protocol.WorkspaceEntryFile}), nil)
	if len(state.workspaceFilePicker.observations) != 0 || state.workspaceFilePicker.Open {
		t.Fatalf("late completion mutated closed picker: %+v", state.workspaceFilePicker)
	}
}

func TestWorkspaceFilePickerZeroPendingReleasesSlotBeforeStaleCursorRestart(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, first := controller.beginLoad("", "")
	controller.applyPage(key, first, "", directoryPage("", "dir_root", "opaque"))
	_, serial := controller.beginLoad("", "opaque")
	restartedPath, restartedCursor := "unrequested", "unrequested"
	activeAtRestart := -1
	files := &pickerWorkspaceSession{limits: protocol.WorkspaceLimits{MaxActiveRequests: 1, MaxPendingRequests: 0}}
	state := appState{workspaceFilePicker: controller, workspaceFilePickerActive: 1, workspaceFilePickerMaxActive: 1, workspaceFilePickerMaxPending: 0}
	state.filePickerLoadHook = func(path, cursor string) {
		restartedPath, restartedCursor = path, cursor
		activeAtRestart = state.workspaceFilePickerActive
	}
	job := workspaceFilePickerJob{key: key, serial: serial, generation: controller.Generation, workspaceID: "workspace_1", cursor: "opaque"}
	state.deliverWorkspaceFilePickerJob(files, job, protocol.DirectoryPage{}, &protocol.WorkspaceError{Code: protocol.WorkspaceErrorStaleCursor, Message: "cursor expired"})
	if restartedPath != "" || restartedCursor != "" || activeAtRestart != 0 {
		t.Fatalf("stale cursor restart = path:%q cursor:%q active:%d", restartedPath, restartedCursor, activeAtRestart)
	}
}

func TestCurrentStaleWorkspaceCompletionRequestsRefresh(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, serial := controller.beginLoad("", "")
	refreshes := 0
	state := appState{workspaceFilePicker: controller, filePickerRefreshHook: func() { refreshes++ }}
	state.completeWorkspaceFilePickerJob(workspaceFilePickerJob{key: key, serial: serial, generation: controller.Generation}, protocol.DirectoryPage{},
		&protocol.WorkspaceError{Code: protocol.WorkspaceErrorStaleWorkspace, Message: "workspace changed"})
	if refreshes != 1 {
		t.Fatalf("stale workspace refresh count = %d", refreshes)
	}
}

func TestSupersededStaleWorkspaceCompletionIsIgnored(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, stale := controller.beginLoad("", "")
	controller.beginLoad("", "")
	generation := controller.Generation
	state := appState{workspaceFilePicker: controller}
	state.completeWorkspaceFilePickerJob(workspaceFilePickerJob{key: key, serial: stale, generation: generation}, protocol.DirectoryPage{},
		&protocol.WorkspaceError{Code: protocol.WorkspaceErrorStaleWorkspace, Message: "workspace changed"})
	if !state.workspaceFilePicker.Open || state.workspaceFilePicker.Generation != generation {
		t.Fatalf("superseded stale workspace changed invocation: %+v", state.workspaceFilePicker)
	}
}
