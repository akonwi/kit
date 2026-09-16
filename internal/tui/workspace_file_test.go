package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/highlight"
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type fileViewerSession struct {
	mu        sync.Mutex
	reads     []protocol.ReadWorkspaceFileInput
	results   []protocol.WorkspaceFileRead
	errors    []error
	block     chan struct{}
	canceled  chan struct{}
	completed chan struct{}
}

func (s *fileViewerSession) WorkspaceLimits() protocol.WorkspaceLimits {
	return protocol.DefaultWorkspaceLimits()
}

func (s *fileViewerSession) Workspace(context.Context) (protocol.WorkspaceRef, error) {
	return protocol.WorkspaceRef{}, nil
}

func (s *fileViewerSession) ListDirectory(context.Context, protocol.ListDirectoryInput) (protocol.DirectoryPage, error) {
	return protocol.DirectoryPage{}, nil
}

func (s *fileViewerSession) ReadWorkspaceFile(ctx context.Context, input protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	defer func() {
		if s.completed != nil {
			select {
			case s.completed <- struct{}{}:
			default:
			}
		}
	}()
	s.mu.Lock()
	index := len(s.reads)
	s.reads = append(s.reads, input)
	block := s.block
	var result protocol.WorkspaceFileRead
	var err error
	if index < len(s.results) {
		result = s.results[index]
	}
	if index < len(s.errors) {
		err = s.errors[index]
	}
	s.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			// Let the test runtime finish the build that delivered cancellation;
			// unlike the production runtime, uitest dispatches synchronously.
			time.Sleep(2 * time.Millisecond)
			if s.canceled != nil {
				select {
				case s.canceled <- struct{}{}:
				default:
				}
			}
			return protocol.WorkspaceFileRead{}, ctx.Err()
		}
	} else {
		// Keep the fake asynchronous so an immediate test runtime cannot deliver
		// completion while the pane's initial build is still mounting.
		time.Sleep(time.Millisecond)
	}
	return result, err
}

func (s *fileViewerSession) inputs() []protocol.ReadWorkspaceFileInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.ReadWorkspaceFileInput(nil), s.reads...)
}

func fileViewerRead(workspaceID, path, revision, content string) protocol.WorkspaceFileRead {
	return protocol.WorkspaceFileRead{
		SessionID: "session_1", Workspace: protocol.WorkspaceRef{SessionID: "session_1", WorkspaceID: workspaceID, CWD: "/repo", State: protocol.WorkspaceReady},
		Path: path, Revision: revision, Size: int64(len(content)), Encoding: "utf-8", Content: content,
		ReturnedBytes: len(content), ReturnedLines: len(workspaceFileLines(content)),
	}
}

type filePaneHarnessModel struct {
	descriptor      workspacePaneDescriptor
	workspace       string
	active          bool
	show            bool
	files           *fileViewerSession
	highlighter     highlight.Highlighter
	focused         int
	state           *filePaneHarnessState
	dispatchMu      sync.Mutex
	pendingDispatch []func()
}

type filePaneHarness struct{ model *filePaneHarnessModel }

func (w filePaneHarness) CreateState() ui.State {
	state := &filePaneHarnessState{}
	w.model.state = state
	return state
}

type filePaneHarnessState struct{ ui.StateBase }

func (s *filePaneHarnessState) Build(ui.BuildContext) ui.Widget {
	model := s.Widget().(filePaneHarness).model
	if !model.show {
		return ui.SizedBox{}
	}
	return ui.SelectionArea{Child: workspaceFilePane{
		Descriptor: model.descriptor, CurrentWorkspaceID: model.workspace, Files: model.files, Highlighter: model.highlighter, Dispatch: model.queueDispatch,
		Presentation:   workspacePanePresentation{Active: model.active, Visible: model.active, Focused: model.active},
		OnFocusRequest: func(ui.EventContext) { model.focused++ },
	}}
}

func (m *filePaneHarnessModel) queueDispatch(callback func()) {
	m.dispatchMu.Lock()
	defer m.dispatchMu.Unlock()
	m.pendingDispatch = append(m.pendingDispatch, callback)
}

