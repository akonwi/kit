package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestMessageHistoryEntriesKeepNewestUniqueUserText(t *testing.T) {
	t.Parallel()

	entries := messageHistoryEntries([]protocol.TranscriptMessage{
		{ID: "message_new", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "  newer prompt  "}}},
		{ID: "message_dup", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "newer prompt"}}},
		{ID: "message_blank", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "   "}}},
		{ID: "message_old", Role: "assistant", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "not recalled"}}},
		{ID: "message_older", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "older prompt"}}},
	})
	if len(entries) != 2 || entries[0].Text != "newer prompt" || entries[1].ID != "message_older" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestMessageHistorySelectionFillsComposerWithoutSubmitting(t *testing.T) {
	t.Parallel()

	var history messageHistoryController
	if history.OpenFor(nil) {
		t.Fatal("OpenFor() opened an empty history")
	}
	if !history.OpenFor([]messageHistoryEntry{{ID: "message_1", Text: "first"}, {ID: "message_2", Text: "second"}}) {
		t.Fatal("OpenFor() did not open")
	}
	history.Move(1)
	selected, ok := history.Selected()
	if !ok || selected.Text != "second" {
		t.Fatalf("selected = %+v, %t", selected, ok)
	}
	composer := ""
	composer = selected.Text
	history.Close()
	if composer != "second" || history.Open {
		t.Fatalf("composer = %q open = %t", composer, history.Open)
	}
}
