package tui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

const (
	toolNavigationSessionID   = "session_tool_navigation"
	toolNavigationWorkspaceID = "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	toolNavigationRevision    = "file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

type toolNavigationSession struct {
	fakeSession

	mu              sync.Mutex
	workspace       protocol.WorkspaceRef
	workspaceErr    error
	workspaceGate   <-chan struct{}
	readErr         error
	readGate        <-chan struct{}
	readGatesByPath map[string]<-chan struct{}
	readStarted     chan struct{}
	readInputs      []protocol.ReadWorkspaceFileInput
}

func (s *toolNavigationSession) WorkspaceLimits() protocol.WorkspaceLimits {
	return protocol.DefaultWorkspaceLimits()
}

func (s *toolNavigationSession) Workspace(ctx context.Context) (protocol.WorkspaceRef, error) {
	if s.workspaceGate != nil {
		select {
		case <-s.workspaceGate:
		case <-ctx.Done():
			return protocol.WorkspaceRef{}, ctx.Err()
		}
	}
	return s.workspace, s.workspaceErr
}

func (*toolNavigationSession) ListDirectory(context.Context, protocol.ListDirectoryInput) (protocol.DirectoryPage, error) {
	panic("unexpected ListDirectory")
}

func (s *toolNavigationSession) ReadWorkspaceFile(ctx context.Context, input protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	s.mu.Lock()
	s.readInputs = append(s.readInputs, input)
	started := s.readStarted
	s.readStarted = nil
	s.mu.Unlock()
	if started != nil {
		close(started)
	}
	if gate := s.readGatesByPath[input.Path]; gate != nil {
		// Deliberately permit a non-cooperative backend in latest-click tests.
		<-gate
	} else if s.readGate != nil {
		select {
		case <-s.readGate:
		case <-ctx.Done():
			return protocol.WorkspaceFileRead{}, ctx.Err()
		}
	}
	if s.readErr != nil {
		return protocol.WorkspaceFileRead{}, s.readErr
	}
	content := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten"
	return protocol.WorkspaceFileRead{
		SessionID: s.workspace.SessionID, Workspace: s.workspace, Path: input.Path, Revision: toolNavigationRevision,
		Encoding: "utf-8", Content: content, Size: int64(len(content)), ReturnedBytes: len(content), ReturnedLines: 10,
	}, nil
}

func (s *toolNavigationSession) inputs() []protocol.ReadWorkspaceFileInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.ReadWorkspaceFileInput(nil), s.readInputs...)
}

type toolNavigationDispatch struct {
	mu        sync.Mutex
	callbacks []func()
}

func (d *toolNavigationDispatch) dispatch(callback func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callbacks = append(d.callbacks, callback)
}

func (d *toolNavigationDispatch) pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.callbacks)
}

func (d *toolNavigationDispatch) flush() {
	d.mu.Lock()
	callbacks := d.callbacks
	d.callbacks = nil
	d.mu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
}

type toolNavigationHarness struct{ state *toolNavigationState }

func (w toolNavigationHarness) CreateState() ui.State { return w.state }

type toolNavigationState struct{ appState }

func (*toolNavigationState) InitState()                      {}
func (*toolNavigationState) Dispose()                        {}
func (*toolNavigationState) Build(ui.BuildContext) ui.Widget { return ui.Text{Value: "mounted"} }

func mountedToolNavigation(t *testing.T, backend *toolNavigationSession) (*uitest.App, *toolNavigationState, *toolNavigationDispatch, *[]toastInput) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	toasts := []toastInput{}
	state := &toolNavigationState{appState: appState{
		phase: phaseReady, operation: 7, session: protocol.SessionInfo{ID: toolNavigationSessionID, CWD: "/repo"},
		workspaceID: toolNavigationWorkspaceID, bound: backend, ctx: ctx, attachmentCtx: ctx, attachmentCancel: cancel,
		showToastOverride: func(input toastInput) { toasts = append(toasts, input) },
	}}
	application := uitest.New(toolNavigationHarness{state: state})
	application.Pump(40, 8)
	return application, state, &toolNavigationDispatch{}, &toasts
}