func (m *filePaneHarnessModel) flushDispatch() {
	for {
		m.dispatchMu.Lock()
		callbacks := m.pendingDispatch
		m.pendingDispatch = nil
		m.dispatchMu.Unlock()
		if len(callbacks) == 0 {
			return
		}
		for _, callback := range callbacks {
			callback()
		}
	}
}

func (m *filePaneHarnessModel) update(change func()) {
	m.flushDispatch()
	m.state.SetState(change)
}

func pumpUntil(t *testing.T, app *uitest.App, model *filePaneHarnessModel, width, height int, text string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// The production runtime serializes Dispatch on its event loop. Uitest's
		// runtime is synchronous, so let the fake read complete before pumping.
		time.Sleep(3 * time.Millisecond)
		model.update(func() {})
		app.Pump(width, height)
		if app.Contains(text) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in:\n%s", text, app.Text())
}

func TestWorkspaceFileViewerCentersLoadingStateHorizontally(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	files := &fileViewerSession{block: block}
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace_a", "cmd/main.go"), workspace: "workspace_a", active: true, show: true, files: files}
	app := uitest.New(filePaneHarness{model: model})
	const width, height = 40, 8
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	label := []rune("Loading file…")
	labelColumn := -1
	for row := range height {
		for column := 0; column+len(label) <= width; column++ {
			matches := true
			for offset, want := range label {
				if app.Cell(column+offset, row).Grapheme != string(want) {
					matches = false
					break
				}
			}
			if matches {
				labelColumn = column
			}
		}
	}
	wantColumn := (width-(len(label)+2))/2 + 2 // spinner, gap, then label
	if labelColumn != wantColumn {
		t.Fatalf("loading label column = %d, want %d:\n%s", labelColumn, wantColumn, strings.Join(rows, "\n"))
	}
}

func TestWorkspaceFileViewerReadCompletionSchedulesRebuild(t *testing.T) {
	files := &fileViewerSession{
		results:   []protocol.WorkspaceFileRead{fileViewerRead("workspace_a", "main.go", "file_one", "package p\n")},
		completed: make(chan struct{}, 1),
	}
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace_a", "main.go"), workspace: "workspace_a", active: true, show: true, files: files}
	app := uitest.New(filePaneHarness{model: model})
	app.Pump(40, 8)
	select {
	case <-files.completed:
	case <-time.After(time.Second):
		t.Fatal("file read did not complete")
	}
	// No harness/root SetState call: the read completion must dirty its own pane.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		model.flushDispatch()
		app.Pump(40, 8)
		if app.Contains("package p") {
			return
		}
	}
	t.Fatalf("completed read remained in loading state:\n%s", app.Text())
}

type controlledFileHighlighter struct {
	release   chan struct{}
	completed chan struct{}
}

func (h *controlledFileHighlighter) Highlight(ctx context.Context, request highlight.Request) highlight.Result {
	select {
	case <-h.release:
	case <-ctx.Done():
		return highlight.Plain(request, ctx.Err())
	}
	result := highlight.Result{Source: highlight.Sanitize(request.Source), Language: "go", Spans: []highlight.Span{{Start: 0, End: 7, Role: highlight.Keyword}}}
	close(h.completed)
	return result
}

func TestWorkspaceFileViewerHighlightCompletionSchedulesRebuild(t *testing.T) {
	files := &fileViewerSession{
		results:   []protocol.WorkspaceFileRead{fileViewerRead("workspace_a", "main.go", "file_one", "package p\n")},
		completed: make(chan struct{}, 1),
	}
	highlighter := &controlledFileHighlighter{release: make(chan struct{}), completed: make(chan struct{})}
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace_a", "main.go"), workspace: "workspace_a", active: true, show: true, files: files, highlighter: highlighter}
	app := uitest.New(filePaneHarness{model: model})
	const width, height = 40, 8
	app.Pump(width, height)
	select {
	case <-files.completed:
	case <-time.After(time.Second):
		t.Fatal("file read did not complete")
	}
	deadline := time.Now().Add(time.Second)
	for !app.Contains("package p") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		model.flushDispatch()
		app.Pump(width, height)
	}
	rows := paintedRows(app, width, height)
	column, row := findTextCell(t, rows, "package")
	plainStyle := app.Cell(column, row).Style

	close(highlighter.release)
	select {
	case <-highlighter.completed:
	case <-time.After(time.Second):
		t.Fatal("highlight did not complete")
	}
	// Again, only the pane's queued dispatch may schedule the semantic repaint.
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		model.flushDispatch()
		app.Pump(width, height)
		if app.Cell(column, row).Style.Foreground != plainStyle.Foreground {
			return
		}
	}
	t.Fatal("completed highlight did not repaint semantic styling")
}

