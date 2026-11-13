package workingdiff

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/workspace"
)

// commitFile writes name with content in dir and commits it.
func commitFile(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "-qm", message)
}

// remoteFixture returns an upstream repository on main and a clone of it
// whose remote is named remote, with a service over the clone.
func remoteFixture(t *testing.T, remote string) (upstream, clone string, service *Service, workspaceID string) {
	t.Helper()
	upstream = fixture(t)
	git(t, upstream, "branch", "-M", "main")
	clone = filepath.Join(t.TempDir(), "clone")
	git(t, upstream, "clone", "-q", "-o", remote, upstream, clone)
	git(t, clone, "config", "user.name", "Test")
	git(t, clone, "config", "user.email", "test@example.com")
	ws := workspace.NewService()
	service, err := NewService(ws)
	if err != nil {
		t.Fatal(err)
	}
	return upstream, clone, service, ws.Ref("session_test", clone).WorkspaceID
}

func listTargets(t *testing.T, service *Service, dir, workspaceID string) protocol.DiffTargetCatalog {
	t.Helper()
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	return catalog
}

// branchTargets returns the catalog's branch targets in catalog order.
func branchTargets(catalog protocol.DiffTargetCatalog) []protocol.DiffTargetEntry {
	var result []protocol.DiffTargetEntry
	for _, target := range catalog.Targets {
		if target.Kind == protocol.DiffTargetBranch {
			result = append(result, target)
		}
	}
	return result
}

// targetPaths observes target and returns its changed paths.
func targetPaths(t *testing.T, service *Service, dir, workspaceID string, target protocol.DiffTargetEntry) []string {
	t.Helper()
	page, err := service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: target.Reference, ExpectedTargetID: target.TargetID})
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(page.Files))
	for i, file := range page.Files {
		paths[i] = file.Path
	}
	return paths
}

// assertDefaultBranchTarget checks the first branch target compares current
// with baseRef at the merge base mergeBase, and changes exactly paths.
func assertDefaultBranchTarget(t *testing.T, service *Service, dir, workspaceID string, catalog protocol.DiffTargetCatalog, current, baseRef, mergeBase string, paths ...string) protocol.DiffTargetEntry {
	t.Helper()
	targets := branchTargets(catalog)
	if len(targets) == 0 {
		t.Fatalf("no branch targets in %+v", catalog.Targets)
	}
	target := targets[0]
	want := protocol.DiffTargetMetadata{Label: current + " vs " + baseRef, RefName: current, BaseRefName: baseRef, Abbreviated: abbreviate(gitOutput(t, dir, "rev-parse", "HEAD"))}
	if target.Metadata != want {
		t.Fatalf("default branch metadata = %+v, want %+v", target.Metadata, want)
	}
	if target.Base != (protocol.DiffEndpoint{Kind: "commit", OID: mergeBase}) {
		t.Fatalf("default branch base = %+v, want merge base %s", target.Base, mergeBase)
	}
	got := targetPaths(t, service, dir, workspaceID, target)
	if len(got) != len(paths) {
		t.Fatalf("default branch paths = %v, want %v", got, paths)
	}
	for i := range paths {
		if got[i] != paths[i] {
			t.Fatalf("default branch paths = %v, want %v", got, paths)
		}
	}
	return target
}

func TestBranchTargetUsesRemoteBaseWhenLocalBaseIsBehind(t *testing.T) {
	upstream, clone, service, workspaceID := remoteFixture(t, "origin")
	commitFile(t, upstream, "upstream.txt", "upstream\n", "upstream change")
	git(t, clone, "fetch", "-q", "origin")
	git(t, clone, "checkout", "-qb", "feature", "--no-track", "origin/main")
	commitFile(t, clone, "feature.txt", "feature\n", "feature change")

	catalog := listTargets(t, service, clone, workspaceID)
	assertDefaultBranchTarget(t, service, clone, workspaceID, catalog, "feature", "origin/main", gitOutput(t, clone, "rev-parse", "origin/main"), "feature.txt")
	if len(branchTargets(catalog)) != 1 {
		t.Fatalf("branch targets = %+v", branchTargets(catalog))
	}
}

