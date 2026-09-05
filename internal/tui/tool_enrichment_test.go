package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDetectActivityEnrichment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		call   transcriptToolCall
		state  transcriptMessage
		exists bool
		kind   activityEnrichmentKind
		ok     bool
	}{
		{
			name: "read result", exists: true, ok: true, kind: activityEnrichmentFile,
			call:  transcriptToolCall{Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)},
			state: transcriptMessage{Text: "package main", ToolStatus: "Completed"},
		},
		{
			name: "truncated read result", exists: true, ok: true, kind: activityEnrichmentFile,
			call: transcriptToolCall{Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)},
			state: transcriptMessage{
				Text: "package main\n[truncated]", ToolStatus: "Completed",
				ToolDetails: json.RawMessage(`{"path":"main.go","lines":42,"truncated":true}`),
			},
		},
		{
			name: "write arguments", exists: true, ok: true, kind: activityEnrichmentFile,
			call:  transcriptToolCall{Name: "write", Arguments: json.RawMessage(`{"path":"main.go","content":"package main"}`)},
			state: transcriptMessage{Text: "Wrote 1 line", ToolStatus: "Completed"},
		},
		{
			name: "canonical edit", exists: true, ok: true, kind: activityEnrichmentEdits,
			call:  transcriptToolCall{Name: "edit", Arguments: json.RawMessage(`{"path":"main.go","edits":[{"oldText":"old","newText":"new"}]}`)},
			state: transcriptMessage{ToolStatus: "Completed"},
		},
		{
			name: "legacy edit", exists: true, ok: true, kind: activityEnrichmentEdits,
			call:  transcriptToolCall{Name: "edit", Arguments: json.RawMessage(`{"path":"main.go","oldText":"old","newText":"new"}`)},
			state: transcriptMessage{ToolStatus: "Completed"},
		},
		{name: "error", exists: true, call: transcriptToolCall{Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)}, state: transcriptMessage{Text: "failure", IsError: true}},
		{name: "pending", exists: true, call: transcriptToolCall{Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)}, state: transcriptMessage{Text: "partial", Pending: true}},
		{name: "truncated arguments", exists: true, call: transcriptToolCall{Name: "write", ArgumentsTruncated: true}, state: transcriptMessage{ToolStatus: "Completed"}},
		{name: "malformed edits", exists: true, call: transcriptToolCall{Name: "edit", Arguments: json.RawMessage(`{"path":"main.go","edits":[{"oldText":1}]}`)}, state: transcriptMessage{ToolStatus: "Completed"}},
		{name: "unknown tool", exists: true, call: transcriptToolCall{Name: "grep", Arguments: json.RawMessage(`{"path":"main.go"}`)}, state: transcriptMessage{Text: "match", ToolStatus: "Completed"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail, ok := detectActivityEnrichment(test.call, test.state, test.exists)
			if ok != test.ok || ok && detail.Kind != test.kind {
				t.Fatalf("detectActivityEnrichment() = %+v, %v", detail, ok)
			}
			if test.name == "write arguments" && detail.Content != "package main" {
				t.Fatalf("write content = %q", detail.Content)
			}
			if test.name == "truncated read result" && (detail.Content != "package main" || detail.Notice != "[truncated]" || detail.LineCount != 42) {
				t.Fatalf("truncated read detail = %+v", detail)
			}
			if test.name == "legacy edit" && len(detail.Edits) != 1 {
				t.Fatalf("legacy edits = %+v", detail.Edits)
			}
		})
	}
}

func TestBuildActivityDiffPreservesLineKinds(t *testing.T) {
	t.Parallel()

	lines, ok := buildActivityDiff("same\nold\ntail\n", "same\nnew\ntail\n")
	if !ok {
		t.Fatal("bounded diff was rejected")
	}
	want := []activityDiffLine{
		{Kind: activityDiffContext, Text: "same"},
		{Kind: activityDiffDelete, Text: "old"},
		{Kind: activityDiffAdd, Text: "new"},
		{Kind: activityDiffContext, Text: "tail"},
	}
	if len(lines) != len(want) {
		t.Fatalf("diff lines = %+v", lines)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Errorf("diff line %d = %+v, want %+v", index, lines[index], want[index])
		}
	}
}

func TestActivityEnrichedPresentationUsesBoundedCodeAndDiffMetadata(t *testing.T) {
	t.Parallel()

	fileLines := make([]string, maxEnrichedPresentationLines+5)
	for index := range fileLines {
		fileLines[index] = "line"
	}
	_, measurement, metadata, ok := activityEnrichedPresentation(activityEnrichment{
		Kind: activityEnrichmentFile, Path: "main.go", Content: strings.Join(fileLines, "\n"),
	})
	if !ok || metadata != "2005 lines" || len(strings.Split(measurement, "\n")) != maxEnrichedPresentationLines+1 || !strings.HasSuffix(measurement, "… 5 more lines") {
		t.Fatalf("bounded code presentation = ok %v metadata %q rows %d", ok, metadata, len(strings.Split(measurement, "\n")))
	}

	_, diffText, metadata, ok := activityEnrichedPresentation(activityEnrichment{
		Kind:  activityEnrichmentEdits,
		Edits: []activityEdit{{OldText: "old", NewText: "new"}, {OldText: "gone", NewText: "added"}},
	})
	if !ok || metadata != "2 edits" || diffText != "- old\n+ new\n  "+glyphEllipsis+"\n- gone\n+ added" {
		t.Fatalf("diff presentation = ok %v metadata %q text %q", ok, metadata, diffText)
	}
}

func TestActivityEnrichedPresentationRejectsOversizedDiff(t *testing.T) {
	t.Parallel()

	large := strings.Repeat("line\n", maxActivityDiffInputLines+1)
	if _, _, _, ok := activityEnrichedPresentation(activityEnrichment{
		Kind: activityEnrichmentEdits, Edits: []activityEdit{{OldText: large, NewText: "new"}},
	}); ok {
		t.Fatal("oversized diff presentation was accepted")
	}
}

func TestBuildActivityDiffHandlesEmptyAndBoundsLargeInputs(t *testing.T) {
	t.Parallel()

	added, ok := buildActivityDiff("", "界\n")
	if !ok || len(added) != 1 || added[0] != (activityDiffLine{Kind: activityDiffAdd, Text: "界"}) {
		t.Fatalf("added diff = %+v, %v", added, ok)
	}
	deleted, ok := buildActivityDiff("old", "")
	if !ok || len(deleted) != 1 || deleted[0].Kind != activityDiffDelete {
		t.Fatalf("deleted diff = %+v, %v", deleted, ok)
	}
	newline, ok := buildActivityDiff("same", "same\n")
	if !ok || len(newline) != 3 || newline[1].Text != "\\ line 1 ending: none" ||
		newline[2].Text != "\\ line 1 ending: LF" {
		t.Fatalf("newline-only diff = %+v, %v", newline, ok)
	}
	mixed, ok := buildActivityDiff("a\r\nb\n", "a\nb\r\n")
	if !ok || len(mixed) != 4 || mixed[2].Text != "\\ line 1 ending: CRLF" || mixed[3].Text != "\\ line 1 ending: LF" {
		t.Fatalf("mixed-ending diff = %+v, %v", mixed, ok)
	}
	large := strings.Repeat("line\n", maxActivityDiffInputLines+1)
	if _, ok := buildActivityDiff(large, "replacement"); ok {
		t.Fatal("oversized quadratic diff was accepted")
	}
}
