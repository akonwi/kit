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
