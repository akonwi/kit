package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"golang.org/x/term"
)

const (
	sessionPickerMinHeight = 10
	sessionPickerMaxHeight = 20
)

// SessionPickerOptions configures the standalone saved-session picker.
type SessionPickerOptions struct {
	Context context.Context
	Server  sessionclient.Server
}

type sessionPickerResult struct {
	mu           sync.Mutex
	selection    string
	regionHeight int
}

func (r *sessionPickerResult) selectSession(sessionID string) {
	r.mu.Lock()
	r.selection = sessionID
	r.mu.Unlock()
}

func (r *sessionPickerResult) selectedSession() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.selection
}

func (r *sessionPickerResult) setRegionHeight(height int) {
	r.mu.Lock()
	r.regionHeight = height
	r.mu.Unlock()
}

func (r *sessionPickerResult) visibleRegionHeight(maxRows int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return min(r.regionHeight, maxRows)
}

// RunSessionPicker runs a bounded primary-screen session manager. It returns an
// exact session ID when the user opens one, or an empty ID when they cancel.
func RunSessionPicker(options SessionPickerOptions) (string, error) {
	if options.Server == nil {
		return "", errors.New("tui: session picker server is required")
	}
	if options.Context == nil {
		options.Context = context.Background()
	}
	parentContext := options.Context
	runContext, cancel := context.WithCancel(parentContext)
	options.Context = runContext
	result := &sessionPickerResult{}
	err := ui.Run(sessionPicker{Options: options, Result: result},
		ui.WithDynamicPrimaryScreen(), ui.WithShortcuts(nativeRootShortcuts()))
	cancel()
	cleanupErr := clearSessionPickerRegion(result)
	if err != nil || cleanupErr != nil {
		return "", errors.Join(err, cleanupErr)
	}
	if err := parentContext.Err(); err != nil {
		return "", err
	}
	return result.selectedSession(), nil
}

type sessionPicker struct {
	Options SessionPickerOptions
	Result  *sessionPickerResult

	initialSet      bool
	initialSessions []protocol.SessionInfo
	initialError    error
}

func (sessionPicker) CreateState() ui.State { return &sessionPickerState{} }

type sessionPickerState struct {
	ui.StateBase
	ctx        context.Context
	cancel     context.CancelFunc
	controller sessionExplorerController
	focus      ui.FocusNode
}

func (s *sessionPickerState) InitState() {
	options := s.Widget().(sessionPicker).Options
	s.ctx, s.cancel = context.WithCancel(options.Context)
	generation := s.controller.Begin("")
	runtime := s.Context().Runtime()
	eventContext := s.Context().EventContext()
	widget := s.Widget().(sessionPicker)
	if widget.initialSet {
		s.controller.Resolve(generation, projectSessionExplorerItems(widget.initialSessions), widget.initialError)
	} else {
		go func() {
			sessions, err := listSessionPickerSessions(s.ctx, options.Server)
			if s.ctx.Err() != nil {
				return
			}
			runtime.Dispatch(func() {
				s.SetState(func() {
					s.controller.Resolve(generation, projectSessionExplorerItems(sessions), err)
				})
			})
		}()
	}
	go func() {
		<-s.ctx.Done()
		if options.Context.Err() != nil {
			runtime.Dispatch(func() { eventContext.Quit() })
		}
	}()
}

func (s *sessionPickerState) Dispose() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *sessionPickerState) TickFrame(_ time.Time) bool {
	return s.controller.TickFrame()
}

