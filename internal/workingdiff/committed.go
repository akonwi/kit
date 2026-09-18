package workingdiff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
)

func (s *Service) observeCommitted(ctx context.Context, session, cwd, workspaceID string, ref targetReference, requestedPageSize int) (protocol.DiffPage, error) {
	release, err := s.acquire(ctx, session)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	defer release()
	workspace := s.workspaces.Ref(session, cwd)
	if workspace.WorkspaceID != workspaceID {
		return protocol.DiffPage{}, &Error{Code: StaleWorkspace, Message: "the session workspace changed", Details: map[string]string{"currentWorkspaceId": workspace.WorkspaceID}}
	}
	repo, err := s.discoverRepository(ctx, cwd)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	if authorityToken(repo) != ref.Authority {
		return protocol.DiffPage{}, &Error{Code: StaleTarget, Message: "repository authority changed"}
	}
	if err := s.verifyPinnedTarget(ctx, repo, ref); err != nil {
		return protocol.DiffPage{}, err
	}
	baseTree := map[string]treeEntry{}
	omitted := 0
	if ref.Base.Kind == "commit" {
		baseTree, omitted, err = s.readCommitTree(ctx, repo, ref.Base.OID)
		if err != nil {
			return protocol.DiffPage{}, err
		}
	}
	headTree, headOmitted, err := s.readCommitTree(ctx, repo, ref.Head.OID)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	omitted += headOmitted
	blobEntries := make(map[string]treeEntry, len(baseTree)+len(headTree))
	for path, entry := range baseTree {
		blobEntries["old:"+path] = entry
	}
	for path, entry := range headTree {
		blobEntries["new:"+path] = entry
	}
	blobs, err := s.readBlobs(ctx, repo, blobEntries)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	files, size, complete, truncation, omissions, err := classifyCommitted(baseTree, headTree, blobs, omitted)
	if err != nil {
		return protocol.DiffPage{}, err
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
		return protocol.DiffPage{}, &Error{Code: CapacityExceeded, Message: "diff observation exceeds cache capacity", Details: map[string]string{"scope": "session"}}
	}
	s.store(held)
	return s.listPage(held, 0, pageSize(requestedPageSize), ""), nil
}

func retainedSummaries(files []retainedFile) []protocol.DiffFileSummary {
	result := make([]protocol.DiffFileSummary, len(files))
	for i := range files {
		result[i] = files[i].summary
	}
	return result
}

func (s *Service) verifyPinnedTarget(ctx context.Context, repo *repository, ref targetReference) error {
	oids := []string{ref.Head.OID}
	if ref.Base.Kind == "commit" {
		oids = append(oids, ref.Base.OID)
	}
	storageBefore, err := validateObjectStorage(repo, oids)
	if err != nil {
		return err
	}
	var input bytes.Buffer
	for _, oid := range oids {
		input.WriteString(oid)
		input.WriteByte('\n')
	}
	out, err := s.runner.run(ctx, repo, input.Bytes(), 4096, "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return commandFailure(ctx)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(oids) {
		return &Error{Code: RepositoryUnavailable, Message: "pinned diff objects are unavailable"}
	}
	for i, line := range lines {
		if line != oids[i]+" commit" {
			return &Error{Code: RepositoryUnavailable, Message: "pinned diff object is unavailable"}
		}
	}
	if ref.Kind == protocol.DiffTargetCommit {
		raw, err := s.runner.run(ctx, repo, nil, 64<<10, "cat-file", "commit", ref.Head.OID)
		if err != nil {
			return commandFailure(ctx)
		}
		info, ok := parseCommit(ref.Head.OID, raw)
		if !ok || info.parent == "" && ref.Base.Kind != "empty_tree" || info.parent != "" && (ref.Base.Kind != "commit" || ref.Base.OID != info.parent) {
			return &Error{Code: StaleTarget, Message: "commit endpoints do not match"}
		}
	}
	if ref.Kind == protocol.DiffTargetBranch {
		base, err := s.mergeBase(ctx, repo, ref.Base.OID, ref.Head.OID)
		if err != nil || base != ref.Base.OID {
			return &Error{Code: StaleTarget, Message: "branch endpoints do not match"}
		}
	}
	storageAfter, err := validateObjectStorage(repo, oids)
	if err != nil {
		return err
	}
	if storageBefore != storageAfter {
		return &Error{Code: RepositoryUnavailable, Message: "pinned diff objects changed while they were read"}
	}
	return nil
}

