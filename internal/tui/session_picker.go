package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

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

func (s *sessionPickerState) Build(ctx ui.BuildContext) ui.Widget {
	widget := s.Widget().(sessionPicker)
	snapshot := s.controller.Snapshot()
	height := sessionPickerHeight(snapshot)
	if widget.Result != nil {
		widget.Result.setRegionHeight(height)
	}
	surface := sessionExplorerSurface{
		Snapshot: snapshot, Action: "open",
		Callbacks: sessionExplorerCallbacks{
			Toggle: func(_ ui.EventContext, sessionID string) {
				s.SetState(func() { s.controller.ToggleExpanded(sessionID) })
			},
			Activate: func(ctx ui.EventContext, sessionID string) {
				s.SetState(func() { s.controller.Select(sessionID) })
				if s.controller.Selection == sessionID {
					s.open(ctx)
				}
			},
			RenameChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.controller.SetRenameText(value) })
			},
			RenameSubmitted: func(_ ui.EventContext, value string) {
				s.rename(value, widget.Options.Server)
			},
		},
	}
	// The canonical picker is centered at its standard width in this region.
	theme := ui.MustDepend[ui.Theme](ctx)
	return ui.FocusScope{
		AutoFocus: true, Trap: true,
		Child: ui.Focus(&s.focus, ui.DecoratedBox(
			ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}},
			ui.SizedBox{Height: height, Child: surface},
		)),
	}
}

// open returns the highlighted session to the caller and ends the picker.
func (s *sessionPickerState) open(ctx ui.EventContext) {
	if sessionID, ok := s.controller.ActivatableSelection(); ok {
		s.Widget().(sessionPicker).Result.selectSession(sessionID)
		ctx.Quit()
	}
}

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

// sessionPickerChromeRows are the canonical picker rows around its list: the
// borders, title and its spacer, search field, dividers, and footer.
const sessionPickerChromeRows = 9

func sessionPickerHeight(snapshot sessionExplorerSnapshot) int {
	rows := len(snapshot.Sessions) + sessionPickerChromeRows
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
	if key.EventType != vaxis.EventPaste && key.MatchString("Ctrl+c") {
		if key.EventType != ui.EventRelease {
			ctx.Quit()
		}
		return ui.EventHandled
	}
	if s.controller.DeleteOpen {
		if key.EventType == ui.EventRelease {
			return ui.EventHandled
		}
		if key.EventType != vaxis.EventPaste && key.MatchString("Escape") {
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
		if key.EventType == vaxis.EventPaste {
			if !s.controller.RenamePending {
				ctx.Invoke(ui.InsertTextIntent{Text: pastedKeyText(key)})
			}
			return ui.EventHandled
		}
		if key.MatchString("Escape") {
			s.SetState(func() { s.controller.CancelRename() })
			return ui.EventHandled
		}
		if key.MatchString("Enter") {
			s.rename(s.controller.RenameText, s.Widget().(sessionPicker).Options.Server)
			return ui.EventHandled
		}
		if s.controller.RenamePending {
			return ui.EventHandled
		}
		return ui.EventIgnored
	}
	if key.EventType != ui.EventRelease && key.EventType != vaxis.EventPaste && key.MatchString("Escape") {
		ctx.Quit()
		return ui.EventHandled
	}
	var result pickerKeyResult
	s.SetState(func() { result = s.controller.HandleKey(key) })
	if !result.Handled {
		return ui.EventIgnored
	}
	if result.Activate {
		s.open(ctx)
	}
	return ui.EventHandled
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
