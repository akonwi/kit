package tui

import (
	"reflect"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestAnnotationEventsUseIdleMetadataStream(t *testing.T) {
	for _, kind := range []protocol.SessionEventKind{
		protocol.SessionEventAnnotationCreated, protocol.SessionEventAnnotationUpdated,
		protocol.SessionEventAnnotationDeleted, protocol.SessionEventAnnotationSubmitted,
	} {
		if !sessionMetadataEvent(kind) {
			t.Fatalf("%s is not treated as metadata", kind)
		}
	}
}

func TestApplyAnnotationEventsKeepsOrderedLiveChips(t *testing.T) {
	state := &appState{}
	annotation := func(id uint64, body string) protocol.Annotation {
		return protocol.Annotation{
			ID: id, SessionID: "session_0123456789abcdef0123456789abcdef",
			Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
				WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4", Path: "main.go",
				FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A", StartLine: 2, EndLine: 3,
			}},
			Body: body, Preview: protocol.AnnotationPreview{StartLine: 2, EndLine: 3, Text: "source"},
		}
	}
	second, first := annotation(2, "second"), annotation(1, "first")
	state.applyAnnotationEvent(protocol.SessionEvent{Kind: protocol.SessionEventAnnotationCreated, Annotation: &second})
	state.applyAnnotationEvent(protocol.SessionEvent{Kind: protocol.SessionEventAnnotationCreated, Annotation: &first})
	if got := state.annotationIDs(); !reflect.DeepEqual(got, []uint64{1, 2}) {
		t.Fatalf("annotation ids = %v", got)
	}
	first.Body = "updated"
	state.applyAnnotationEvent(protocol.SessionEvent{Kind: protocol.SessionEventAnnotationUpdated, Annotation: &first})
	if state.annotations[0].BodyPreview != "updated" {
		t.Fatalf("updated annotation = %+v", state.annotations[0])
	}
	state.applyAnnotationEvent(protocol.SessionEvent{Kind: protocol.SessionEventAnnotationSubmitted, AnnotationIDs: []uint64{2, 1}})
	if len(state.annotations) != 0 {
		t.Fatalf("submitted annotations remain = %+v", state.annotations)
	}
}
