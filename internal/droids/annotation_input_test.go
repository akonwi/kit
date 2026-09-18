package droids

import (
	"strings"
	"testing"
	"time"
)

func TestAnnotationInputCanonicalRoundTrip(t *testing.T) {
	annotation := SubmittedAnnotation{
		ID: 7, Kind: "workspace_file", WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4", Path: "main.go", FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A",
		StartLine: 2, EndLine: 3, Preview: "line two\nline three", Body: "Handle this.",
	}
	envelope := MessageEnvelope{
		ID: "message_annotation", ConversationID: "conversation_annotation", TurnID: "turn_annotation", CreatedAt: time.Unix(1, 0).UTC(),
		Message: UserMessage{Content: []InputContent{AnnotationInput{SubmissionID: "annotation_submission_0123456789abcdef0123456789abcdef", Text: "model projection", Annotations: []SubmittedAnnotation{annotation}}}},
	}
	encoded, err := encodeMessageEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMessageEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	input := decoded.Message.(UserMessage).Content[0].(AnnotationInput)
	if input.Text != "model projection" || len(input.Annotations) != 1 || input.Annotations[0] != annotation {
		t.Fatalf("input = %+v", input)
	}
}

func TestCommittedDiffAnnotationInputRoundTrip(t *testing.T) {
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	annotation := SubmittedAnnotation{
		ID: 8, Kind: "working_tree_diff", TargetID: "difftarget_" + token, TargetRevision: "diffrev_" + token,
		TargetWorkspaceID: "workspace_" + token, TargetKind: "branch", TargetBaseKind: "commit", TargetBaseOID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		TargetHeadKind: "commit", TargetHeadOID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Path: "main.go", FileRevision: "diff_file_" + token, Side: "new", StartLine: 4, EndLine: 4, Preview: "changed", Body: "Review this.",
	}
	envelope := MessageEnvelope{
		ID: "message_diff_annotation", ConversationID: "conversation_annotation", TurnID: "turn_annotation", CreatedAt: time.Unix(1, 0).UTC(),
		Message: UserMessage{Content: []InputContent{AnnotationInput{SubmissionID: "annotation_submission_0123456789abcdef0123456789abcdef", Text: "projection", Annotations: []SubmittedAnnotation{annotation}}}},
	}
	encoded, err := encodeMessageEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMessageEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Message.(UserMessage).Content[0].(AnnotationInput).Annotations[0]
	if got != annotation {
		t.Fatalf("annotation = %+v", got)
	}
}

func TestDiffAnnotationInputValidation(t *testing.T) {
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	annotation := SubmittedAnnotation{
		ID: 8, Kind: "working_tree_diff", TargetID: "difftarget_" + token, TargetRevision: "diffrev_" + token,
		TargetWorkspaceID: "workspace_" + token, TargetKind: "commit", TargetBaseKind: "empty_tree",
		TargetHeadKind: "commit", TargetHeadOID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Path: "main.go", FileRevision: "diff_file_" + token, Side: "new",
		StartLine: 4, EndLine: 4, Preview: "changed", Body: "Review this.",
	}
	input := AnnotationInput{SubmissionID: "annotation_submission_0123456789abcdef0123456789abcdef", Text: "projection", Annotations: []SubmittedAnnotation{annotation}}
	if err := validateAnnotationInput(input); err != nil {
		t.Fatalf("valid diff annotation: %v", err)
	}
	annotation.Side = "both"
	input.Annotations[0] = annotation
	if err := validateAnnotationInput(input); err == nil {
		t.Fatal("invalid diff side was accepted")
	}
	annotation.Side = "new"
	annotation.TargetHeadOID = "tampered"
	input.Annotations[0] = annotation
	if err := validateAnnotationInput(input); err == nil {
		t.Fatal("invalid pinned target was accepted")
	}
}

func TestAnnotationInputValidation(t *testing.T) {
	_, err := inputToMessage(Input{Content: []InputContent{AnnotationInput{SubmissionID: "annotation_submission_0123456789abcdef0123456789abcdef", Text: "projection"}}})
	if err == nil {
		t.Fatal("empty annotation input was accepted")
	}
	base := SubmittedAnnotation{
		ID: 1, Kind: "workspace_file", WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4",
		FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A", StartLine: 1, EndLine: 1, Preview: "source", Body: "instruction",
	}
	for _, invalidPath := range []string{"../secret", ".", "bad\x1bpath", strings.Repeat("a", 4097)} {
		annotation := base
		annotation.Path = invalidPath
		_, err := inputToMessage(Input{Content: []InputContent{AnnotationInput{
			SubmissionID: "annotation_submission_0123456789abcdef0123456789abcdef", Text: "projection", Annotations: []SubmittedAnnotation{annotation},
		}}})
		if err == nil {
			t.Fatalf("invalid annotation path %q was accepted", invalidPath)
		}
	}
}
