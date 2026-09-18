package workingdiff

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/workspace"
)

func committedAnnotationFixture(t *testing.T) (string, *workspace.Service, *Service, protocol.DiffPage, protocol.PinnedDiffTarget) {
	t.Helper()
	dir := fixture(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-qm", "annotation target")
	workspaces := workspace.NewService()
	service, err := NewService(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := workspaces.Ref("session_test", dir).WorkspaceID
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	target := findTarget(t, catalog, protocol.DiffTargetCommit, func(entry protocol.DiffTargetEntry) bool { return entry.Metadata.Subject == "annotation target" })
	page, err := service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: target.Reference, ExpectedTargetID: target.TargetID})
	if err != nil {
		t.Fatal(err)
	}
	definition := protocol.PinnedDiffTarget{WorkspaceID: workspaceID, Kind: page.Observation.Target.Kind, Base: page.Observation.Target.Base, Head: page.Observation.Target.Head}
	return dir, workspaces, service, page, definition
}

func TestReadLineRangeReconstructsCommittedEvidenceAfterExpiryAndRestart(t *testing.T) {
	dir, workspaces, service, page, definition := committedAnnotationFixture(t)
	input := LineRangeInput{
		TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Target: &definition,
		Path: "a.txt", FileRevision: page.Files[0].FileRevision, Side: "old", StartLine: 2, EndLine: 2,
	}
	service.now = func() time.Time { return time.Now().Add(observationTTL + time.Minute) }
	evidence, err := service.ReadLineRange(t.Context(), "session_test", dir, input)
	if err != nil || evidence.Content != "two" || evidence.Target == nil || *evidence.Target != definition {
		t.Fatalf("expired evidence = %+v, %v", evidence, err)
	}

	restarted, err := NewService(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err = restarted.ReadLineRange(t.Context(), "session_test", dir, input)
	if err != nil || evidence.Content != "two" || evidence.Target == nil || *evidence.Target != definition {
		t.Fatalf("restarted evidence = %+v, %v", evidence, err)
	}
}

func TestReadLineRangeRejectsTamperedCommittedTargetDefinition(t *testing.T) {
	dir, _, service, page, definition := committedAnnotationFixture(t)
	definition.Head.OID = definition.Base.OID
	input := LineRangeInput{
		TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Target: &definition,
		Path: "a.txt", FileRevision: page.Files[0].FileRevision, Side: "new", StartLine: 2, EndLine: 2,
	}
	_, err := service.ReadLineRange(t.Context(), "session_test", dir, input)
	var diffErr *Error
	if !errors.As(err, &diffErr) || diffErr.Code != StaleTarget {
		t.Fatalf("error = %v", err)
	}
}
