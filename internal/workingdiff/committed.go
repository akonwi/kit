package workingdiff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	protocol "github.com/akonwi/kit/api/contract"
)

func (s *Service) observeCommitted(ctx context.Context, session, cwd, workspaceID string, ref targetReference, requestedPageSize int) (protocol.DiffPage, error) {
	held, err := s.observeCommittedObservation(ctx, session, cwd, workspaceID, ref)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	return s.listPage(held, 0, pageSize(requestedPageSize), ""), nil
}

func (s *Service) observeCommittedObservation(ctx context.Context, session, cwd, workspaceID string, ref targetReference) (*observation, error) {
	release, err := s.acquire(ctx, session)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	workspace := s.workspaces.Ref(session, cwd)
	if workspace.WorkspaceID != workspaceID {
		return nil, &Error{Code: StaleWorkspace, Message: "the session workspace changed", Details: map[string]string{"currentWorkspaceId": workspace.WorkspaceID}}
	}
	repo, err := s.discoverRepository(ctx, cwd)
	if err != nil {
		return nil, err
	}
	if authorityToken(repo) != ref.Authority {
		return nil, &Error{Code: StaleTarget, Message: "repository authority changed"}
	}
	pinned, err := s.verifyPinnedTarget(ctx, repo, ref)
	if err != nil {
		return nil, err
	}
	baseTree, headTree, omitted, err := s.diffCommittedTrees(ctx, repo, committedBaseTree(ref, pinned), pinned[ref.Head.OID].tree)
	if err != nil {
		return nil, err
	}
	selectedPaths := boundedCommittedPaths(baseTree, headTree)
	blobEntries := make(map[string]treeEntry, len(selectedPaths)*2)
	for _, path := range selectedPaths {
		oldEntry, oldOK := baseTree[path]
		newEntry, newOK := headTree[path]
		if oldOK && newOK && oldEntry == newEntry {
			continue
		}
		if oldOK {
			blobEntries["old:"+path] = oldEntry
		}
		if newOK {
			blobEntries["new:"+path] = newEntry
		}
	}
	blobs, err := s.readBlobs(ctx, repo, blobEntries)
	if err != nil {
		return nil, err
	}
	files, size, complete, truncation, omissions, err := classifyCommitted(baseTree, headTree, blobs, omitted)
	if err != nil {
		return nil, err
	}
	target := protocol.DiffTarget{ID: targetID(session, workspaceID, repo, ref.Kind, ref.Base, ref.Head), WorkspaceID: workspaceID, Kind: ref.Kind, Base: ref.Base, Head: ref.Head}
	manifest, _ := json.Marshal(struct {
		Policy, Session, Workspace string
		Target                     protocol.DiffTarget
		Files                      []protocol.DiffFileSummary
		Complete                   bool
		Truncation                 *protocol.DiffTruncation
		Omissions                  []protocol.DiffOmission
	}{"diff-policy-2", session, workspaceID, target, retainedSummaries(files), complete, truncation, omissions})
	wireObservation := protocol.DiffObservation{SessionID: session, Target: target, Revision: token("diffrev_", string(manifest)), Head: protocol.DiffHead{State: "commit", OID: ref.Head.OID}, IndexSummary: "clean", Complete: complete, Truncation: truncation, Omissions: omissions}
	held := &observation{DiffObservation: wireObservation, repo: repo, cwd: cwd, files: files, committed: true, touched: s.now(), expires: s.now().Add(observationTTL), size: size + int64(len(manifest)) + 4096}
	if held.size > 64<<20 {
		return nil, &Error{Code: CapacityExceeded, Message: "diff observation exceeds cache capacity", Details: map[string]string{"scope": "session"}}
	}
	s.store(held)
	return held, nil
}

func retainedSummaries(files []retainedFile) []protocol.DiffFileSummary {
	result := make([]protocol.DiffFileSummary, len(files))
	for i := range files {
		result[i] = files[i].summary
	}
	return result
}