func (s *sessionPickerState) Build(ctx ui.BuildContext) ui.Widget {
	widget := s.Widget().(sessionPicker)
	snapshot := s.controller.Snapshot()
	height := sessionPickerHeight(snapshot)
	if widget.Result != nil {
		widget.Result.setRegionHeight(height)
	}
	if snapshot.Layout != nil {
		snapshot.Layout.AvailableRows = max(0, height-sessionExplorerChromeRows)
	}
	surface := sessionExplorerSurface{
		Snapshot: snapshot, Action: "open",
		Callbacks: sessionExplorerCallbacks{Select: func(_ ui.EventContext, sessionID string) {
			s.SetState(func() { s.controller.Select(sessionID) })
		}},
	}
	theme := ui.MustDepend[ui.Theme](ctx)
	content := ui.Widget(ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}},
		boundedHorizontalCenter{
			Percent: 85, MinWidth: 44, MaxWidth: 120,
			Child: ui.SizedBox{Height: height, Child: surface.content(ctx)},
		},
	))
	children := []ui.Widget{content}
	if snapshot.RenameOpen {
		children = append(children, sessionRenameSurface{
			Snapshot: snapshot,
			Callbacks: sessionRenameCallbacks{
				Changed: func(_ ui.EventContext, value string) {
					s.SetState(func() { s.controller.SetRenameText(value) })
				},
				Submitted: func(_ ui.EventContext, value string) {
					s.rename(value, widget.Options.Server)
				},
			},
		})
	}
	if snapshot.DeleteOpen {
		children = append(children, sessionDeleteSurface{Snapshot: snapshot})
	}
	return ui.FocusScope{
		AutoFocus: true, Trap: true,
		Child: ui.Focus(&s.focus, ui.Stack{Alignment: ui.CenterAlign, Children: children}),
	}
}

type boundedHorizontalCenter struct {
	Percent  int
	MinWidth int
	MaxWidth int
	Child    ui.Widget
}

func (w boundedHorizontalCenter) WidgetChild() ui.Widget { return w.Child }

func (w boundedHorizontalCenter) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderBoundedHorizontalCenter{Percent: w.Percent, MinWidth: w.MinWidth, MaxWidth: w.MaxWidth}
}

func (w boundedHorizontalCenter) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderBoundedHorizontalCenter)
	if render.Percent == w.Percent && render.MinWidth == w.MinWidth && render.MaxWidth == w.MaxWidth {
		return
	}
	render.Percent = w.Percent
	render.MinWidth = w.MinWidth
	render.MaxWidth = w.MaxWidth
	render.MarkNeedsLayout()
}

type renderBoundedHorizontalCenter struct {
	ui.SingleChildRenderObject
	Percent  int
	MinWidth int
	MaxWidth int
	offset   ui.Offset
}

func (r *renderBoundedHorizontalCenter) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	r.SetSize(r.layout(ctx, constraints, false))
}

func (r *renderBoundedHorizontalCenter) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return r.layout(ctx, constraints, true)
}

func (r *renderBoundedHorizontalCenter) layout(ctx ui.LayoutContext, constraints ui.Constraints, dry bool) ui.Size {
	available := constraints.MinWidth
	if constraints.HasBoundedWidth() {
		available = constraints.MaxWidth
	}
	width := available * r.Percent / 100
	width = max(r.MinWidth, min(r.MaxWidth, width))
	width = min(available, width)
	childConstraints := ui.Constraints{MinWidth: width, MaxWidth: width, MaxHeight: constraints.MaxHeight}
	height := 0
	if child := r.Child(); child != nil {
		if dry {
			height = ui.DryLayout(ctx, child, childConstraints).Height
		} else {
			child.Layout(ctx, childConstraints)
			height = child.Base().Size().Height
		}
	}
	if !dry {
		r.offset = ui.Offset{X: max(0, (available-width)/2)}
	}
	return constraints.Constrain(ui.Size{Width: available, Height: height})
}

func (r *renderBoundedHorizontalCenter) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(r.offset))
	}
}

func (r *renderBoundedHorizontalCenter) ChildOffset(ui.RenderObject) ui.Offset { return r.offset }

func (*renderBoundedHorizontalCenter) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func clearSessionPickerRegion(result *sessionPickerResult) error {
	if result == nil || result.visibleRegionHeight(sessionPickerMaxHeight) == 0 {
		return nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("tui: open terminal to clear session picker: %w", err)
	}
	defer tty.Close()
	maxRows := sessionPickerMaxHeight
	if _, rows, sizeErr := term.GetSize(int(tty.Fd())); sizeErr == nil && rows > 0 {
		maxRows = rows
	}
	if err := writeSessionPickerCleanup(tty, result.visibleRegionHeight(maxRows)); err != nil {
		return fmt.Errorf("tui: clear session picker: %w", err)
	}
	return nil
}