func readyToolNavigationSession() *toolNavigationSession {
	workspace := protocol.WorkspaceRef{
		SessionID: toolNavigationSessionID, CWD: "/repo", WorkspaceID: toolNavigationWorkspaceID,
		State: protocol.WorkspaceReady, Limits: protocol.DefaultWorkspaceLimits(),
	}
	return &toolNavigationSession{fakeSession: fakeSession{id: toolNavigationSessionID}, workspace: workspace}
}

func pumpToolNavigationUntil(t *testing.T, application *uitest.App, dispatch *toolNavigationDispatch, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() && time.Now().Before(deadline) {
		dispatch.flush()
		application.Pump(40, 8)
		time.Sleep(time.Millisecond)
	}
	if !condition() {
		t.Fatal("timed out waiting for tool file navigation")
	}
}

func TestOpenToolFileResolvesValidatesAndRevealsRange(t *testing.T) {
	backend := readyToolNavigationSession()
	application, state, dispatch, toasts := mountedToolNavigation(t, backend)
	target := toolFileTarget{
		SessionID: toolNavigationSessionID, WorkspaceID: toolNavigationWorkspaceID, CWD: "/repo",
		Path: "pkg/../main.go", StartLine: 2, EndLine: 3,
	}
	state.openToolFileWithDispatch(target, dispatch.dispatch)
	pumpToolNavigationUntil(t, application, dispatch, func() bool { return len(state.workspace.Panes()) == 1 })

	inputs := backend.inputs()
	panes := state.workspace.Panes()
	if len(inputs) != 1 || inputs[0].WorkspaceID != toolNavigationWorkspaceID || inputs[0].Path != "main.go" {
		t.Fatalf("guarded read inputs = %+v", inputs)
	}
	if len(panes) != 1 || panes[0].Path != "main.go" || panes[0].ExpectedRevision != toolNavigationRevision || panes[0].RevealStartLine != 2 || panes[0].RevealEndLine != 3 {
		t.Fatalf("opened panes = %+v", panes)
	}
	if len(*toasts) != 0 {
		t.Fatalf("unexpected toasts = %+v", *toasts)
	}

	generation := panes[0].OpenGeneration
	var otherErr error
	state.SetState(func() {
		_, _, otherErr = state.workspace.Open(fileWorkspacePane(toolNavigationWorkspaceID, "other.go"))
	})
	if otherErr != nil {
		t.Fatal(otherErr)
	}
	target.Path = "/repo/main.go"
	target.StartLine, target.EndLine = 8, 0
	state.openToolFileWithDispatch(target, dispatch.dispatch)
	pumpToolNavigationUntil(t, application, dispatch, func() bool {
		return len(backend.inputs()) == 2 && state.workspace.Panes()[0].OpenGeneration > generation
	})
	panes = state.workspace.Panes()
	if len(panes) != 2 || panes[0].Path != "main.go" || panes[1].Path != "other.go" || panes[0].RevealStartLine != 8 || panes[0].RevealEndLine != 8 {
		t.Fatalf("deduplicated anchored pane order = %+v", panes)
	}

	generation = panes[0].OpenGeneration
	target.StartLine, target.EndLine = int(^uint(0)>>1), int(^uint(0)>>1)
	state.openToolFileWithDispatch(target, dispatch.dispatch)
	pumpToolNavigationUntil(t, application, dispatch, func() bool {
		return len(backend.inputs()) == 3 && state.workspace.Panes()[0].OpenGeneration > generation
	})
	panes = state.workspace.Panes()
	if panes[0].RevealStartLine != 10 || panes[0].RevealEndLine != 10 {
		t.Fatalf("preflight-clamped anchored pane = %+v", panes[0])
	}
}

func TestToolFileRangeWithinReadDoesNotInventWriteOrEditAnchor(t *testing.T) {
	start, end := toolFileRangeWithinRead(0, 0, 10)
	if start != 0 || end != 0 {
		t.Fatalf("unanchored range = %d:%d", start, end)
	}
}