func (s *Service) verifyPinnedTarget(ctx context.Context, repo *repository, ref targetReference) (map[string]commitInfo, error) {
	oids := []string{ref.Head.OID}
	if ref.Base.Kind == "commit" {
		oids = append(oids, ref.Base.OID)
	}
	storageBefore, err := validateObjectStorage(repo, oids)
	if err != nil {
		return nil, err
	}
	commits := make(map[string]commitInfo, len(oids))
	for _, oid := range oids {
		raw, err := s.runner.run(ctx, repo, nil, 64<<10, "cat-file", "commit", oid)
		if err != nil {
			return nil, commandFailure(ctx)
		}
		if !matchesObjectOID("commit", raw, oid) {
			return nil, &Error{Code: RepositoryUnavailable, Message: "pinned commit content does not match its object id"}
		}
		info, ok := parseCommit(oid, raw)
		if !ok {
			return nil, &Error{Code: RepositoryUnavailable, Message: "pinned commit is malformed"}
		}
		commits[oid] = info
	}
	if ref.Kind == protocol.DiffTargetCommit {
		info := commits[ref.Head.OID]
		if info.parent == "" && ref.Base.Kind != "empty_tree" || info.parent != "" && (ref.Base.Kind != "commit" || ref.Base.OID != info.parent) {
			return nil, &Error{Code: StaleTarget, Message: "commit endpoints do not match"}
		}
	}
	if ref.Kind == protocol.DiffTargetBranch {
		base, err := s.mergeBase(ctx, repo, ref.Base.OID, ref.Head.OID)
		if err != nil {
			return nil, err
		}
		if base != ref.Base.OID {
			return nil, &Error{Code: StaleTarget, Message: "branch endpoints do not match"}
		}
	}
	storageAfter, err := validateObjectStorage(repo, oids)
	if err != nil {
		return nil, err
	}
	if storageBefore != storageAfter {
		return nil, &Error{Code: RepositoryUnavailable, Message: "pinned diff objects changed while they were read"}
	}
	return commits, nil
}

// committedBaseTree returns the base tree OID for a pinned target, or the
// empty string when the target compares against the empty tree.
func committedBaseTree(ref targetReference, commits map[string]commitInfo) string {
	if ref.Base.Kind != "commit" {
		return ""
	}
	return commits[ref.Base.OID].tree
}

// emptyTreeOID returns the well-known empty tree object ID for the
// repository's object format.
func emptyTreeOID(repo *repository) string {
	if repo.config["extensions.objectformat"] == "sha256" {
		return "6ef19b41225c5369f1c104d45d8d85efa9b057b53b14b4b9b939dd74decc5321"
	}
	return "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
}