func TestBranchTargetUsesLocalBaseWithoutRemote(t *testing.T) {
	dir, service, workspaceID := targetFixture(t)
	git(t, dir, "branch", "-M", "main")
	git(t, dir, "checkout", "-qb", "feature")
	commitFile(t, dir, "feature.txt", "feature\n", "feature change")

	catalog := listTargets(t, service, dir, workspaceID)
	assertDefaultBranchTarget(t, service, dir, workspaceID, catalog, "feature", "main", gitOutput(t, dir, "rev-parse", "main"), "feature.txt")
}

func TestBranchTargetOffersRemoteOnlyDefaultBase(t *testing.T) {
	_, clone, service, workspaceID := remoteFixture(t, "origin")
	git(t, clone, "checkout", "-qb", "feature")
	git(t, clone, "branch", "-D", "main")
	commitFile(t, clone, "feature.txt", "feature\n", "feature change")

	catalog := listTargets(t, service, clone, workspaceID)
	assertDefaultBranchTarget(t, service, clone, workspaceID, catalog, "feature", "origin/main", gitOutput(t, clone, "rev-parse", "origin/main"), "feature.txt")
}

func TestBranchTargetUsesLocalBaseWhenRemoteBaseIsSameCommit(t *testing.T) {
	_, clone, service, workspaceID := remoteFixture(t, "origin")
	git(t, clone, "checkout", "-qb", "feature", "--no-track", "main")
	commitFile(t, clone, "feature.txt", "feature\n", "feature change")

	catalog := listTargets(t, service, clone, workspaceID)
	assertDefaultBranchTarget(t, service, clone, workspaceID, catalog, "feature", "main", gitOutput(t, clone, "rev-parse", "main"), "feature.txt")
}

func TestBranchTargetOffersRemoteOnlyBaseOnSlashNamedRemote(t *testing.T) {
	_, clone, service, workspaceID := remoteFixture(t, "company/upstream")
	git(t, clone, "checkout", "-qb", "feature", "--track", "company/upstream/main")
	git(t, clone, "branch", "-D", "main")
	commitFile(t, clone, "feature.txt", "feature\n", "feature change")

	catalog := listTargets(t, service, clone, workspaceID)
	assertDefaultBranchTarget(t, service, clone, workspaceID, catalog, "feature", "company/upstream/main", gitOutput(t, clone, "rev-parse", "company/upstream/main"), "feature.txt")
}

func TestBranchTargetPrefersCloserMergeBaseWhenLocalBaseIsAheadOrDiverged(t *testing.T) {
	cases := []struct {
		name string
		// upstreamAhead adds a fetched commit on origin/main.
		upstreamAhead bool
		// from is where the feature branch starts.
		from, wantBase string
	}{
		{name: "ahead, branched from local", from: "main", wantBase: "main"},
		{name: "ahead, branched from origin", from: "origin/main", wantBase: "main"},
		{name: "diverged, branched from local", upstreamAhead: true, from: "main", wantBase: "main"},
		{name: "diverged, branched from origin", upstreamAhead: true, from: "origin/main", wantBase: "origin/main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream, clone, service, workspaceID := remoteFixture(t, "origin")
			commitFile(t, clone, "local.txt", "local\n", "unpushed local change")
			if tc.upstreamAhead {
				commitFile(t, upstream, "upstream.txt", "upstream\n", "upstream change")
				git(t, clone, "fetch", "-q", "origin")
			}
			git(t, clone, "checkout", "-qb", "feature", "--no-track", tc.from)
			commitFile(t, clone, "feature.txt", "feature\n", "feature change")

			catalog := listTargets(t, service, clone, workspaceID)
			mergeBase := gitOutput(t, clone, "merge-base", tc.wantBase, "HEAD")
			assertDefaultBranchTarget(t, service, clone, workspaceID, catalog, "feature", tc.wantBase, mergeBase, "feature.txt")
		})
	}
}

