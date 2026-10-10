package sessionbridge

import (
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

func TestSummariesFlattenFileAndDiffAnchors(t *testing.T) {
	got := Summaries([]protocol.AnnotationSummary{
		{ID: 3, Anchor: FileAnchor("workspace_a", "main.go", "rev", 4, 6), BodyPreview: "tighten", Preview: "func main() {"},
		{ID: 7, Anchor: DiffAnchor("difftarget_a", "diffrev_a", "main.go", "diff_file_a", "old", 2, 2), BodyPreview: "why?", Stale: true, StaleReason: protocol.AnnotationStaleFile},
	})
	if len(got) != 2 {
		t.Fatalf("got %d annotations", len(got))
	}
	file := got[0]
	if file.ID != 3 || file.Kind != "file" || file.WorkspaceID != "workspace_a" || file.Path != "main.go" || file.FileRevision != "rev" || file.StartLine != 4 || file.EndLine != 6 || file.Body != "tighten" || file.Preview != "func main() {" {
		t.Fatalf("file annotation = %+v", file)
	}
	diff := got[1]
	if diff.Kind != "diff" || diff.TargetID != "difftarget_a" || diff.TargetRevision != "diffrev_a" || diff.Side != "old" || !diff.Stale || diff.StaleReason != "file_changed" {
		t.Fatalf("diff annotation = %+v", diff)
	}
}

func TestRecordBoundsTheBodyWithoutSplittingACharacter(t *testing.T) {
	body := strings.Repeat("a", protocol.MaxAnnotationSummaryTextBytes-1) + "é"
	got := Record(protocol.Annotation{ID: 1, Anchor: FileAnchor("workspace_a", "a.go", "rev", 1, 1), Body: body})
	if got.Body != strings.Repeat("a", protocol.MaxAnnotationSummaryTextBytes-1) {
		t.Fatalf("body has %d bytes", len(got.Body))
	}
}

func TestEventsProjectAnnotationChanges(t *testing.T) {
	created := protocol.Annotation{ID: 9, Anchor: FileAnchor("workspace_a", "a.go", "rev", 1, 2), Body: "note"}
	got := Events([]protocol.SessionEvent{
		{Payload: protocol.AnnotationCreatedEvent{AnnotationID: 9, Annotation: &created}},
		{Payload: protocol.AnnotationDeletedEvent{AnnotationID: 9}},
		{Payload: protocol.AnnotationSubmittedEvent{AnnotationIDs: []uint64{4, 5}, AcceptedMessageID: "message_a"}},
	})
	if got[0].Annotation == nil || got[0].Annotation.ID != 9 || got[0].Annotation.Body != "note" {
		t.Fatalf("created = %+v", got[0])
	}
	if got[1].AnnotationID != 9 {
		t.Fatalf("deleted = %+v", got[1])
	}
	if len(got[2].AnnotationIDs) != 2 || got[2].AnnotationIDs[1] != 5 {
		t.Fatalf("submitted = %+v", got[2])
	}
}

func TestSubmittedKeepTheirWholeNotesAndTargets(t *testing.T) {
	got := Submitted([]protocol.SubmittedAnnotation{
		{OriginalAnnotationID: 2, Anchor: FileAnchor("workspace_a", "a.go", "rev", 3, 4), Body: "the whole note", Preview: protocol.AnnotationPreview{StartLine: 3, EndLine: 4, Text: "a\nb"}},
		{OriginalAnnotationID: 3, Anchor: DiffAnchor("difftarget_a", "diffrev_a", "a.go", "diff_file_a", "new", 1, 1), DiffTarget: &protocol.PinnedDiffTarget{Kind: protocol.DiffTargetCommit}},
	})
	if got[0].ID != 2 || got[0].Body != "the whole note" || got[0].Preview != "a\nb" || got[0].Kind != "file" {
		t.Fatalf("file = %+v", got[0])
	}
	if got[1].TargetKind != protocol.DiffTargetCommit {
		t.Fatalf("diff = %+v", got[1])
	}
	if diff := Summaries([]protocol.AnnotationSummary{{ID: 1, Anchor: DiffAnchor("difftarget_a", "diffrev_a", "a.go", "diff_file_a", "old", 1, 1)}}); diff[0].TargetKind != protocol.DiffTargetWorkingTree {
		t.Fatalf("working tree = %+v", diff[0])
	}
}

func TestMessagesCarryTheirSentAnnotations(t *testing.T) {
	got := Messages([]protocol.TranscriptMessage{{
		ID: "message_a", Role: "user",
		Content: []protocol.TranscriptContent{
			protocol.TextBlock("look"),
			protocol.NewTranscriptContent(protocol.AnnotationsContent{Annotations: []protocol.SubmittedAnnotation{
				{OriginalAnnotationID: 4, Anchor: FileAnchor("workspace_a", "a.go", "rev", 1, 1), Body: "why"},
			}}),
		},
	}})
	blocks := got[0].Content
	if len(blocks) != 2 || blocks[1].Kind != "annotations" || len(blocks[1].Annotations) != 1 || blocks[1].Annotations[0].Body != "why" {
		t.Fatalf("content = %+v", blocks)
	}
}