func TestWorkspaceFileViewerPresentsAlignedSelectableHighlightedContent(t *testing.T) {
	files := &fileViewerSession{results: []protocol.WorkspaceFileRead{
		fileViewerRead("workspace_a", "cmd/main.go", "file_one", "package main\n\nfunc main() {\n\tvalue := 42\n}\n"),
	}}
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace_a", "cmd/main.go"), workspace: "workspace_a", active: true, show: true, files: files}
	app := uitest.New(filePaneHarness{model: model})
	pumpUntil(t, app, model, 48, 10, "func main()")
	rows := paintedRows(app, 48, 10)
	visible := strings.Join(rows, "\n")
	for _, want := range []string{"cmd/main.go", "Ln 1 · 5 lines · 43 B", "1 │ package main", "4 │     value := 42", "↑↓ lines · ←→ columns · r refresh"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("file viewer missing %q:\n%s", want, visible)
		}
	}
	packageColumn, packageRow := findTextCell(t, rows, "package")
	mainColumn, mainRow := findTextCell(t, rows, "main")
	if app.Cell(packageColumn, packageRow).Style.Foreground == app.Cell(mainColumn, mainRow).Style.Foreground {
		t.Fatal("keyword and identifier did not receive distinct syntax styles")
	}
	firstGutter, _ := findTextCell(t, rows, "1 │")
	fourthGutter, _ := findTextCell(t, rows, "4 │")
	if firstGutter != fourthGutter {
		t.Fatalf("line-number gutters are not aligned: %d != %d", firstGutter, fourthGutter)
	}
	app.Click(packageColumn, packageRow)
	if model.focused != 1 {
		t.Fatalf("primary mouse focus requests = %d, want 1", model.focused)
	}
	// RichText is used deliberately: it participates in the enclosing
	// SelectionArea and preserves source order for drag selection and copy.
	result := highlight.Result{Source: "package main", Spans: []highlight.Span{{Start: 0, End: 7, Role: highlight.Keyword}}}
	if spans := workspaceFileSpans(result, ui.Theme{}, semanticFallback(ui.Theme{})); len(spans) < 3 {
		t.Fatalf("selectable rich-text spans = %d", len(spans))
	}
}