func TestBranchTargetUsesConfiguredUpstreamOnAnotherRemote(t *testing.T) {
	upstream, clone, service, workspaceID := remoteFixture(t, "upstream")
	// origin is a stale fork with a HEAD, so it is the default remote.
	fork := filepath.Join(t.TempDir(), "fork")
	git(t, upstream, "clone", "-q", "--bare", upstream, fork)
	git(t, clone, "remote", "add", "origin", fork)
	git(t, clone, "fetch", "-q", "origin")
	git(t, clone, "remote", "set-head", "origin", "main")
	commitFile(t, upstream, "upstream.txt", "upstream\n", "upstream change")
	git(t, clone, "fetch", "-q", "upstream")
	git(t, clone, "checkout", "-qb", "feature", "--no-track", "upstream/main")
	commitFile(t, clone, "feature.txt", "feature\n", "feature change")

	catalog := listTargets(t, service, clone, workspaceID)
	assertDefaultBranchTarget(t, service, clone, workspaceID, catalog, "feature", "upstream/main", gitOutput(t, clone, "rev-parse", "upstream/main"), "feature.txt")
}

func TestBranchTargetReferenceSurvivesFetchAfterCatalog(t *testing.T) {
	upstream, clone, service, workspaceID := remoteFixture(t, "origin")
	commitFile(t, upstream, "upstream.txt", "upstream\n", "upstream change")
	git(t, clone, "fetch", "-q", "origin")
	git(t, clone, "checkout", "-qb", "feature", "--no-track", "origin/main")
	commitFile(t, clone, "feature.txt", "feature\n", "feature change")
	mergeBase := gitOutput(t, clone, "rev-parse", "origin/main")
	pinned := assertDefaultBranchTarget(t, service, clone, workspaceID, listTargets(t, service, clone, workspaceID), "feature", "origin/main", mergeBase, "feature.txt")

	commitFile(t, upstream, "later.txt", "later\n", "later upstream change")
	git(t, clone, "fetch", "-q", "origin")

	if got := targetPaths(t, service, clone, workspaceID, pinned); len(got) != 1 || got[0] != "feature.txt" {
		t.Fatalf("pinned paths after fetch = %v, want [feature.txt]", got)
	}
	again := assertDefaultBranchTarget(t, service, clone, workspaceID, listTargets(t, service, clone, workspaceID), "feature", "origin/main", mergeBase, "feature.txt")
	if again.TargetID != pinned.TargetID {
		t.Fatalf("target after fetch = %s, want %s", again.TargetID, pinned.TargetID)
	}
}

func TestCatalogSnapshotDetectsFetchWhileListing(t *testing.T) {
	upstream, clone, service, _ := remoteFixture(t, "origin")
	commitFile(t, upstream, "upstream.txt", "upstream\n", "upstream change")
	git(t, clone, "fetch", "-q", "origin")
	git(t, clone, "checkout", "-qb", "feature", "--no-track", "origin/main")
	commitFile(t, clone, "feature.txt", "feature\n", "feature change")
	repo, err := service.discoverRepository(t.Context(), clone)
	if err != nil {
		t.Fatal(err)
	}
	head := gitOutput(t, clone, "rev-parse", "HEAD")
	refs, _, err := service.listRefs(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.verifyCatalogSnapshot(t.Context(), repo, head, false, refs); err != nil {
		t.Fatalf("unchanged snapshot error = %v", err)
	}

	commitFile(t, upstream, "later.txt", "later\n", "later upstream change")
	git(t, clone, "fetch", "-q", "origin")

	err = service.verifyCatalogSnapshot(t.Context(), repo, head, false, refs)
	var diffErr *Error
	if !errors.As(err, &diffErr) || diffErr.Code != StaleTarget {
		t.Fatalf("snapshot error after fetch = %v, want %s", err, StaleTarget)
	}
}