func TestOpenToolFileRejectsUnsafeOrStaleTargetsAndInputOwners(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*toolNavigationState, *toolFileTarget)
	}{
		{name: "outside workspace", mutate: func(_ *toolNavigationState, target *toolFileTarget) { target.Path = "../secret" }},
		{name: "stale session", mutate: func(_ *toolNavigationState, target *toolFileTarget) { target.SessionID = "session_stale" }},
		{name: "stale workspace", mutate: func(_ *toolNavigationState, target *toolFileTarget) {
			target.WorkspaceID = "workspace_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
		}},
		{name: "modal", mutate: func(state *toolNavigationState, _ *toolFileTarget) { state.palette.Open = true }},
		{name: "interaction", mutate: func(state *toolNavigationState, _ *toolFileTarget) {
			state.pendingInteractions = []protocol.InteractionRequest{{ID: "interaction"}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := readyToolNavigationSession()
			_, state, dispatch, toasts := mountedToolNavigation(t, backend)
			target := toolFileTarget{SessionID: toolNavigationSessionID, WorkspaceID: toolNavigationWorkspaceID, CWD: "/repo", Path: "main.go", StartLine: 1, EndLine: 1}
			test.mutate(state, &target)
			state.openToolFileWithDispatch(target, dispatch.dispatch)
			if inputs := backend.inputs(); len(inputs) != 0 {
				t.Fatalf("rejected target read inputs = %+v", inputs)
			}
			if len(state.workspace.Panes()) != 0 || len(*toasts) != 1 || (*toasts)[0].Title != "Could not open file" {
				t.Fatalf("rejection panes=%+v toasts=%+v", state.workspace.Panes(), *toasts)
			}
		})
	}
}

func TestOpenToolFileDropsResultAfterSessionOperationChanges(t *testing.T) {
	gate := make(chan struct{})
	backend := readyToolNavigationSession()
	backend.readGate = gate
	readStarted := make(chan struct{})
	backend.readStarted = readStarted
	application, state, dispatch, toasts := mountedToolNavigation(t, backend)
	state.openToolFileWithDispatch(toolFileTarget{SessionID: toolNavigationSessionID, WorkspaceID: toolNavigationWorkspaceID, CWD: "/repo", Path: "main.go", StartLine: 1, EndLine: 1}, dispatch.dispatch)
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("file read did not start")
	}
	state.SetState(func() {
		state.operation++
		state.session = protocol.SessionInfo{ID: "session_replacement", CWD: "/other"}
		state.workspaceID = "workspace_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	})
	close(gate)
	for range 10 {
		dispatch.flush()
		application.Pump(40, 8)
		time.Sleep(time.Millisecond)
	}
	if len(state.workspace.Panes()) != 0 || len(*toasts) != 0 {
		t.Fatalf("stale result mutated replacement: panes=%+v toasts=%+v", state.workspace.Panes(), *toasts)
	}
}

func TestOpenToolFileReportsWorkspaceFailure(t *testing.T) {
	backend := readyToolNavigationSession()
	backend.workspaceErr = errors.New("workspace lookup failed")
	application, state, dispatch, toasts := mountedToolNavigation(t, backend)
	state.openToolFileWithDispatch(toolFileTarget{SessionID: toolNavigationSessionID, WorkspaceID: toolNavigationWorkspaceID, CWD: "/repo", Path: "main.go"}, dispatch.dispatch)
	pumpToolNavigationUntil(t, application, dispatch, func() bool { return len(*toasts) == 1 })
	if toast := (*toasts)[0]; toast.Title != "Could not open file" || toast.Subtitle != "workspace lookup failed" || toast.Variant != toastWarning {
		t.Fatalf("failure toast = %+v", toast)
	}
}

func TestOpenToolFileLatestClickWinsAgainstNonCooperativeRead(t *testing.T) {
	firstGate := make(chan struct{})
	backend := readyToolNavigationSession()
	backend.readGatesByPath = map[string]<-chan struct{}{"first.go": firstGate}
	readStarted := make(chan struct{})
	backend.readStarted = readStarted
	application, state, dispatch, toasts := mountedToolNavigation(t, backend)
	base := toolFileTarget{SessionID: toolNavigationSessionID, WorkspaceID: toolNavigationWorkspaceID, CWD: "/repo", StartLine: 1, EndLine: 1}
	first := base
	first.Path = "first.go"
	state.openToolFileWithDispatch(first, dispatch.dispatch)
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("first file read did not start")
	}
	second := base
	second.Path, second.StartLine, second.EndLine = "second.go", 7, 9
	state.openToolFileWithDispatch(second, dispatch.dispatch)
	pumpToolNavigationUntil(t, application, dispatch, func() bool { return len(state.workspace.Panes()) == 1 })
	close(firstGate)
	for range 10 {
		dispatch.flush()
		application.Pump(40, 8)
		time.Sleep(time.Millisecond)
	}
	panes := state.workspace.Panes()
	if len(panes) != 1 || panes[0].Path != "second.go" || panes[0].RevealStartLine != 7 || panes[0].RevealEndLine != 9 || len(*toasts) != 0 {
		t.Fatalf("latest navigation lost: panes=%+v toasts=%+v", panes, *toasts)
	}
}

