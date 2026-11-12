package contract

import (
	"errors"
	"strings"

	"github.com/akonwi/kit/internal/identifier"
)

// ViewIDPrefix starts every view identifier.
const ViewIDPrefix = "view_"

// MaxViewRevealLine bounds the line a reveal names.
const MaxViewRevealLine = 1<<31 - 1

// ValidViewID reports whether id is a client-generated view identifier:
// "view_" and 32 lowercase hex digits (ADR 0068). It addresses one view
// client of a session; it grants nothing.
func ValidViewID(id string) bool {
	return identifier.Valid(id, ViewIDPrefix) && strings.ToLower(id) == id
}

// ViewRevealInput asks the view clients that watch one view identifier to
// show a line or an annotation. Exactly one of Line and Annotation is set.
type ViewRevealInput struct {
	// Line is the 1-based line a File view shows, or 0.
	Line int `json:"line,omitempty"`
	// Annotation is the annotation a Diff or File view reveals, or 0.
	Annotation uint64 `json:"annotation,omitempty"`
}

// Validate checks that the request names exactly one bounded target.
func (i ViewRevealInput) Validate() error {
	switch {
	case i.Line < 0 || i.Line > MaxViewRevealLine:
		return errors.New("view reveal line is out of range")
	case (i.Line == 0) == (i.Annotation == 0):
		return errors.New("view reveal names exactly one of line and annotation")
	}
	return nil
}

// ViewRevealResult reports whether a view client watching the identifier
// was sent the reveal. It confirms the reveal was sent, not that it was shown.
type ViewRevealResult struct {
	Delivered bool `json:"delivered"`
}

// Validate accepts every result.
func (ViewRevealResult) Validate() error { return nil }

// ViewReveal is one live-only request, sent to the view clients watching
// ViewID in SessionID, to show a line or an annotation. It is never stored or
// replayed.
type ViewReveal struct {
	SessionID  string `json:"sessionId"`
	ViewID     string `json:"viewId"`
	Line       int    `json:"line,omitempty"`
	Annotation uint64 `json:"annotation,omitempty"`
}

// Validate checks the reveal's identity and target.
func (r ViewReveal) Validate() error {
	if r.SessionID == "" || !ValidViewID(r.ViewID) {
		return errors.New("view reveal has an invalid identity")
	}
	return ViewRevealInput{Line: r.Line, Annotation: r.Annotation}.Validate()
}
