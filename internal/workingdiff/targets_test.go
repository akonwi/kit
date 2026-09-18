package workingdiff

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/workspace"
)

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func targetFixture(t *testing.T) (string, *Service, string) {
	t.Helper()
	dir := fixture(t)
	ws := workspace.NewService()
	service, err := NewService(ws)
	if err != nil {
		t.Fatal(err)
	}
	return dir, service, ws.Ref("session_test", dir).WorkspaceID
}

func findTarget(t *testing.T, catalog protocol.DiffTargetCatalog, kind string, match func(protocol.DiffTargetEntry) bool) protocol.DiffTargetEntry {
	t.Helper()
	for _, target := range catalog.Targets {
		if target.Kind == kind && (match == nil || match(target)) {
			return target
		}
	}
	t.Fatalf("target %s not found in %+v", kind, catalog.Targets)
	return protocol.DiffTargetEntry{}
}

func TestTargetCatalogAndRootCommitObservation(t *testing.T) {
	dir, service, workspaceID := targetFixture(t)
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	if catalog.Targets[0].Kind != protocol.DiffTargetWorkingTree {
		t.Fatalf("first target = %s", catalog.Targets[0].Kind)
	}
	root := findTarget(t, catalog, protocol.DiffTargetCommit, nil)
	if root.Base.Kind != "empty_tree" || root.Head.Kind != "commit" || len(root.Head.OID) != 40 {
		t.Fatalf("root endpoints = %+v -> %+v", root.Base, root.Head)
	}
	page, err := service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: root.Reference})
	if err != nil {
		t.Fatal(err)
	}
	if page.Observation.Target.ID != root.TargetID || page.Observation.Target.Kind != protocol.DiffTargetCommit || len(page.Files) != 1 || page.Files[0].Path != "a.txt" || page.Files[0].Change != "added" {
		t.Fatalf("root observation = %+v", page)
	}
	file, err := service.ReadFile(t.Context(), "session_test", dir, protocol.ReadFileDiffInput{TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "a.txt", ExpectedFileRevision: page.Files[0].FileRevision})
	if err != nil || len(file.Hunks) != 1 || file.Hunks[0].Lines[0].Kind != "addition" {
		t.Fatalf("root diff = %+v, %v", file, err)
	}
}

func TestCommitModificationReadsBothObjectSides(t *testing.T) {
	dir, service, workspaceID := targetFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-qm", "modify existing")
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	target := findTarget(t, catalog, protocol.DiffTargetCommit, func(entry protocol.DiffTargetEntry) bool { return entry.Metadata.Subject == "modify existing" })
	page, err := service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: target.Reference})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Files) != 1 || page.Files[0].Change != "modified" {
		t.Fatalf("files = %+v", page.Files)
	}
}

func TestCommitUsesFirstParentAndIgnoresWorktree(t *testing.T) {
	dir, service, workspaceID := targetFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-qm", "main change")
	git(t, dir, "checkout", "-qb", "side", "HEAD~1")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("side\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "side.txt")
	git(t, dir, "commit", "-qm", "side change")
	git(t, dir, "checkout", "-q", "master")
	git(t, dir, "merge", "--no-ff", "-qm", "merge side", "side")
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	merge := findTarget(t, catalog, protocol.DiffTargetCommit, func(entry protocol.DiffTargetEntry) bool { return entry.Metadata.Subject == "merge side" })
	page, err := service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: merge.Reference})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Files) != 1 || page.Files[0].Path != "side.txt" || page.Files[0].Change != "added" {
		t.Fatalf("first-parent files = %+v", page.Files)
	}
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("dirty worktree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := service.ReadFile(t.Context(), "session_test", dir, protocol.ReadFileDiffInput{TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "side.txt", ExpectedFileRevision: page.Files[0].FileRevision})
	if err != nil || len(read.Hunks) == 0 || read.Hunks[0].Lines[len(read.Hunks[0].Lines)-1].Content != "side" {
		t.Fatalf("committed read = %+v, %v", read, err)
	}
}