// diffCommittedTrees compares two committed trees with one git process and
// returns only the entries that differ: base-side entries in oldTree and
// head-side entries in newTree. Unchanged paths appear in neither map, so the
// cost is proportional to the change rather than the repository. An empty
// baseTreeOID compares against the empty tree. Optional paths restrict the
// comparison to those literal paths.
func (s *Service) diffCommittedTrees(ctx context.Context, repo *repository, baseTreeOID, headTreeOID string, paths ...string) (map[string]treeEntry, map[string]treeEntry, int, error) {
	if baseTreeOID == "" {
		baseTreeOID = emptyTreeOID(repo)
	}
	if !validOID(baseTreeOID) || !validOID(headTreeOID) {
		return nil, nil, 0, repoUnavailable()
	}
	storageBefore, err := validateObjectStoreAuthority(repo)
	if err != nil {
		return nil, nil, 0, err
	}
	args := []string{"diff-tree", "-r", "-z", "--raw", "--no-abbrev", "--no-renames", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none", baseTreeOID, headTreeOID}
	if len(paths) > 0 {
		args = append(append(args, "--"), paths...)
	}
	raw, err := s.runner.run(ctx, repo, nil, 16<<20, args...)
	if err != nil {
		return nil, nil, 0, commandFailure(ctx)
	}
	storageAfter, err := validateObjectStoreAuthority(repo)
	if err != nil {
		return nil, nil, 0, err
	}
	if storageBefore != storageAfter {
		return nil, nil, 0, repoUnavailable()
	}
	oldTree, newTree, omitted, ok := parseRawTreeDiff(raw)
	if !ok {
		return nil, nil, 0, repoUnavailable()
	}
	return oldTree, newTree, omitted, nil
}

// parseRawTreeDiff parses `git diff-tree -r -z --raw --no-renames` output.
// Each change is a ":oldmode newmode oldoid newoid status" record followed by
// its path; an all-zero mode marks the absent side of an addition or deletion.
func parseRawTreeDiff(raw []byte) (map[string]treeEntry, map[string]treeEntry, int, bool) {
	oldTree := map[string]treeEntry{}
	newTree := map[string]treeEntry{}
	omitted := 0
	records := splitNUL(raw)
	if len(records)%2 != 0 {
		return nil, nil, 0, false
	}
	for index := 0; index < len(records); index += 2 {
		meta, path := records[index], string(records[index+1])
		if len(meta) == 0 || meta[0] != ':' {
			return nil, nil, 0, false
		}
		fields := strings.Fields(string(meta[1:]))
		if len(fields) != 5 || len(fields[4]) != 1 || !strings.Contains("ADMT", fields[4]) {
			return nil, nil, 0, false
		}
		if protocol.ValidateWorkspacePath(path, false) != nil {
			omitted++
			continue
		}
		oldEntry, oldOK, ok := rawTreeDiffSide(fields[0], fields[2])
		if !ok {
			return nil, nil, 0, false
		}
		newEntry, newOK, ok := rawTreeDiffSide(fields[1], fields[3])
		if !ok || !oldOK && !newOK {
			return nil, nil, 0, false
		}
		if oldOK {
			oldTree[path] = oldEntry
		}
		if newOK {
			newTree[path] = newEntry
		}
	}
	return oldTree, newTree, omitted, true
}

// rawTreeDiffSide converts one side of a raw diff record into a tree entry.
// It reports present=false for the all-zero mode of an absent side.
func rawTreeDiffSide(modeText, oid string) (entry treeEntry, present, ok bool) {
	mode, err := gitMode(modeText)
	if err != nil || !validOID(oid) {
		return treeEntry{}, false, false
	}
	if mode == 0 {
		return treeEntry{}, false, strings.Trim(oid, "0") == ""
	}
	switch mode {
	case 040000:
		return treeEntry{}, false, false
	case 0160000:
		return treeEntry{mode: mode, kind: "commit", oid: oid}, true, true
	default:
		return treeEntry{mode: mode, kind: "blob", oid: oid}, true, true
	}
}

func boundedCommittedPaths(oldTree, newTree map[string]treeEntry) []string {
	paths := make([]string, 0)
	seen := map[string]bool{}
	for path, oldEntry := range oldTree {
		seen[path] = true
		if newEntry, ok := newTree[path]; !ok || newEntry != oldEntry {
			paths = append(paths, path)
		}
	}
	for path := range newTree {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) > protocol.MaxDiffCandidates {
		return paths[:protocol.MaxDiffCandidates]
	}
	return paths
}

func classifyCommitted(oldTree, newTree map[string]treeEntry, blobs map[string]blobEvidence, omitted int) ([]retainedFile, int64, bool, *protocol.DiffTruncation, []protocol.DiffOmission, error) {
	allPathCount := 0
	for path, oldEntry := range oldTree {
		if newEntry, ok := newTree[path]; !ok || newEntry != oldEntry {
			allPathCount++
		}
	}
	for path := range newTree {
		if _, exists := oldTree[path]; !exists {
			allPathCount++
		}
	}
	paths := boundedCommittedPaths(oldTree, newTree)
	complete := omitted == 0
	omissions := []protocol.DiffOmission{}
	if omitted > 0 {
		omissions = append(omissions, protocol.DiffOmission{Reason: "unsupported_path", Count: omitted})
	}
	var truncation *protocol.DiffTruncation
	if allPathCount > protocol.MaxDiffCandidates {
		count := allPathCount - protocol.MaxDiffCandidates
		complete = false
		truncation = &protocol.DiffTruncation{Reason: "candidate_limit", Count: count}
		omissions = append(omissions, protocol.DiffOmission{Reason: "unexamined_candidate", Count: count})
	}
	files := make([]retainedFile, 0)
	var retained int64
	for _, path := range paths {
		oldEntry, oldOK := oldTree[path]
		newEntry, newOK := newTree[path]
		if oldOK && newOK && oldEntry == newEntry {
			continue
		}
		summary := protocol.DiffFileSummary{Path: path, Old: sideForTree(oldEntry, oldOK), New: sideForTree(newEntry, newOK), ContentState: "text"}
		file := retainedFile{summary: summary, oldTree: oldEntry, newTree: newEntry, hasOld: oldOK, hasNew: newOK}
		exact := true
		for _, item := range []struct {
			entry treeEntry
			ok    bool
			dst   *[]byte
		}{{oldEntry, oldOK, &file.old}, {newEntry, newOK, &file.new}} {
			if !item.ok {
				continue
			}
			if item.entry.kind != "blob" || item.entry.mode == 0160000 {
				exact = false
				summary.ContentState, summary.Reason = "unsupported_kind", "submodule"
				complete = false
				continue
			}
			blob, ok := blobs[item.entry.oid]
			if !ok {
				return nil, 0, false, nil, nil, &Error{Code: RepositoryUnavailable, Message: "committed blob is unavailable"}
			}
			if blob.oversized {
				exact = false
				summary.ContentState, summary.Reason = "too_large", "file_bytes"
				complete = false
				continue
			}
			*item.dst = blob.data
		}
		if exact && retained+int64(len(file.old)+len(file.new)) > 32<<20 {
			exact, complete = false, false
			summary.ContentState, summary.Reason = "too_large", "file_bytes"
			if truncation == nil {
				truncation = &protocol.DiffTruncation{Reason: "byte_limit", Count: 1}
			} else {
				truncation.Count++
			}
		}
		if exact && (summary.Old.Kind == "symlink" || summary.New.Kind == "symlink") {
			exact = false
			summary.ContentState, summary.Reason = "unsupported_kind", "symlink"
		}
		if exact && (bytes.IndexByte(file.old, 0) >= 0 || bytes.IndexByte(file.new, 0) >= 0) {
			exact = false
			summary.ContentState, summary.Reason = "binary", "nul"
		}
		if exact && (!utf8.Valid(file.old) || !utf8.Valid(file.new)) {
			exact = false
			summary.ContentState, summary.Reason = "binary", "malformed_utf8"
		}
		if exact {
			if _, reason := splitLines(file.old); reason != "" {
				exact, complete = false, false
				summary.ContentState, summary.Reason = "too_large", reason
			}
			if _, reason := splitLines(file.new); reason != "" {
				exact, complete = false, false
				summary.ContentState, summary.Reason = "too_large", reason
			}
		}
		oldDigest, newDigest := sha256.Sum256(file.old), sha256.Sum256(file.new)
		switch {
		case summary.Old.Kind == "absent":
			summary.Change = "added"
		case summary.New.Kind == "absent":
			summary.Change = "deleted"
		case exact && oldDigest == newDigest && summary.Old.Mode != summary.New.Mode:
			summary.Change = "mode_changed"
		case exact:
			summary.Change = "modified"
		default:
			summary.Change = "unknown"
		}
		if exact {
			summary.FileRevision = token("diff_file_", path, summary.Old.Kind, fmt.Sprint(summary.Old.Mode), base64.RawURLEncoding.EncodeToString(oldDigest[:]), summary.New.Kind, fmt.Sprint(summary.New.Mode), base64.RawURLEncoding.EncodeToString(newDigest[:]), "policy-2")
			retained += int64(len(file.old) + len(file.new))
		} else {
			file.old, file.new = nil, nil
		}
		file.summary = summary
		files = append(files, file)
	}
	return files, retained, complete, truncation, omissions, nil
}

func (s *Service) revalidateCommittedFile(ctx context.Context, repo *repository, ref targetReference, commits map[string]commitInfo, file retainedFile) error {
	oldTree, newTree, _, err := s.diffCommittedTrees(ctx, repo, committedBaseTree(ref, commits), commits[ref.Head.OID].tree, file.summary.Path)
	if err != nil {
		return err
	}
	oldEntry, oldOK := oldTree[file.summary.Path]
	newEntry, newOK := newTree[file.summary.Path]
	if oldOK != file.hasOld || newOK != file.hasNew || oldEntry != file.oldTree || newEntry != file.newTree {
		return &Error{Code: StaleTarget, Message: "committed file evidence changed"}
	}
	entries := map[string]treeEntry{}
	if oldOK {
		entries["old"] = oldEntry
	}
	if newOK {
		entries["new"] = newEntry
	}
	blobs, err := s.readBlobs(ctx, repo, entries)
	if err != nil {
		return err
	}
	for _, side := range []struct {
		entry treeEntry
		ok    bool
		want  []byte
	}{{oldEntry, oldOK, file.old}, {newEntry, newOK, file.new}} {
		if !side.ok || side.entry.kind != "blob" {
			continue
		}
		blob, exists := blobs[side.entry.oid]
		if !exists {
			return &Error{Code: RepositoryUnavailable, Message: "committed blob evidence is unavailable"}
		}
		if file.summary.ContentState == "text" && (blob.oversized || !bytes.Equal(blob.data, side.want)) {
			return &Error{Code: RepositoryUnavailable, Message: "committed blob evidence changed"}
		}
	}
	return nil
}
