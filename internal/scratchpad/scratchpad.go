// Package scratchpad owns shared root-session scratchpad records and mutation semantics.
package scratchpad

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxContentBytes   = 64 << 10
	MaxEditCount      = 64
	MaxEditFieldBytes = 64 << 10
	MaxEditInputBytes = 128 << 10
	maxEditProblems   = 16
)

var (
	ErrNotFound          = errors.New("scratchpad not found")
	ErrInvalidContent    = errors.New("scratchpad content is invalid")
	ErrContentTooLarge   = errors.New("scratchpad content is too large")
	ErrInvalidRevision   = errors.New("scratchpad revision is invalid")
	ErrConflict          = errors.New("scratchpad revision conflict")
	ErrRevisionExhausted = errors.New("scratchpad revision exhausted")
	ErrMigrationRequired = errors.New("scratchpad migration required")
	ErrUnsupported       = errors.New("scratchpad unsupported")
	ErrUnavailable       = errors.New("scratchpad unavailable")
	ErrInvalidEdit       = errors.New("scratchpad edit is invalid")
)

// Record is one authoritative root-session-family scratchpad.
type Record struct {
	OwnerSessionID string
	Content        string
	Revision       int64
	UpdatedAt      time.Time
}

// ConflictError reports a rejected stale update and the authoritative record.
// Edit is one exact replacement matched against the original content.
type Edit struct {
	OldText string
	NewText string
}

// EditError reports exact replacement validation failures.
type EditError struct {
	Problems []string
}

func (err *EditError) Error() string {
	if err == nil || len(err.Problems) == 0 {
		return ErrInvalidEdit.Error()
	}
	return fmt.Sprintf("%v: %s", ErrInvalidEdit, strings.Join(err.Problems, "; "))
}

func (*EditError) Unwrap() error { return ErrInvalidEdit }

// ConflictError reports a rejected stale update and the authoritative record.
type ConflictError struct {
	Expected int64
	Current  Record
}

func (err *ConflictError) Error() string {
	return fmt.Sprintf("%v: expected %d, actual %d", ErrConflict, err.Expected, err.Current.Revision)
}

func (*ConflictError) Unwrap() error { return ErrConflict }

// Repository persists shared scratchpads resolved from bound session identities.
type Repository interface {
	Get(context.Context, string) (Record, error)
	Update(context.Context, string, int64, string) (Record, error)
	Edit(context.Context, string, []Edit) (Record, int, bool, error)
}

// ValidateEdits bounds exact replacement input before transactional work.
func ValidateEdits(edits []Edit) error {
	if len(edits) == 0 || len(edits) > MaxEditCount {
		return &EditError{Problems: []string{fmt.Sprintf("edits must contain between 1 and %d replacements", MaxEditCount)}}
	}
	total := 0
	for index, edit := range edits {
		if len(edit.OldText) > MaxEditFieldBytes || len(edit.NewText) > MaxEditFieldBytes {
			return &EditError{Problems: []string{fmt.Sprintf("edits[%d]: oldText and newText must each be at most %d bytes", index, MaxEditFieldBytes)}}
		}
		total += len(edit.OldText) + len(edit.NewText)
		if total > MaxEditInputBytes {
			return &EditError{Problems: []string{fmt.Sprintf("combined edit input exceeds %d bytes", MaxEditInputBytes)}}
		}
	}
	return nil
}

// ApplyExactEdits validates and simultaneously applies exact replacements.
func ApplyExactEdits(original string, edits []Edit) (string, int, error) {
	if err := ValidateEdits(edits); err != nil {
		return original, 0, err
	}
	type editRange struct {
		Edit
		index int
		start int
		end   int
	}
	problems := make([]string, 0, maxEditProblems+1)
	problemsOmitted := false
	addProblem := func(problem string) {
		if len(problems) < maxEditProblems {
			problems = append(problems, problem)
		} else {
			problemsOmitted = true
		}
	}
	ranges := make([]editRange, 0, len(edits))
	for index, edit := range edits {
		if edit.OldText == "" {
			if original == "" && len(edits) == 1 {
				ranges = append(ranges, editRange{Edit: edit, index: index})
				continue
			}
			addProblem(fmt.Sprintf("edits[%d]: oldText must not be empty unless initializing an empty scratchpad", index))
			continue
		}
		offsets := matchOffsets(original, edit.OldText)
		switch len(offsets) {
		case 0:
			addProblem(fmt.Sprintf("edits[%d]: oldText not found in scratchpad", index))
		case 1:
			ranges = append(ranges, editRange{Edit: edit, index: index, start: offsets[0], end: offsets[0] + len(edit.OldText)})
		default:
			addProblem(fmt.Sprintf("edits[%d]: oldText matches %d locations — must be unique", index, len(offsets)))
		}
	}
	for left := 0; left < len(ranges); left++ {
		for right := left + 1; right < len(ranges); right++ {
			if ranges[left].start < ranges[right].end && ranges[right].start < ranges[left].end {
				addProblem(fmt.Sprintf("edits[%d] and edits[%d] overlap — merge them into one edit", ranges[left].index, ranges[right].index))
			}
		}
	}
	if problemsOmitted {
		problems = append(problems, "additional errors omitted")
	}
	if len(problems) > 0 {
		return original, 0, &EditError{Problems: problems}
	}
	sort.Slice(ranges, func(left, right int) bool { return ranges[left].start > ranges[right].start })
	updated := original
	for _, replacement := range ranges {
		updated = updated[:replacement.start] + replacement.NewText + updated[replacement.end:]
	}
	if err := ValidateContent(updated); err != nil {
		return original, 0, err
	}
	return updated, len(edits), nil
}

func matchOffsets(content, search string) []int {
	var offsets []int
	for from := 0; from <= len(content)-len(search); {
		index := strings.Index(content[from:], search)
		if index < 0 {
			break
		}
		index += from
		offsets = append(offsets, index)
		from = index + 1
	}
	return offsets
}

// ValidateContent validates canonical multiline scratchpad Markdown.
func ValidateContent(content string) error {
	if !utf8.ValidString(content) {
		return fmt.Errorf("%w: content must be valid UTF-8", ErrInvalidContent)
	}
	if len(content) > MaxContentBytes {
		return fmt.Errorf("%w: content exceeds %d bytes", ErrContentTooLarge, MaxContentBytes)
	}
	for _, r := range content {
		if r == '\n' || r == '\t' {
			continue
		}
		if r == '\r' {
			return fmt.Errorf("%w: content must use LF line endings", ErrInvalidContent)
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return fmt.Errorf("%w: content contains an unsupported control or format character", ErrInvalidContent)
		}
	}
	return nil
}

// ValidateRevision checks the persisted and protocol revision domain.
func ValidateRevision(revision int64) error {
	if revision < 1 {
		return fmt.Errorf("%w: revision must be positive", ErrInvalidRevision)
	}
	return nil
}
