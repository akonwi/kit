package tui

import (
	"strings"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

// pasteCoalescer accumulates the individual key events a terminal emits
// between bracketed paste markers so the application can apply one paste as a
// single change. Vaxis reports each pasted character as its own key event,
// which otherwise repaints the composer once per character.
type pasteCoalescer struct {
	active bool
	text   strings.Builder
}

// Begin starts buffering a bracketed paste.
func (c *pasteCoalescer) Begin() {
	c.active = true
	c.text.Reset()
}

// Active reports whether a bracketed paste is currently buffering.
func (c *pasteCoalescer) Active() bool { return c.active }

// Append buffers key when it belongs to the in-progress paste. It reports
// whether the key was consumed and must not be dispatched on its own.
func (c *pasteCoalescer) Append(key ui.Key) bool {
	if !c.active || key.EventType != vaxis.EventPaste {
		return false
	}
	c.text.WriteString(pastedKeyText(key))
	return true
}

// Flush ends the in-progress paste and returns a single synthetic key holding
// the accumulated text. It reports false when no paste was buffering or the
// paste carried no insertable text.
func (c *pasteCoalescer) Flush() (ui.Key, bool) {
	if !c.active {
		return ui.Key{}, false
	}
	text := c.text.String()
	c.active = false
	c.text.Reset()
	if text == "" {
		return ui.Key{}, false
	}
	return ui.Key{Text: text, EventType: vaxis.EventPaste}, true
}

// Observe applies bracketed paste framing from a root capture handler. Keys
// belonging to an in-progress paste are buffered and consumed; the completed
// paste is offered to deliver as one synthetic key and, if unclaimed, inserted
// into the focused text editor as a single edit. It reports the event result
// and whether the event was fully consumed.
func (c *pasteCoalescer) Observe(ctx ui.EventContext, event ui.Event, deliver func(ui.EventContext, ui.Key) ui.EventResult) (ui.EventResult, bool) {
	switch event.(type) {
	case vaxis.PasteStartEvent:
		c.Begin()
		return ui.EventIgnored, true
	case vaxis.PasteEndEvent:
		c.apply(ctx, deliver)
		return ui.EventIgnored, true
	}
	key, ok := event.(ui.Key)
	if !ok {
		// A terminal that never delivers the closing marker must not strand
		// buffered text, so any other event completes the paste.
		c.apply(ctx, deliver)
		return ui.EventIgnored, true
	}
	if c.Append(key) {
		return ui.EventHandled, true
	}
	c.apply(ctx, deliver)
	return ui.EventIgnored, false
}

func (c *pasteCoalescer) apply(ctx ui.EventContext, deliver func(ui.EventContext, ui.Key) ui.EventResult) {
	key, ok := c.Flush()
	if !ok {
		return
	}
	if deliver != nil && deliver(ctx, key) == ui.EventHandled {
		return
	}
	insertPastedText(ctx, key.Text)
}

// pastedKeyText returns the literal text a pasted key contributes. Control
// keys inside a paste are data rather than commands, so newlines and tabs are
// preserved and every other non-textual key contributes nothing.
func pastedKeyText(key ui.Key) string {
	if key.Text != "" {
		return key.Text
	}
	if key.Modifiers&vaxis.ModCtrl != 0 {
		switch key.Keycode {
		case 'i':
			return "\t"
		case 'j', 'm':
			return "\n"
		}
		return ""
	}
	switch key.Keycode {
	case vaxis.KeyEnter, '\n':
		return "\n"
	case vaxis.KeyTab:
		return "\t"
	}
	return ""
}

// insertPasteIntent marks provenance only at actual delivery, never at framing.
type insertPasteIntent struct{ Text string }

func (insertPasteIntent) IntentType() ui.IntentType { return "kit.insert-paste" }
func insertPastedText(ctx ui.EventContext, text string) {
	if text != "" && ctx.Invoke(insertPasteIntent{Text: text}) != ui.EventHandled {
		ctx.Invoke(ui.InsertTextIntent{Text: text})
	}
}