func (s *Service) readCommitTree(ctx context.Context, repo *repository, oid string) (map[string]treeEntry, int, error) {
	raw, err := s.runner.run(ctx, repo, nil, 64<<10, "cat-file", "commit", oid)
	if err != nil {
		return nil, 0, commandFailure(ctx)
	}
	commit, ok := parseCommit(oid, raw)
	if !ok || !validOID(commit.tree) {
		return nil, 0, repoUnavailable()
	}
	result := map[string]treeEntry{}
	omitted := 0
	objects := 0
	var walk func(string, string) error
	walk = func(treeOID, prefix string) error {
		objects++
		if objects > protocol.MaxDiffCandidates*2 {
			return &Error{Code: LimitExceeded, Message: "committed tree exceeds traversal limit", Details: map[string]string{"limit": "candidate_limit"}}
		}
		storageBefore, err := validateObjectStorage(repo, []string{treeOID})
		if err != nil {
			return err
		}
		raw, err := s.runner.run(ctx, repo, nil, 16<<20, "cat-file", "tree", treeOID)
		if err != nil {
			return commandFailure(ctx)
		}
		storageAfter, err := validateObjectStorage(repo, []string{treeOID})
		if err != nil {
			return err
		}
		if storageBefore != storageAfter {
			return repoUnavailable()
		}
		for len(raw) > 0 {
			space := bytes.IndexByte(raw, ' ')
			nul := bytes.IndexByte(raw, 0)
			if space < 1 || nul <= space+1 {
				return repoUnavailable()
			}
			mode, err := gitMode(string(raw[:space]))
			if err != nil {
				return repoUnavailable()
			}
			name := string(raw[space+1 : nul])
			oidBytes := 20
			if repo.config["extensions.objectformat"] == "sha256" {
				oidBytes = 32
			}
			if len(raw) < nul+1+oidBytes {
				return repoUnavailable()
			}
			childOID := hex.EncodeToString(raw[nul+1 : nul+1+oidBytes])
			raw = raw[nul+1+oidBytes:]
			path := name
			if prefix != "" {
				path = prefix + "/" + name
			}
			if protocol.ValidateWorkspacePath(path, false) != nil {
				omitted++
				continue
			}
			if mode == 040000 {
				if err := walk(childOID, path); err != nil {
					return err
				}
				continue
			}
			kind := "blob"
			if mode == 0160000 {
				kind = "commit"
			}
			result[path] = treeEntry{mode: mode, kind: kind, oid: childOID}
		}
		return nil
	}
	if err := walk(commit.tree, ""); err != nil {
		return nil, 0, err
	}
	return result, omitted, nil
}

func classifyCommitted(oldTree, newTree map[string]treeEntry, blobs map[string]blobEvidence, omitted int) ([]retainedFile, int64, bool, *protocol.DiffTruncation, []protocol.DiffOmission, error) {
	paths := make([]string, 0, len(oldTree)+len(newTree))
	seen := map[string]bool{}
	for path := range oldTree {
		seen[path] = true
		paths = append(paths, path)
	}
	for path := range newTree {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	complete := omitted == 0
	omissions := []protocol.DiffOmission{}
	if omitted > 0 {
		omissions = append(omissions, protocol.DiffOmission{Reason: "unsupported_path", Count: omitted})
	}
	var truncation *protocol.DiffTruncation
	if len(paths) > protocol.MaxDiffCandidates {
		count := len(paths) - protocol.MaxDiffCandidates
		paths = paths[:protocol.MaxDiffCandidates]
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
		file := retainedFile{summary: summary}
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