func writeSessionPickerCleanup(writer io.Writer, regionHeight int) error {
	if regionHeight <= 0 {
		return nil
	}
	_, err := fmt.Fprintf(writer, "\x1b[%dA\r\x1b[J", regionHeight)
	return err
}

func sessionPickerHeight(snapshot sessionExplorerSnapshot) int {
	rows := len(snapshot.Sessions) + sessionExplorerChromeRows
	if snapshot.Loading || snapshot.Error != "" || len(snapshot.Sessions) == 0 {
		rows = sessionPickerMinHeight
	}
	return max(sessionPickerMinHeight, min(sessionPickerMaxHeight, rows))
}

func listSessionPickerSessions(ctx context.Context, server sessionclient.Server) ([]protocol.SessionInfo, error) {
	return server.ListSessions(ctx, "")
}

func (s *sessionPickerState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase {
		return ui.EventIgnored
	}
	key, ok := event.(ui.Key)
	if !ok {
		return ui.EventIgnored
	}
	if s.controller.DeleteOpen {
		if key.EventType == ui.EventRelease {
			return ui.EventHandled
		}
		if key.MatchString("Escape") || key.MatchString("Ctrl+c") {
			s.SetState(func() { s.controller.CancelDelete() })
			return ui.EventHandled
		}
		if key.EventType != vaxis.EventPaste && key.MatchString("Enter") {
			s.deleteSelected(s.Widget().(sessionPicker).Options.Server)
		}
		return ui.EventHandled
	}
	if s.controller.RenameOpen {
		if key.EventType == ui.EventRelease {
			return ui.EventHandled
		}
		if key.MatchString("Escape") || key.MatchString("Ctrl+c") {
			s.SetState(func() { s.controller.CancelRename() })
			return ui.EventHandled
		}
		if key.EventType != vaxis.EventPaste && key.MatchString("Enter") {
			s.rename(s.controller.RenameText, s.Widget().(sessionPicker).Options.Server)
			return ui.EventHandled
		}
		if s.controller.RenamePending {
			return ui.EventHandled
		}
		return ui.EventIgnored
	}
	if key.EventType == ui.EventRelease {
		return ui.EventIgnored
	}
	if key.MatchString("Escape") || key.MatchString("Ctrl+c") {
		ctx.Quit()
		return ui.EventHandled
	}
	if key.EventType != vaxis.EventPaste && key.MatchString("Enter") {
		if sessionID, ok := s.controller.ActivatableSelection(); ok {
			s.Widget().(sessionPicker).Result.selectSession(sessionID)
			ctx.Quit()
		}
		return ui.EventHandled
	}
	var handled bool
	s.SetState(func() { handled = s.controller.HandleKey(key) })
	if handled {
		return ui.EventHandled
	}
	return ui.EventIgnored
}

func (s *sessionPickerState) rename(value string, server sessionclient.Server) {
	s.SetState(func() { s.controller.SetRenameText(value) })
	var generation uint64
	var sessionID, name string
	var started bool
	s.SetState(func() {
		generation, sessionID, name, started = s.controller.BeginRenameSave()
	})
	if !started {
		return
	}
	runtime := s.Context().Runtime()
	go func() {
		renamed, err := server.RenameSession(s.ctx, sessionID, name)
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			s.SetState(func() { s.controller.ResolveRename(generation, renamed, err) })
		})
	}()
}

func (s *sessionPickerState) deleteSelected(server sessionclient.Server) {
	var generation uint64
	var sessionID string
	var started bool
	s.SetState(func() {
		generation, sessionID, started = s.controller.BeginDeleteConfirm()
	})
	if !started {
		return
	}
	runtime := s.Context().Runtime()
	go func() {
		err := server.DeleteSession(s.ctx, sessionID)
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			s.SetState(func() { s.controller.ResolveDelete(generation, err) })
		})
	}()
}