func TestWorkspaceFileViewerUsesGuardedReadsRefreshAndRevealWithoutReorder(t *testing.T) {
	first := fileViewerRead("workspace_a", "main.go", "file_loaded", "one\ntwo\nthree\nfour\n")
	second := fileViewerRead("workspace_a", "main.go", "file_new", "one\ntwo changed\nthree\nfour\n")
	files := &fileViewerSession{
		results: []protocol.WorkspaceFileRead{first, {}, second},
		errors:  []error{nil, &protocol.WorkspaceError{Code: protocol.WorkspaceErrorStaleFile, Message: "changed"}, nil},
	}
	descriptor := fileWorkspacePane("workspace_a", "main.go")
	descriptor.ExpectedRevision = "file_hint"
	descriptor.RevealStartLine = 4
	descriptor.OpenGeneration = 1
	model := &filePaneHarnessModel{descriptor: descriptor, workspace: "workspace_a", active: true, show: true, files: files}
	app := uitest.New(filePaneHarness{model: model})
	pumpUntil(t, app, model, 40, 8, "four")
	inputs := files.inputs()
	if len(inputs) != 1 || inputs[0].ExpectedFileRevision != "file_hint" {
		t.Fatalf("initial guarded read = %+v", inputs)
	}
	model.update(func() {
		model.descriptor.OpenGeneration++
		model.descriptor.RevealStartLine = 40
	})
	app.Pump(40, 8)
	if !app.Contains("Ln 4") || app.Contains("Ln 40") {
		t.Fatalf("out-of-range repeated reveal was not clamped:\n%s", app.Text())
	}
	app.Send(vaxis.Key{Keycode: 'r', Text: "r"})
	pumpUntil(t, app, model, 40, 8, "File changed")
	inputs = files.inputs()
	if len(inputs) != 2 || inputs[1].ExpectedFileRevision != "file_loaded" {
		t.Fatalf("refresh guarded read = %+v", inputs)
	}
	app.Send(vaxis.Key{Keycode: 'r', Text: "r"})
	pumpUntil(t, app, model, 40, 8, "two changed")
	inputs = files.inputs()
	if len(inputs) != 3 || inputs[2].ExpectedFileRevision != "" {
		t.Fatalf("stale acceptance read = %+v", inputs)
	}

	var controller workspaceController
	firstPane := fileWorkspacePane("workspace_a", "main.go")
	firstPane.RevealStartLine = 2
	_, added, err := controller.Open(firstPane)
	if err != nil || !added {
		t.Fatalf("first open = added:%t err:%v", added, err)
	}
	other := fileWorkspacePane("workspace_a", "other.go")
	_, _, _ = controller.Open(other)
	reopen := fileWorkspacePane("workspace_a", "main.go")
	reopen.RevealStartLine, reopen.RevealEndLine = 40, 44
	_, added, err = controller.Open(reopen)
	panes := controller.Panes()
	if err != nil || added || len(panes) != 2 || panes[0].Path != "main.go" || panes[0].RevealStartLine != 40 || panes[0].OpenGeneration == 0 {
		t.Fatalf("repeated anchored open = added:%t err:%v panes:%+v", added, err, panes)
	}
}

func TestWorkspaceFileViewerRetainsCursorAndBoundedHorizontalPosition(t *testing.T) {
	files := &fileViewerSession{results: []protocol.WorkspaceFileRead{fileViewerRead("workspace_a", "wide.go", "file_wide", "first ABCDEFGHIJKLMNOPQRSTUVWXYZ\nsecond line\nthird line\n")}}
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace_a", "wide.go"), workspace: "workspace_a", active: true, show: true, files: files}
	app := uitest.New(filePaneHarness{model: model})
	pumpUntil(t, app, model, 26, 8, "ABCDEFGHI")
	app.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	app.Send(vaxis.Key{Keycode: vaxis.KeyRight})
	app.Send(vaxis.Key{Keycode: vaxis.KeyRight})
	app.Pump(26, 8)
	if !app.Contains("Ln 2") {
		t.Fatalf("vertical navigation did not retain the cursor:\n%s", app.Text())
	}
	afterScroll := strings.Join(paintedRows(app, 26, 8), "\n")
	if strings.Contains(afterScroll, "1 │ first") {
		t.Fatalf("horizontal navigation did not move the bounded viewport:\n%s", afterScroll)
	}
	model.update(func() { model.active = false })
	app.Pump(34, 10)
	model.update(func() { model.active = true })
	app.Pump(34, 10)
	if !app.Contains("Ln 2") {
		t.Fatalf("tab switch/resize lost the retained cursor:\n%s", app.Text())
	}
}

func TestWorkspaceFileViewerFreezesOldWorkspaceContentAndPosition(t *testing.T) {
	files := &fileViewerSession{results: []protocol.WorkspaceFileRead{fileViewerRead("workspace_old", "same.go", "file_old", "old evidence\nsecond\nthird\n")}}
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace_old", "same.go"), workspace: "workspace_old", active: true, show: true, files: files}
	app := uitest.New(filePaneHarness{model: model})
	pumpUntil(t, app, model, 38, 8, "old evidence")
	app.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	model.update(func() { model.workspace = "workspace_new" })
	app.Pump(38, 8)
	text := strings.Join(paintedRows(app, 38, 8), "\n")
	for _, want := range []string{"old evidence", "stale · frozen", "Frozen evidence · navigation only"} {
		if !strings.Contains(text, want) {
			t.Fatalf("frozen pane missing %q:\n%s", want, text)
		}
	}
	app.Send(vaxis.Key{Keycode: 'r', Text: "r"})
	app.Pump(38, 8)
	if got := len(files.inputs()); got != 1 {
		t.Fatalf("frozen pane performed %d reads, want 1", got)
	}
	var controller workspaceController
	_, _, _ = controller.Open(fileWorkspacePane("workspace_old", "same.go"))
	_, _, _ = controller.Open(fileWorkspacePane("workspace_new", "same.go"))
	if len(controller.Panes()) != 2 {
		t.Fatalf("new workspace path did not create a distinct pane: %+v", controller.Panes())
	}
}