func TestOpenToolFileRejectsCompletionWhenInteractionTakesOwnership(t *testing.T) {
	gate := make(chan struct{})
	backend := readyToolNavigationSession()
	backend.readGate = gate
	readStarted := make(chan struct{})
	backend.readStarted = readStarted
	application, state, dispatch, toasts := mountedToolNavigation(t, backend)
	state.openToolFileWithDispatch(toolFileTarget{SessionID: toolNavigationSessionID, WorkspaceID: toolNavigationWorkspaceID, CWD: "/repo", Path: "main.go"}, dispatch.dispatch)
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("file read did not start")
	}
	state.SetState(func() { state.pendingInteractions = []protocol.InteractionRequest{{ID: "interaction"}} })
	close(gate)
	pumpToolNavigationUntil(t, application, dispatch, func() bool { return len(*toasts) == 1 })
	if len(state.workspace.Panes()) != 0 || (*toasts)[0].Subtitle != "Finish the current interaction before opening a file." {
		t.Fatalf("owned-input completion panes=%+v toasts=%+v", state.workspace.Panes(), *toasts)
	}
}

func TestOpenToolFileReportsDeadlineAndSilencesLifecycleCancellation(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		backend := readyToolNavigationSession()
		backend.workspaceGate = make(chan struct{})
		application, state, dispatch, toasts := mountedToolNavigation(t, backend)
		state.openToolFileWithDispatchTimeout(toolFileTarget{SessionID: toolNavigationSessionID, WorkspaceID: toolNavigationWorkspaceID, CWD: "/repo", Path: "main.go"}, dispatch.dispatch, time.Millisecond)
		pumpToolNavigationUntil(t, application, dispatch, func() bool { return len(*toasts) == 1 })
		if (*toasts)[0].Subtitle != "Timed out while validating the workspace file." {
			t.Fatalf("deadline toast = %+v", (*toasts)[0])
		}
	})
	t.Run("canceled", func(t *testing.T) {
		backend := readyToolNavigationSession()
		backend.workspaceGate = make(chan struct{})
		application, state, dispatch, toasts := mountedToolNavigation(t, backend)
		ctx, cancel := context.WithCancel(t.Context())
		state.attachmentCtx = ctx
		state.openToolFileWithDispatch(toolFileTarget{SessionID: toolNavigationSessionID, WorkspaceID: toolNavigationWorkspaceID, CWD: "/repo", Path: "main.go"}, dispatch.dispatch)
		cancel()
		for range 10 {
			dispatch.flush()
			application.Pump(40, 8)
			time.Sleep(time.Millisecond)
		}
		if len(*toasts) != 0 || len(state.workspace.Panes()) != 0 {
			t.Fatalf("canceled navigation mutated state: panes=%+v toasts=%+v", state.workspace.Panes(), *toasts)
		}
	})
}

func TestOpenToolFileDropsQueuedSuccessAfterLifecycleCancellation(t *testing.T) {
	gate := make(chan struct{})
	backend := readyToolNavigationSession()
	backend.readGate = gate
	readStarted := make(chan struct{})
	backend.readStarted = readStarted
	application, state, dispatch, toasts := mountedToolNavigation(t, backend)
	state.openToolFileWithDispatch(toolFileTarget{SessionID: toolNavigationSessionID, WorkspaceID: toolNavigationWorkspaceID, CWD: "/repo", Path: "main.go"}, dispatch.dispatch)
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("file read did not start")
	}
	close(gate)
	deadline := time.Now().Add(time.Second)
	for dispatch.pending() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if dispatch.pending() == 0 {
		t.Fatal("success callback was not queued")
	}
	state.attachmentCancel()
	dispatch.flush()
	application.Pump(40, 8)
	if len(state.workspace.Panes()) != 0 || len(*toasts) != 0 {
		t.Fatalf("canceled lifecycle accepted queued success: panes=%+v toasts=%+v", state.workspace.Panes(), *toasts)
	}
}