func TestBranchPinsMergeBaseAndSurvivesMovingRef(t *testing.T) {
	dir, service, workspaceID := targetFixture(t)
	git(t, dir, "branch", "main")
	git(t, dir, "checkout", "-qb", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "feature.txt")
	git(t, dir, "commit", "-qm", "feature one")
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	branch := findTarget(t, catalog, protocol.DiffTargetBranch, func(entry protocol.DiffTargetEntry) bool { return entry.Metadata.RefName == "feature" })
	if branch.Metadata.BaseRefName != "main" {
		t.Fatalf("base = %q", branch.Metadata.BaseRefName)
	}
	page, err := service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: branch.Reference})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "feature.txt")
	git(t, dir, "commit", "-qm", "feature two")
	read, err := service.ReadFile(t.Context(), "session_test", dir, protocol.ReadFileDiffInput{TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "feature.txt", ExpectedFileRevision: page.Files[0].FileRevision})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, hunk := range read.Hunks {
		for _, line := range hunk.Lines {
			joined += line.Content + "\n"
		}
	}
	if !strings.Contains(joined, "one\n") || strings.Contains(joined, "two\n") {
		t.Fatalf("pinned evidence = %q", joined)
	}
}

func TestUnbornWorkingTreeTargetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := workspace.NewService()
	service, err := NewService(ws)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := ws.Ref("session_test", dir).WorkspaceID
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: catalog.Targets[0].Reference})
	if err != nil || page.Observation.Head.State != "unborn" || len(page.Files) != 1 || page.Files[0].Change != "added" {
		t.Fatalf("unborn observation = %+v, %v", page, err)
	}
}

func TestCatalogDeduplicatesBranchesWithSamePinnedEndpoints(t *testing.T) {
	dir, service, workspaceID := targetFixture(t)
	git(t, dir, "branch", "main")
	git(t, dir, "checkout", "-qb", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "feature.txt")
	git(t, dir, "commit", "-qm", "feature")
	git(t, dir, "branch", "alias")
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	branches := 0
	for _, target := range catalog.Targets {
		if target.Kind == protocol.DiffTargetBranch {
			branches++
		}
	}
	if branches != 1 {
		t.Fatalf("branch targets = %d", branches)
	}
}

func TestCommittedTreeStorageIsValidatedBeforeRead(t *testing.T) {
	dir, service, workspaceID := targetFixture(t)
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	target := findTarget(t, catalog, protocol.DiffTargetCommit, nil)
	treeOID := gitOutput(t, dir, "rev-parse", "HEAD^{tree}")
	objectPath := filepath.Join(dir, ".git", "objects", treeOID[:2], treeOID[2:])
	outside := filepath.Join(t.TempDir(), "tree-object")
	data, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(objectPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, objectPath); err != nil {
		t.Fatal(err)
	}
	_, err = service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: target.Reference})
	var diffErr *Error
	if !errors.As(err, &diffErr) || diffErr.Code != UnsupportedRepository {
		t.Fatalf("error = %v", err)
	}
}

func TestTargetReferencesRejectTamperingCrossSessionAndExpiry(t *testing.T) {
	dir, service, workspaceID := targetFixture(t)
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	target := findTarget(t, catalog, protocol.DiffTargetCommit, nil)
	for name, test := range map[string]struct {
		session, reference string
	}{
		"cross session": {"other_session", target.Reference},
		"tampered":      {"session_test", target.Reference[:len(target.Reference)-1] + map[bool]string{true: "B", false: "A"}[target.Reference[len(target.Reference)-1:] == "A"]},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.ObserveTarget(t.Context(), test.session, dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: test.reference})
			var diffErr *Error
			if !errors.As(err, &diffErr) || diffErr.Code != StaleTarget && diffErr.Code != StaleWorkspace {
				t.Fatalf("error = %v", err)
			}
		})
	}
	service.now = func() time.Time { return time.Now().Add(targetReferenceTTL + time.Minute) }
	_, err = service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: target.Reference})
	var diffErr *Error
	if !errors.As(err, &diffErr) || diffErr.Code != StaleTarget {
		t.Fatalf("expired error = %v", err)
	}
}

func TestCommittedCatalogAndObservationDoNotExecuteHelpers(t *testing.T) {
	dir, service, workspaceID := targetFixture(t)
	marker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join(t.TempDir(), "hostile")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "config", "core.fsmonitor", script)
	git(t, dir, "config", "credential.helper", script)
	git(t, dir, "config", "diff.external", script)
	git(t, dir, "config", "filter.evil.smudge", script)
	git(t, dir, "config", "core.pager", script)
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	target := findTarget(t, catalog, protocol.DiffTargetCommit, nil)
	if _, err := service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: target.Reference}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hostile helper executed: %v", err)
	}
}
