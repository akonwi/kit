package scratchpad

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateContentAcceptsCanonicalMultilineMarkdown(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"", "# Notes\n\n- one\n\tcontinued ✨\n", strings.Repeat("x", MaxContentBytes)} {
		if err := ValidateContent(content); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateContentRejectsUnsafeOrOversizedText(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"invalid utf8":    string([]byte{0xff}),
		"carriage return": "one\r\ntwo",
		"nul":             "one\x00two",
		"escape":          "one\x1btwo",
		"bidi override":   "one\u202etwo",
		"oversized":       strings.Repeat("x", MaxContentBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateContent(content); err == nil {
				t.Fatal("content was valid")
			}
		})
	}
}

func TestApplyExactEditsInitializesAndAppliesSimultaneousReplacements(t *testing.T) {
	t.Parallel()
	initialized, applied, err := ApplyExactEdits("", []Edit{{OldText: "", NewText: "# Notes\n"}})
	if err != nil || initialized != "# Notes\n" || applied != 1 {
		t.Fatalf("initialized edit = %q, %d, %v", initialized, applied, err)
	}
	updated, applied, err := ApplyExactEdits("alpha beta gamma", []Edit{
		{OldText: "alpha", NewText: "A"},
		{OldText: "gamma", NewText: "G"},
	})
	if err != nil || updated != "A beta G" || applied != 2 {
		t.Fatalf("simultaneous edits = %q, %d, %v", updated, applied, err)
	}
}

func TestApplyExactEditsRejectsMissingDuplicateAndOverlappingMatches(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		original string
		edits    []Edit
	}{
		{"abcdef", []Edit{{OldText: "missing", NewText: "x"}}},
		{"aaaa", []Edit{{OldText: "aa", NewText: "x"}}},
		{"abcdef", []Edit{{OldText: "abc", NewText: "x"}, {OldText: "bcd", NewText: "y"}}},
		{"content", []Edit{{OldText: "", NewText: "not initialization"}}},
	} {
		if updated, applied, err := ApplyExactEdits(test.original, test.edits); !errors.Is(err, ErrInvalidEdit) || updated != test.original || applied != 0 {
			t.Fatalf("invalid edits = %q, %d, %v", updated, applied, err)
		}
	}
}

func TestExactEditInputsAndDiagnosticsAreBounded(t *testing.T) {
	t.Parallel()
	if err := ValidateEdits([]Edit{{OldText: "x", NewText: strings.Repeat("y", MaxEditFieldBytes+1)}}); !errors.Is(err, ErrInvalidEdit) {
		t.Fatalf("oversized edit error = %v", err)
	}
	edits := make([]Edit, MaxEditCount)
	for index := range edits {
		edits[index] = Edit{OldText: "abc", NewText: "x"}
	}
	_, _, err := ApplyExactEdits("abcdef", edits)
	var editErr *EditError
	if !errors.As(err, &editErr) || len(editErr.Problems) != maxEditProblems+1 || editErr.Problems[len(editErr.Problems)-1] != "additional errors omitted" {
		t.Fatalf("bounded diagnostics = %#v, %v", editErr, err)
	}
}

func TestScratchpadErrorsPreserveTypedIdentity(t *testing.T) {
	t.Parallel()
	current := Record{OwnerSessionID: "session_owner", Revision: 2}
	conflict := &ConflictError{Expected: 1, Current: current}
	if !errors.Is(conflict, ErrConflict) || conflict.Current != current {
		t.Fatalf("conflict = %+v", conflict)
	}
	if !errors.Is(ValidateContent(strings.Repeat("x", MaxContentBytes+1)), ErrContentTooLarge) {
		t.Fatal("oversized content did not return ErrContentTooLarge")
	}
	if !errors.Is(ValidateContent("bad\x00content"), ErrInvalidContent) {
		t.Fatal("unsafe content did not return ErrInvalidContent")
	}
	if !errors.Is(ValidateRevision(0), ErrInvalidRevision) {
		t.Fatal("zero revision did not return ErrInvalidRevision")
	}
}
