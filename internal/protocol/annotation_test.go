package protocol

import (
	"encoding/base64"
	"testing"
)

func validTestAnnotation() Annotation {
	return Annotation{
		ID: 1, SessionID: "session_0123456789abcdef0123456789abcdef",
		Anchor: AnnotationAnchor{Kind: AnnotationAnchorWorkspaceFile, WorkspaceFile: &WorkspaceFileAnnotationAnchor{
			WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4",
			Path:        "internal/tui/app.go", FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A",
			StartLine: 10, EndLine: 12,
		}},
		Body: "Keep this bounded.", Preview: AnnotationPreview{StartLine: 10, EndLine: 12, Text: "one\ntwo\nthree"},
	}
}

func TestAnnotationValidation(t *testing.T) {
	valid := validTestAnnotation()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid annotation: %v", err)
	}
	deferred := valid
	deferred.ValidationDeferred = true
	if err := deferred.Validate(); err != nil {
		t.Fatalf("deferred annotation: %v", err)
	}
	cases := []Annotation{valid, valid, valid, valid, valid, valid}
	cases[0].ID = 0
	cases[1].Anchor.WorkspaceFile = nil
	cases[2].Body = " "
	cases[3].Preview.EndLine = 11
	cases[4].Stale = true
	cases[5].Stale = true
	cases[5].StaleReason = AnnotationStaleFile
	cases[5].ValidationDeferred = true
	for index, candidate := range cases {
		if err := candidate.Validate(); err == nil {
			t.Errorf("case %d unexpectedly valid", index)
		}
	}
}

func TestWorkingTreeDiffAnnotationValidation(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	anchor := AnnotationAnchor{Kind: AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &WorkingTreeDiffAnnotationAnchor{
		TargetID: "difftarget_" + token, TargetRevision: "diffrev_" + token,
		Path: "internal/tui/app.go", FileRevision: "diff_file_" + token,
		Side: "old", StartLine: 4, EndLine: 6,
	}}
	if err := anchor.Validate(); err != nil {
		t.Fatalf("valid diff anchor: %v", err)
	}
	if err := (AnnotationPreview{StartLine: 4, EndLine: 6, Text: "old evidence"}).Validate(anchor); err != nil {
		t.Fatalf("valid diff preview: %v", err)
	}
	anchor.WorkspaceFile = validTestAnnotation().Anchor.WorkspaceFile
	if err := anchor.Validate(); err == nil {
		t.Fatal("mixed annotation variants were valid")
	}
}

func TestAnnotationAnchorRangeBound(t *testing.T) {
	anchor := validTestAnnotation().Anchor
	anchor.WorkspaceFile.EndLine = anchor.WorkspaceFile.StartLine + MaxAnnotationRangeLines - 1
	if err := anchor.Validate(); err != nil {
		t.Fatalf("maximum range: %v", err)
	}
	anchor.WorkspaceFile.EndLine++
	if err := anchor.Validate(); err == nil {
		t.Fatal("oversized range was valid")
	}
}

func TestAnnotationPageRequiresOrderedNonNilEntries(t *testing.T) {
	annotation := validTestAnnotation()
	page := AnnotationPage{SessionID: annotation.SessionID, Entries: []Annotation{annotation}}
	if err := page.Validate(); err != nil {
		t.Fatalf("valid page: %v", err)
	}
	page.Entries = nil
	if err := page.Validate(); err == nil {
		t.Fatal("nil entries were valid")
	}
	second := annotation
	second.ID = 1
	page.Entries = []Annotation{annotation, second}
	if err := page.Validate(); err == nil {
		t.Fatal("duplicate ids were valid")
	}
}

func TestAnnotationSessionEventValidation(t *testing.T) {
	annotation := validTestAnnotation()
	event := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: annotation.SessionID,
		Kind: SessionEventAnnotationCreated, AnnotationID: annotation.ID, Annotation: &annotation,
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("valid annotation event: %v", err)
	}
	event.AnnotationID = 0
	if err := event.Validate(); err == nil {
		t.Fatal("annotation event without id was valid")
	}
	submitted := SessionEvent{
		StreamID: "stream_test", Sequence: 2, SessionID: annotation.SessionID,
		Kind: SessionEventAnnotationSubmitted, AnnotationIDs: []uint64{2, 1},
		AcceptedMessageID: "message_0123456789abcdef0123456789abcdef",
	}
	if err := submitted.Validate(); err != nil {
		t.Fatalf("valid submission event: %v", err)
	}
}

func TestSubmittedAnnotationValidation(t *testing.T) {
	annotation := validTestAnnotation()
	submitted := SubmittedAnnotation{OriginalAnnotationID: annotation.ID, Anchor: annotation.Anchor, Body: annotation.Body, Preview: annotation.Preview}
	if err := submitted.Validate(); err != nil {
		t.Fatalf("valid submitted annotation: %v", err)
	}
}
