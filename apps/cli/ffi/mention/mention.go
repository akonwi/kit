// Package mention tracks an inline mention, such as `@path`, while the
// composer is edited. Offsets are UTF-8 byte offsets into the draft, as the
// composer's cursor is. It follows the vaxis client's mention controller.
package mention

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// State is an open or closed mention. Anchor is the trigger's offset and
// QueryEnd the end of the query typed after it.
type State struct {
	Open     bool
	Anchor   int
	QueryEnd int
	Query    string
}

// Observed is the mention after an edit, and whether that edit opened it.
type Observed struct {
	State  State
	Opened bool
}

// Observe follows one contiguous edit from previous to next. A mention opens
// when the trigger is typed at the start of the draft or after whitespace, and
// its query follows edits at its end. A paste, an edit elsewhere, whitespace or
// another trigger in the query, or removing the trigger closes it.
func Observe(state State, previous, next string, pasted bool, trigger string) Observed {
	if len(trigger) != 1 {
		return Observed{}
	}
	mark := trigger[0]
	start, oldEnd, newEnd := ChangedRange(previous, next)
	if state.Open {
		if pasted || start < state.Anchor || oldEnd != state.QueryEnd {
			return Observed{}
		}
		state.QueryEnd += newEnd - oldEnd
		if state.QueryEnd <= state.Anchor || state.QueryEnd > len(next) || next[state.Anchor] != mark {
			return Observed{}
		}
		query := next[state.Anchor+1 : state.QueryEnd]
		if strings.IndexFunc(query, func(r rune) bool { return r == rune(mark) || unicode.IsSpace(r) }) >= 0 {
			return Observed{}
		}
		state.Query = query
		return Observed{State: state}
	}
	if pasted || newEnd-start != 1 || start >= len(next) || next[start] != mark {
		return Observed{}
	}
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(next[:start])
		if !unicode.IsSpace(r) {
			return Observed{}
		}
	}
	return Observed{State: State{Open: true, Anchor: start, QueryEnd: newEnd}, Opened: true}
}

// Inserted is a draft with a mention replaced by its token.
type Inserted struct {
	Text   string
	Cursor int
	OK     bool
}

// Insert replaces the open mention's trigger and query in text with token and
// places the cursor after it.
func Insert(state State, text, token string) Inserted {
	if !state.Open || state.Anchor < 0 || state.QueryEnd > len(text) || state.Anchor >= state.QueryEnd {
		return Inserted{Text: text}
	}
	next := text[:state.Anchor] + token + text[state.QueryEnd:]
	return Inserted{Text: next, Cursor: state.Anchor + len(token), OK: true}
}

// ChangedRange returns where previous and next differ: the shared prefix
// length and the end of the differing span in each.
func ChangedRange(previous, next string) (start, previousEnd, nextEnd int) {
	for start < len(previous) && start < len(next) && previous[start] == next[start] {
		start++
	}
	previousEnd, nextEnd = len(previous), len(next)
	for previousEnd > start && nextEnd > start && previous[previousEnd-1] == next[nextEnd-1] {
		previousEnd--
		nextEnd--
	}
	return start, previousEnd, nextEnd
}
