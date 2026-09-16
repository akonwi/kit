package tui

import (
	"fmt"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestAnnotationPickerShowsFrozenStaleEvidence(t *testing.T) {
	annotation := protocol.AnnotationSummary{
		ID: 1, BodyPreview: "Still apply this change", Preview: "old source", Stale: true, StaleReason: protocol.AnnotationStaleFile,
		Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
			WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4", Path: "main.go",
			FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A", StartLine: 2, EndLine: 2,
		}},
	}
	application := uitest.New(annotationPickerSurface{Snapshot: annotationPickerSnapshot{Open: true, Annotations: []protocol.AnnotationSummary{annotation}}})
	application.Pump(72, 16)
	for _, want := range []string{"Frozen evidence", "old source", "Still apply this change", "r re-anchor"} {
		if !application.Contains(want) {
			t.Fatalf("stale detail missing %q:\n%s", want, application.Text())
		}
	}
}

func TestAnnotationPickerWindowsLegalAnnotationCounts(t *testing.T) {
	annotations := make([]protocol.AnnotationSummary, 128)
	for index := range annotations {
		annotations[index] = protocol.AnnotationSummary{
			ID: uint64(index + 1), BodyPreview: "comment", Preview: "source",
			Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
				WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4", Path: fmt.Sprintf("file-%03d.go", index+1),
				FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A", StartLine: 1, EndLine: 1,
			}},
		}
	}
	application := uitest.New(annotationPickerSurface{Snapshot: annotationPickerSnapshot{Open: true, Selection: 127, Annotations: annotations}})
	application.Pump(72, 18)
	if !application.Contains("128 live") || !application.Contains("file-128.go") || application.Contains("file-001.go") {
		t.Fatalf("picker window is not bounded around selection:\n%s", application.Text())
	}
}