func TestWorkspaceFileViewerCancelsHiddenAndDisposedReads(t *testing.T) {
	files := &fileViewerSession{block: make(chan struct{}), canceled: make(chan struct{}, 2)}
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace_a", "main.go"), workspace: "workspace_a", active: true, show: true, files: files}
	app := uitest.New(filePaneHarness{model: model})
	app.Pump(40, 8)
	model.update(func() { model.active = false })
	app.Pump(40, 8)
	select {
	case <-files.canceled:
	case <-time.After(time.Second):
		t.Fatal("hidden pane did not cancel its active read")
	}
	time.Sleep(3 * time.Millisecond)
	model.update(func() { model.active = true })
	app.Pump(40, 8)
	model.update(func() { model.show = false })
	app.Pump(40, 8)
	select {
	case <-files.canceled:
	case <-time.After(time.Second):
		t.Fatal("disposed pane did not cancel its active read")
	}
	time.Sleep(3 * time.Millisecond)
}

type blockingFileHighlighter struct {
	mu       sync.Mutex
	calls    int
	canceled chan struct{}
}

func (h *blockingFileHighlighter) Highlight(ctx context.Context, request highlight.Request) highlight.Result {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	<-ctx.Done()
	select {
	case h.canceled <- struct{}{}:
	default:
	}
	return highlight.Plain(request, ctx.Err())
}

func (h *blockingFileHighlighter) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

func TestWorkspaceFileViewerCancelsHighlightingAndDoesNotParseWhileHidden(t *testing.T) {
	highlighter := &blockingFileHighlighter{canceled: make(chan struct{}, 2)}
	files := &fileViewerSession{results: []protocol.WorkspaceFileRead{fileViewerRead("workspace_a", "main.go", "one", "package main\n")}}
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace_a", "main.go"), workspace: "workspace_a", active: true, show: true, files: files, highlighter: highlighter}
	app := uitest.New(filePaneHarness{model: model})
	pumpUntil(t, app, model, 40, 8, "package main")
	deadline := time.Now().Add(time.Second)
	for highlighter.callCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	model.update(func() { model.active = false })
	app.Pump(40, 8)
	select {
	case <-highlighter.canceled:
	case <-time.After(time.Second):
		t.Fatal("hidden pane did not cancel syntax highlighting")
	}
	for range 3 {
		app.Pump(40, 8)
	}
	if calls := highlighter.callCount(); calls != 1 {
		t.Fatalf("hidden pane started %d highlights, want 1", calls)
	}
}

func TestWorkspaceFileViewerPreservesUnicodeAndNeutralizesUnsafeFormatting(t *testing.T) {
	result := highlight.Plain(highlight.Request{Path: "main.go", Source: "const 名前 = \"世界\"\nleft\u202eright"}, nil)
	spans := workspaceFileSpans(result, ui.Theme{}, semanticFallback(ui.Theme{}))
	var rendered strings.Builder
	for _, span := range spans {
		rendered.WriteString(span.Text)
	}
	if got := rendered.String(); got != "1 │ const 名前 = \"世界\"\n2 │ left�right" {
		t.Fatalf("sanitized viewer text = %q", got)
	}
}

func TestWorkspaceFileViewerDiscardsStaleHighlightGeneration(t *testing.T) {
	state := &workspaceFilePaneState{highlightGeneration: 4, pendingHighlight: &workspaceHighlightResult{
		generation: 3,
		result:     highlight.Result{Source: "stale", Spans: []highlight.Span{{Start: 0, End: 5, Role: highlight.Keyword}}},
	}}
	state.applyPendingResults()
	if state.highlightReady || state.highlighted.Source != "" {
		t.Fatalf("stale highlight applied: %+v", state.highlighted)
	}
	state.pendingHighlight = &workspaceHighlightResult{generation: 4, result: highlight.Result{Source: "current"}}
	state.applyPendingResults()
	if !state.highlightReady || state.highlighted.Source != "current" {
		t.Fatalf("current highlight not applied: %+v", state.highlighted)
	}
}

