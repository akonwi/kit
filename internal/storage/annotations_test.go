package storage

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"

	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/session"
)

func TestDiffAnnotationAnchorRoundTrips(t *testing.T) {
	store, sessionID := annotationTestStore(t)
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	record := annotationTestRecord(sessionID, "review this change")
	record.Anchor = protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &protocol.WorkingTreeDiffAnnotationAnchor{
		TargetID: "difftarget_" + token, TargetRevision: "diffrev_" + token, Path: "main.go",
		FileRevision: "diff_file_" + token, Side: "old", StartLine: 2, EndLine: 3,
	}}
	record.Preview = kitannotation.Preview{StartLine: 2, EndLine: 3, Text: "old evidence"}
	created, err := store.CreateAnnotation(t.Context(), record, 10)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetAnnotation(t.Context(), sessionID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Anchor.WorkingTreeDiff == nil || *loaded.Anchor.WorkingTreeDiff != *record.Anchor.WorkingTreeDiff {
		t.Fatalf("loaded diff anchor = %+v", loaded.Anchor)
	}
}

func TestAnnotationIDsIncreaseAndAreNeverReused(t *testing.T) {
	store, sessionID := annotationTestStore(t)
	first, err := store.CreateAnnotation(t.Context(), annotationTestRecord(sessionID, "first"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAnnotation(t.Context(), sessionID, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateAnnotation(t.Context(), annotationTestRecord(sessionID, "second"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("allocated ids = %d, %d", first.ID, second.ID)
	}
}

func TestAnnotationMutationOrderingAndCapacity(t *testing.T) {
	store, sessionID := annotationTestStore(t)
	first, err := store.CreateAnnotation(t.Context(), annotationTestRecord(sessionID, "first"), 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateAnnotation(t.Context(), annotationTestRecord(sessionID, "second"), 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateAnnotation(t.Context(), annotationTestRecord(sessionID, "third"), 2); !errors.Is(err, kitannotation.ErrCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
	updated, err := store.UpdateAnnotationBody(t.Context(), sessionID, first.ID, "updated")
	if err != nil || updated.Body != "updated" {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	records, err := store.ListAnnotations(t.Context(), sessionID, 0, 10)
	if err != nil || len(records) != 2 || records[0].ID != first.ID || records[1].ID != second.ID {
		t.Fatalf("records = %+v, %v", records, err)
	}
	if err := store.DeleteAnnotation(t.Context(), sessionID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateAnnotationBody(t.Context(), sessionID, first.ID, "late"); !errors.Is(err, kitannotation.ErrNotFound) {
		t.Fatalf("late update error = %v", err)
	}
}

func TestAnnotationSubmissionReservationRecovery(t *testing.T) {
	store, sessionID := annotationTestStore(t)
	first, err := store.CreateAnnotation(t.Context(), annotationTestRecord(sessionID, "first"), 10)
	if err != nil {
		t.Fatal(err)
	}
	const submissionID = "annotation_submission_0123456789abcdef0123456789abcdef"
	if err := store.ReserveAnnotations(t.Context(), sessionID, submissionID, []uint64{first.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAnnotation(t.Context(), sessionID, first.ID); !errors.Is(err, kitannotation.ErrNotFound) {
		t.Fatalf("reserved annotation error = %v", err)
	}
	pending, err := store.PendingAnnotationSubmissions(t.Context(), sessionID)
	if err != nil || len(pending) != 1 || pending[0] != submissionID {
		t.Fatalf("pending = %v, %v", pending, err)
	}
	if err := store.RollbackAnnotationSubmission(t.Context(), sessionID, submissionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAnnotation(t.Context(), sessionID, first.ID); err != nil {
		t.Fatalf("rolled back annotation: %v", err)
	}
	if err := store.ReserveAnnotations(t.Context(), sessionID, submissionID, []uint64{first.ID}); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeAnnotationSubmission(t.Context(), sessionID, submissionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAnnotation(t.Context(), sessionID, first.ID); !errors.Is(err, kitannotation.ErrNotFound) {
		t.Fatalf("finalized annotation error = %v", err)
	}
}

func annotationTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sessionID := testSessionID('n')
	if _, err := store.CreateSession(t.Context(), session.NewSession{ID: sessionID, CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model"}); err != nil {
		t.Fatal(err)
	}
	return store, sessionID
}

func annotationTestRecord(sessionID, body string) kitannotation.Record {
	return kitannotation.Record{
		SessionID: sessionID,
		Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &kitannotation.WorkspaceFileAnchor{
			WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4",
			Path:        "file.go", FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A",
			StartLine: 1, EndLine: 1,
		}},
		Body:    body,
		Preview: kitannotation.Preview{StartLine: 1, EndLine: 1, Text: "package main"},
	}
}
