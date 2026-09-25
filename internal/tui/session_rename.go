package tui

import (
	"errors"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

type sessionRenameCallbacks struct {
	Changed   ui.TextChangedCallback
	Submitted ui.TextChangedCallback
}

type sessionRenameSnapshot struct {
	Open         bool
	Target       string
	Text         string
	CursorOffset int
	CursorEnd    uint64
	Pending      bool
	Error        string
}

type currentSessionRenameController struct {
	Open          bool
	SessionID     string
	Target        string
	Text          string
	CursorEnd     uint64
	Pending       bool
	Error         string
	generation    uint64
	captureEditor bool
	captureBuffer ui.TextBuffer
}

func (c *currentSessionRenameController) Begin(session protocol.SessionInfo) bool {
	if c.Open || c.Pending || session.ID == "" {
		return false
	}
	c.generation++
	c.Open = true
	c.SessionID = session.ID
	c.Target = sessionDisplayName(session)
	c.Text = session.Name
	c.CursorEnd++
	c.Pending = false
	c.Error = ""
	c.armEditorCapture()
	return true
}

func (c *currentSessionRenameController) SetText(value string) {
	if c.Open && !c.Pending {
		c.Text = value
		c.Error = ""
		if c.captureEditor {
			c.captureBuffer = ui.NewTextBuffer(value)
			c.captureBuffer.SetCursorOffset(c.captureBuffer.Len())
		}
	}
}

func (c *currentSessionRenameController) HandleEditorKey(key ui.Key) bool {
	if !c.Open || c.Pending || !c.captureEditor || key.EventType == ui.EventRelease {
		return false
	}
	buffer := &c.captureBuffer
	if key.EventType == vaxis.EventPaste {
		buffer.InsertSingleLine(palettePasteText(key))
		c.Text = buffer.Text()
		c.Error = ""
		c.CursorEnd++
		return true
	}
	switch {
	case key.MatchString("Ctrl+a"):
		buffer.SelectAll()
	case key.MatchString("Ctrl+Shift+Left"):
		buffer.ExtendWordLeft()
	case key.MatchString("Ctrl+Shift+Right"):
		buffer.ExtendWordRight()
	case key.MatchString("Ctrl+Left"):
		buffer.MoveWordLeft()
	case key.MatchString("Ctrl+Right"):
		buffer.MoveWordRight()
	case key.MatchString("Shift+Left"):
		buffer.ExtendLeft()
	case key.MatchString("Shift+Right"):
		buffer.ExtendRight()
	case key.MatchString("Shift+Home"):
		buffer.ExtendHome()
	case key.MatchString("Shift+End"):
		buffer.ExtendEnd()
	case key.Keycode == vaxis.KeyLeft:
		buffer.MoveLeft()
	case key.Keycode == vaxis.KeyRight:
		buffer.MoveRight()
	case key.Keycode == vaxis.KeyHome:
		buffer.MoveHome()
	case key.Keycode == vaxis.KeyEnd:
		buffer.MoveEnd()
	case key.Keycode == vaxis.KeyBackspace:
		if key.MatchString("Ctrl+Backspace") {
			buffer.DeleteWordBackward()
		} else {
			buffer.DeleteBackward()
		}
	case key.Keycode == vaxis.KeyDelete:
		if key.MatchString("Ctrl+Delete") {
			buffer.DeleteWordForward()
		} else {
			buffer.DeleteForward()
		}
	case key.Text != "":
		buffer.InsertSingleLine(key.Text)
	}
	c.Text = buffer.Text()
	c.Error = ""
	c.CursorEnd++
	return true
}

func (c *currentSessionRenameController) TickFrame() {
	c.captureEditor = false
}

func (c *currentSessionRenameController) armEditorCapture() {
	c.captureEditor = true
	c.captureBuffer = ui.NewTextBuffer(c.Text)
	c.captureBuffer.SetCursorOffset(c.captureBuffer.Len())
}

func (c *currentSessionRenameController) BeginSave() (uint64, string, string, bool) {
	if !c.Open || c.Pending || c.SessionID == "" {
		return 0, "", "", false
	}
	name := strings.TrimSpace(c.Text)
	if name == "" {
		c.Cancel()
		return 0, "", "", false
	}
	c.generation++
	c.Pending = true
	c.Error = ""
	return c.generation, c.SessionID, name, true
}

func (c *currentSessionRenameController) Resolve(generation uint64, session protocol.SessionInfo, renameErr error) bool {
	if !c.Open || !c.Pending || generation != c.generation {
		return false
	}
	c.Pending = false
	if renameErr == nil && session.ID != c.SessionID {
		renameErr = errors.New("renamed session identity mismatch")
	}
	if renameErr == nil && session.Name != strings.TrimSpace(c.Text) {
		renameErr = errors.New("renamed session name mismatch")
	}
	if renameErr != nil {
		c.Error = renameErr.Error()
		c.CursorEnd++
		c.armEditorCapture()
		return true
	}
	c.close()
	return true
}

func (c *currentSessionRenameController) Cancel() {
	if !c.Open || c.Pending {
		return
	}
	c.close()
}

func (c *currentSessionRenameController) Reset() {
	c.close()
}

func (c *currentSessionRenameController) close() {
	generation := c.generation + 1
	*c = currentSessionRenameController{generation: generation}
}

func (c *currentSessionRenameController) Snapshot() sessionRenameSnapshot {
	return sessionRenameSnapshot{
		Open: c.Open, Target: c.Target, Text: c.Text,
		CursorOffset: c.captureBuffer.CursorOffset(), CursorEnd: c.CursorEnd,
		Pending: c.Pending, Error: c.Error,
	}
}

func sessionExplorerRenameSnapshot(snapshot sessionExplorerSnapshot) sessionRenameSnapshot {
	target := shortSessionID(snapshot.RenameSessionID)
	for _, session := range snapshot.Sessions {
		if session.ID == snapshot.RenameSessionID {
			target = sessionExplorerItemLabel(session)
			break
		}
	}
	return sessionRenameSnapshot{
		Open: snapshot.RenameOpen, Target: target, Text: snapshot.RenameText,
		CursorOffset: len((ui.LayoutContext{}).Characters(snapshot.RenameText)),
		CursorEnd:    snapshot.RenameCursorEnd, Pending: snapshot.RenamePending, Error: snapshot.RenameError,
	}
}

type sessionRenameSurface struct {
	Snapshot  sessionRenameSnapshot
	Callbacks sessionRenameCallbacks
}

func (w sessionRenameSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	fieldTheme := theme
	fieldTheme.Surface = theme.Background
	fieldTheme.SurfaceHovered = theme.Background
	cursorOffset := w.Snapshot.CursorOffset
	field := ui.Widget(textInput(fieldTheme, textInputConfig{
		Value: w.Snapshot.Text, Placeholder: "Enter new session name…",
		OnChanged: w.Callbacks.Changed, OnSubmitted: w.Callbacks.Submitted,
		InitialCursorOffset: &cursorOffset, InitialCursorGeneration: w.Snapshot.CursorEnd,
		AutoFocus: true,
	}))
	if w.Snapshot.Pending {
		field = ui.Expanded(ui.Text{
			Value: w.Snapshot.Text, Style: ui.Style{Foreground: theme.MutedForeground},
			Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		})
	}
	children := []ui.Widget{
		ui.Text{Value: w.Snapshot.Target, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		ui.SizedBox{Height: 1},
	}
	if w.Snapshot.Error != "" {
		children = append(children,
			ui.Text{Value: "Rename failed: " + w.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
			ui.SizedBox{Height: 1},
		)
	}
	children = append(children, ui.DecoratedBox(
		ui.Decoration{
			Style:  ui.Style{Background: theme.Background},
			Border: ui.BorderAll(ui.Style{Foreground: theme.PrimaryText, Background: theme.Background}),
		},
		ui.Padding(ui.Insets{Top: 1, Right: 1, Bottom: 1, Left: 1}, ui.Flex{
			Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, Children: []ui.Widget{field},
		}),
	))
	body := ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch,
		Children: children,
	}
	var footer ui.Widget = ui.Text{
		Value: "enter save · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground},
		Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
	}
	if w.Snapshot.Pending {
		footer = spinnerWithLabel("Saving…", ui.Style{Foreground: theme.MutedForeground})
	}
	return dialogSurface(theme, "Rename session", "", body, footer, true)
}
