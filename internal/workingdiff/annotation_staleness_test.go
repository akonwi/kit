package workingdiff

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/workspace"
)

// workingTreeAnchorFixture changes a.txt and b.txt, observes the working tree,
// and returns a range on a.txt's new side lines 2-3 ("changed\nadded\n").
func workingTreeAnchorFixture(t *testing.T) (string, *Service, string, LineRangeInput) {
	t.Helper()
	dir := fixture(t)
	write(t, dir, "b.txt", "bee\n")
	git(t, dir, "add", "b.txt")
	git(t, dir, "commit", "-qm", "b")
	write(t, dir, "a.txt", "one\nchanged\nadded\n")
	write(t, dir, "b.txt", "bee\nbuzz\n")
	workspaces := workspace.NewService()
	service, err := NewService(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := workspaces.Ref("session_test", dir).WorkspaceID
	page, err := service.Observe(t.Context(), "session_test", dir, protocol.ObserveWorkingTreeInput{WorkspaceID: workspaceID})
	if err != nil || len(page.Files) != 2 || page.Files[0].Path != "a.txt" {
		t.Fatalf("observation = %+v, %v", page, err)
	}
	return dir, service, workspaceID, LineRangeInput{
		TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "a.txt",
		FileRevision: page.Files[0].FileRevision, Side: "new", StartLine: 2, EndLine: 3,
	}
}

func write(t *testing.T, dir, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readRange(t *testing.T, service *Service, dir string, input LineRangeInput) (LineRangeEvidence, error) {
	t.Helper()
	return service.ReadLineRange(t.Context(), "session_test", dir, input)
}

func wantRange(t *testing.T, service *Service, dir string, input LineRangeInput) {
	t.Helper()
	evidence, err := readRange(t, service, dir, input)
	if err != nil || evidence.Content != "changed\nadded\n" || evidence.EndLine != 3 || evidence.Target != nil {
		t.Fatalf("evidence = %+v, %v; want lines 2-3 of a.txt", evidence, err)
	}
}

func wantDiffError(t *testing.T, err error, code protocol.DiffErrorCode) {
	t.Helper()
	var diffErr *Error
	if !errors.As(err, &diffErr) || diffErr.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func retainedCount(service *Service) int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return len(service.observations)
}

func evictAll(service *Service) {
	service.mu.Lock()
	defer service.mu.Unlock()
	clear(service.observations)
}

func TestWorkingTreeLineRangeStaysValidWhileOtherFilesChange(t *testing.T) {
	dir, service, _, input := workingTreeAnchorFixture(t)
	write(t, dir, "b.txt", "bee\nbuzz\nhum\n")
	wantRange(t, service, dir, input)

	write(t, dir, "a.txt", "one\nchanged\nadded\n")
	wantRange(t, service, dir, input)

	write(t, dir, "c.txt", "new file\n")
	git(t, dir, "add", "c.txt")
	wantRange(t, service, dir, input)

	write(t, dir, "b.txt", "bee\n")
	git(t, dir, "update-index", "-q", "--refresh")
	wantRange(t, service, dir, input)
}

func TestWorkingTreeLineRangeUsesTheClientsCurrentObservation(t *testing.T) {
	dir, service, workspaceID, input := workingTreeAnchorFixture(t)
	write(t, dir, "b.txt", "bee\nbuzz\nhum\n")
	page, err := service.Observe(t.Context(), "session_test", dir, protocol.ObserveWorkingTreeInput{WorkspaceID: workspaceID})
	if err != nil || page.Observation.Revision == input.TargetRevision {
		t.Fatalf("second observation = %+v, %v", page.Observation, err)
	}
	evictAnchored(service, input.TargetRevision)
	wantRange(t, service, dir, input)
	if count := retainedCount(service); count != 1 {
		t.Fatalf("retained observations = %d, want the client's one", count)
	}
}

func evictAnchored(service *Service, revision string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	delete(service.observations, revision)
}

// A file that no longer differs is not_found in the diff; the annotation
// server reports not_found and stale_file alike as stale_file.
func TestWorkingTreeLineRangeIsStaleWhenItsOwnFileChanges(t *testing.T) {
	for name, test := range map[string]struct {
		change func(t *testing.T, dir string, service *Service, workspaceID string)
		want   protocol.DiffErrorCode
	}{
		"anchored observation retained": {func(t *testing.T, dir string, _ *Service, _ string) {
			write(t, dir, "a.txt", "one\nchanged\nedited\n")
		}, StaleFile},
		"another file changed too": {func(t *testing.T, dir string, _ *Service, _ string) {
			write(t, dir, "a.txt", "one\nchanged\nedited\n")
			write(t, dir, "b.txt", "bee\nbuzz\nhum\n")
		}, StaleFile},
		"client observed the edit": {func(t *testing.T, dir string, service *Service, workspaceID string) {
			write(t, dir, "a.txt", "one\nchanged\nedited\n")
			if _, err := service.Observe(t.Context(), "session_test", dir, protocol.ObserveWorkingTreeInput{WorkspaceID: workspaceID}); err != nil {
				t.Fatal(err)
			}
		}, StaleFile},
		"file reverted to its base": {func(t *testing.T, dir string, _ *Service, _ string) {
			write(t, dir, "a.txt", "one\ntwo\n")
			git(t, dir, "update-index", "-q", "--refresh")
		}, NotFound},
		"file committed": {func(t *testing.T, dir string, _ *Service, _ string) {
			git(t, dir, "commit", "-qam", "both")
		}, NotFound},
	} {
		t.Run(name, func(t *testing.T) {
			dir, service, workspaceID, input := workingTreeAnchorFixture(t)
			test.change(t, dir, service, workspaceID)
			_, err := readRange(t, service, dir, input)
			wantDiffError(t, err, test.want)
		})
	}
}

func TestWorkingTreeLineRangeAcceptsAFileRestoredAfterTheClientObservedAnEdit(t *testing.T) {
	dir, service, workspaceID, input := workingTreeAnchorFixture(t)
	write(t, dir, "a.txt", "one\nchanged\nedited\n")
	if _, err := service.Observe(t.Context(), "session_test", dir, protocol.ObserveWorkingTreeInput{WorkspaceID: workspaceID}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "one\nchanged\nadded\n")
	wantRange(t, service, dir, input)
}

func TestWorkingTreeLineRangeRevalidatesAfterItsObservationIsEvicted(t *testing.T) {
	dir, service, _, input := workingTreeAnchorFixture(t)
	evictAll(service)
	wantRange(t, service, dir, input)

	service.now = func() time.Time { return time.Now().Add(observationTTL + time.Minute) }
	wantRange(t, service, dir, input)

	evictAll(service)
	write(t, dir, "a.txt", "one\nchanged\nedited\n")
	_, err := readRange(t, service, dir, input)
	wantDiffError(t, err, StaleFile)
}

func TestWorkingTreeLineRangeWithAnotherTargetStaysStaleTarget(t *testing.T) {
	dir, service, _, input := workingTreeAnchorFixture(t)
	evictAll(service)
	input.TargetID = "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	_, err := readRange(t, service, dir, input)
	wantDiffError(t, err, StaleTarget)
	if count := retainedCount(service); count != 0 {
		t.Fatalf("retained observations = %d, want none for a foreign target", count)
	}
}
