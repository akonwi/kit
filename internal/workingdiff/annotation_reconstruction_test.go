package workingdiff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	kitannotation "github.com/akonwi/kit/internal/annotation"
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

type reconstructionFileReader struct{}

func (reconstructionFileReader) ReadFile(context.Context, string, string, kitannotation.WorkspaceFileAnchor) (kitannotation.FileEvidence, error) {
	return kitannotation.FileEvidence{}, errors.New("unexpected workspace read")
}

type reconstructionDiffReader struct{ service *Service }

func (r reconstructionDiffReader) ReadDiff(ctx context.Context, sessionID, cwd string, anchor kitannotation.WorkingTreeDiffAnchor, target *protocol.PinnedDiffTarget, derive bool) (kitannotation.FileEvidence, error) {
	evidence, err := r.service.ReadLineRange(ctx, sessionID, cwd, LineRangeInput{
		TargetID: anchor.TargetID, TargetRevision: anchor.TargetRevision, Path: anchor.Path, FileRevision: anchor.FileRevision,
		Side: anchor.Side, StartLine: anchor.StartLine, EndLine: anchor.EndLine, Target: target, DeriveTarget: derive,
	})
	return kitannotation.FileEvidence{Content: evidence.Content, ContentStartLine: anchor.StartLine, CompleteLineCount: evidence.EndLine, DiffTarget: evidence.Target}, err
}

func TestCommittedAnnotationActivationReconstructsAfterSecondExpiry(t *testing.T) {
	dir, _, diffs, page, _ := committedAnnotationFixture(t)
	annotations, err := kitannotation.NewService(kitannotation.NewMemoryRepository(), reconstructionFileReader{}, reconstructionDiffReader{service: diffs})
	if err != nil {
		t.Fatal(err)
	}
	anchor := protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &protocol.WorkingTreeDiffAnnotationAnchor{
		TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: page.Files[0].Path,
		FileRevision: page.Files[0].FileRevision, Side: "old", StartLine: 2, EndLine: 2,
	}}
	created, err := annotations.Create(t.Context(), "session_test", dir, anchor, "keep this")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now().Add(observationTTL + time.Minute)
	diffs.now = func() time.Time { return clock }
	if _, stale, err := annotations.List(t.Context(), "session_test", dir, 0, 10); err != nil || len(stale) != 0 {
		t.Fatalf("first reconstruction stale=%v err=%v", stale, err)
	}
	clock = clock.Add(observationTTL + time.Minute)
	input := protocol.ReadFileDiffInput{TargetID: anchor.WorkingTreeDiff.TargetID, TargetRevision: anchor.WorkingTreeDiff.TargetRevision, Path: anchor.WorkingTreeDiff.Path, ExpectedFileRevision: anchor.WorkingTreeDiff.FileRevision, AnnotationID: created.ID}
	target, err := annotations.AuthorizeDiffRead(t.Context(), "session_test", dir, created.ID, input)
	if err != nil {
		t.Fatalf("activation authorization: %v", err)
	}
	file, err := diffs.ReadFileForAnnotation(t.Context(), "session_test", dir, input, target)
	if err != nil || file.File.FileRevision != anchor.WorkingTreeDiff.FileRevision {
		t.Fatalf("activated file=%+v err=%v", file, err)
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
