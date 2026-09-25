package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/protocol"
	kitsession "github.com/akonwi/kit/internal/session"
	kitstorage "github.com/akonwi/kit/internal/storage"
	kitworkingdiff "github.com/akonwi/kit/internal/workingdiff"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
)

type unusedAnnotationFileReader struct{}

func (unusedAnnotationFileReader) ReadFile(context.Context, string, string, kitannotation.WorkspaceFileAnchor) (kitannotation.FileEvidence, error) {
	return kitannotation.FileEvidence{}, nil
}

func annotationGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestCommittedAnnotationCreateListAndSubmitAfterDiffServiceRestart(t *testing.T) {
	dir := t.TempDir()
	annotationGit(t, dir, "init", "-q")
	annotationGit(t, dir, "config", "user.name", "Test")
	annotationGit(t, dir, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	annotationGit(t, dir, "add", "a.txt")
	annotationGit(t, dir, "commit", "-qm", "base")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	annotationGit(t, dir, "add", "a.txt")
	annotationGit(t, dir, "commit", "-qm", "annotated")

	const sessionID = "session_0123456789abcdef0123456789abcdef"
	storePath := filepath.Join(t.TempDir(), "kit.db")
	store, err := kitstorage.Open(t.Context(), storePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(t.Context(), kitsession.NewSession{ID: sessionID, ScratchpadOwnerID: sessionID, CWD: dir, Persistent: true, ModelProvider: "test", ModelID: "model"}); err != nil {
		t.Fatal(err)
	}
	workspaces := kitworkspace.NewService()
	diffs, err := kitworkingdiff.NewService(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := workspaces.Ref(sessionID, dir).WorkspaceID
	catalog, err := diffs.ListTargets(t.Context(), sessionID, dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	var target protocol.DiffTargetEntry
	for _, candidate := range catalog.Targets {
		if candidate.Kind == protocol.DiffTargetCommit && candidate.Metadata.Subject == "annotated" {
			target = candidate
			break
		}
	}
	page, err := diffs.ObserveTarget(t.Context(), sessionID, dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: target.Reference, ExpectedTargetID: target.TargetID})
	if err != nil || len(page.Files) != 1 {
		t.Fatalf("observation = %+v, %v", page, err)
	}
	annotations, err := kitannotation.NewService(store, unusedAnnotationFileReader{}, annotationDiffReader{service: diffs})
	if err != nil {
		t.Fatal(err)
	}
	anchor := protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &protocol.WorkingTreeDiffAnnotationAnchor{
		TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "a.txt",
		FileRevision: page.Files[0].FileRevision, Side: "old", StartLine: 2, EndLine: 2,
	}}
	created, err := annotations.Create(t.Context(), sessionID, dir, anchor, "keep old evidence")
	if err != nil || created.DiffTarget == nil || created.DiffTarget.Kind != protocol.DiffTargetCommit {
		t.Fatalf("created = %+v, %v", created, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restartedStore, err := kitstorage.Open(t.Context(), storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restartedStore.Close()
	restartedDiffs, err := kitworkingdiff.NewService(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	restartedAnnotations, err := kitannotation.NewService(restartedStore, unusedAnnotationFileReader{}, annotationDiffReader{service: restartedDiffs})
	if err != nil {
		t.Fatal(err)
	}
	records, stale, err := restartedAnnotations.List(t.Context(), sessionID, dir, 0, 10)
	if err != nil || len(records) != 1 || len(stale) != 0 || records[0].Preview.Text != "two" {
		t.Fatalf("records = %+v, stale = %+v, %v", records, stale, err)
	}
	prepared, err := restartedAnnotations.PrepareSubmission(t.Context(), sessionID, dir, []uint64{created.ID})
	if err != nil || len(prepared.Records) != 1 || prepared.Records[0].DiffTarget == nil {
		t.Fatalf("prepared = %+v, %v", prepared, err)
	}
	prepared.Abort()
}