func TestWorkspaceFileViewerCompletionQueueRetainsNewestGeneration(t *testing.T) {
	state := &workspaceFilePaneState{}
	for generation := uint64(1); generation <= 8; generation++ {
		state.queueResult(workspaceFileResult{generation: generation})
	}
	state.queueResult(workspaceFileResult{generation: 3})
	state.resultMu.Lock()
	defer state.resultMu.Unlock()
	if state.pendingResult == nil || state.pendingResult.generation != 8 {
		t.Fatalf("pending completion = %+v, want generation 8", state.pendingResult)
	}
}

func TestWorkspaceFileViewerClassifiesSafeExplicitStates(t *testing.T) {
	cases := map[protocol.WorkspaceErrorCode]workspaceFileLoadState{
		protocol.WorkspaceErrorStaleWorkspace:   workspaceFileFrozen,
		protocol.WorkspaceErrorStaleFile:        workspaceFileStaleFile,
		protocol.WorkspaceErrorBinary:           workspaceFileBinary,
		protocol.WorkspaceErrorNotFound:         workspaceFileMissing,
		protocol.WorkspaceErrorPermissionDenied: workspaceFilePermission,
		protocol.WorkspaceErrorUnavailable:      workspaceFileUnavailable,
		protocol.WorkspaceErrorCapacity:         workspaceFileCapacity,
	}
	for code, want := range cases {
		if got := classifyWorkspaceFileError(&protocol.WorkspaceError{Code: code, Message: "host-safe"}); got != want {
			t.Errorf("%s classified as %v, want %v", code, got, want)
		}
	}
	if got := classifyWorkspaceFileError(context.Canceled); got != workspaceFileCanceled {
		t.Fatalf("cancellation classified as %v", got)
	}
	if got := classifyWorkspaceFileError(errors.New("/secret/host/path")); got != workspaceFileFailed {
		t.Fatalf("generic error classified as %v", got)
	}
	state := &workspaceFilePaneState{loadState: workspaceFileFailed}
	message, detail := state.emptyStateText()
	if strings.Contains(message+detail, "/secret") || message != "Could not load file" {
		t.Fatalf("generic error was not sanitized: %q %q", message, detail)
	}
}

func TestWorkspaceFileViewerEmptyTruncatedAndErrorPresentation(t *testing.T) {
	tests := []struct {
		name string
		read protocol.WorkspaceFileRead
		err  error
		want string
	}{
		{name: "empty", read: fileViewerRead("workspace_a", "empty.txt", "file_empty", ""), want: "Empty file"},
		{name: "truncated", read: func() protocol.WorkspaceFileRead {
			r := fileViewerRead("workspace_a", "large.txt", "file_large", "partial\n")
			r.Truncated = true
			r.TruncationReason = "line_limit"
			return r
		}(), want: "Preview truncated (line limit)"},
		{name: "binary", err: &protocol.WorkspaceError{Code: protocol.WorkspaceErrorBinary, Message: "binary"}, want: "Binary file"},
		{name: "permission", err: &protocol.WorkspaceError{Code: protocol.WorkspaceErrorPermissionDenied, Message: "denied"}, want: "File is unreadable"},
		{name: "capacity", err: &protocol.WorkspaceError{Code: protocol.WorkspaceErrorCapacity, Message: "busy"}, want: "Workspace is busy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := &fileViewerSession{results: []protocol.WorkspaceFileRead{test.read}, errors: []error{test.err}}
			path := test.read.Path
			if path == "" {
				path = test.name + ".txt"
			}
			model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace_a", path), workspace: "workspace_a", active: true, show: true, files: files}
			app := uitest.New(filePaneHarness{model: model})
			pumpUntil(t, app, model, 44, 8, test.want)
		})
	}
}
