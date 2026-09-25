package annotation

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestQueuedSubmissionRetainsOwnershipAfterFailedAcceptance(t *testing.T) {
	repository := NewMemoryRepository()
	reader := &staticReader{content: "captured evidence"}
	service, err := NewService(repository, reader)
	if err != nil {
		t.Fatal(err)
	}
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	record, err := service.Create(t.Context(), "session_test", "/repo", protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &WorkspaceFileAnchor{WorkspaceID: "workspace_" + token, Path: "file.txt", FileRevision: "file_" + token, StartLine: 1, EndLine: 1}}, "Captured instruction")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.PrepareSubmission(t.Context(), record.SessionID, "/repo", []uint64{record.ID})
	if err != nil {
		t.Fatal(err)
	}
	queued := prepared.Queue()
	defer queued.Release()
	prepared.Abort() // The deferred admission cleanup must not release queue ownership.
	reader.err = ErrStale
	acceptance, err := queued.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptance.Reserve(t.Context(), "annotation_submission_first"); err != nil {
		acceptance.Abort()
		t.Fatal(err)
	}
	acceptance.Abort()
	if !service.isQueued(record.SessionID, record.ID) {
		t.Fatal("failed acceptance released the queued annotation")
	}
	if _, err := repository.GetAnnotation(t.Context(), record.SessionID, record.ID); err != nil {
		t.Fatalf("failed acceptance lost the durable draft: %v", err)
	}
	acceptance, err = queued.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := acceptance.Records[0]; got.Body != "Captured instruction" || got.Preview.Text != "captured evidence" {
		acceptance.Abort()
		t.Fatalf("captured record = %+v", got)
	}
	if err := acceptance.Reserve(t.Context(), "annotation_submission_second"); err != nil {
		acceptance.Abort()
		t.Fatal(err)
	}
	if err := acceptance.Commit(); err != nil {
		acceptance.Release()
		t.Fatal(err)
	}
	acceptance.Release()
	if service.isQueued(record.SessionID, record.ID) {
		t.Fatal("accepted annotation retained queue ownership")
	}
}
