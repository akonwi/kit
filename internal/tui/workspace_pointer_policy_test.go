package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestDockOwnershipBlocksBackgroundAnnotationMutation(t *testing.T) {
	files := &fileViewerSession{results: []protocol.WorkspaceFileRead{fileViewerRead("workspace", "main.go", "revision", "one\ntwo\n")}}
	anchor := protocol.WorkspaceFileAnnotationAnchor{WorkspaceID: "workspace", Path: "main.go", FileRevision: "revision", StartLine: 1, EndLine: 1}
	annotation := protocol.AnnotationSummary{ID: 9, BodyPreview: "Saved note", Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &anchor}}
	loads := 0
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace", "main.go"), workspace: "workspace", active: true, show: true, files: files, annotations: []protocol.AnnotationSummary{annotation}, annotationLoader: func(_ uint64, done func(string, error)) func() { loads++; done("Saved note", nil); return func() {} }}
	owner := inputInteraction
	policy := panePointerPolicy(shellView{Callbacks: shellCallbacks{InputOwner: func() inputOwner { return owner }}})
	application := uitest.New(ui.Provider[workspacePointerPolicy]{Value: policy, Child: filePaneHarness{model: model}})
	pumpUntil(t, application, model, 60, 16, "Saved note")
	rows := paintedRows(application, 60, 16)
	removeCol, removeRow := findTextCell(t, rows, glyphTimes)
	editCol, editRow := findTextCell(t, rows, "Saved note")
	gutterCol, gutterRow := findTextCell(t, rows, "2 │")
	application.Click(removeCol, removeRow)
	application.Click(editCol, editRow)
	application.Click(gutterCol, gutterRow)
	application.Send(ui.Mouse{Col: gutterCol, Row: gutterRow, Button: ui.MouseLeftButton, EventType: ui.EventRelease})
	if len(model.removed) != 0 || len(model.annotated) != 0 || loads != 0 {
		t.Fatalf("dock allowed background mutations: removed=%v created=%v loads=%d", model.removed, model.annotated, loads)
	}
	// Ownership, not an old rendered frame, controls activation.
	owner = inputBase
	application.Click(removeCol, removeRow)
	if len(model.removed) != 1 || model.removed[0] != 9 {
		t.Fatalf("eligible annotation removal=%v, want [9]", model.removed)
	}
}
