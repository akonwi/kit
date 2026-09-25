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

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/workspace"
)

type blobEvidence struct {
	data      []byte
	oversized bool
}

func (s *Service) readBlobs(ctx context.Context, r *repository, tree map[string]treeEntry) (map[string]blobEvidence, error) {
	oids := map[string]bool{}
	for _, e := range tree {
		if e.kind == "blob" {
			oids[e.oid] = true
		}
	}
	ordered := make([]string, 0, len(oids))
	for oid := range oids {
		ordered = append(ordered, oid)
	}
	sort.Strings(ordered)
	storageBefore, storageErr := validateObjectStorage(r, ordered)
	if storageErr != nil {
		return nil, storageErr
	}
	var checkInput bytes.Buffer
	for _, oid := range ordered {
		checkInput.WriteString(oid)
		checkInput.WriteByte('\n')
	}
	checked, err := s.runner.run(ctx, r, checkInput.Bytes(), 1<<20, "cat-file", "--batch-check")
	if err != nil {
		return nil, commandFailure(ctx)
	}
	result := map[string]blobEvidence{}
	eligible := make([]string, 0, len(ordered))
	eligibleBytes := 0
	lines := strings.Split(strings.TrimSpace(string(checked)), "\n")
	if len(ordered) == 0 {
		lines = nil
	}
	if len(lines) != len(ordered) {
		return nil, repoUnavailable()
	}
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) != 3 || f[0] != ordered[i] || f[1] != "blob" {
			continue
		}
		var n int
		if _, e := fmt.Sscanf(f[2], "%d", &n); e == nil && n >= 0 {
			if n <= protocol.MaxDiffFileBytes && eligibleBytes+n <= 32<<20 {
				eligible = append(eligible, ordered[i])
				eligibleBytes += n
			} else {
				result[ordered[i]] = blobEvidence{oversized: true}
			}
		}
	}
	var in bytes.Buffer
	for _, oid := range eligible {
		in.WriteString(oid)
		in.WriteByte('\n')
	}
	out, err := s.runner.run(ctx, r, in.Bytes(), 36<<20, "cat-file", "--batch")
	if err != nil {
		return nil, commandFailure(ctx)
	}
	for _, want := range eligible {
		nl := bytes.IndexByte(out, '\n')
		if nl < 0 {
			return nil, repoUnavailable()
		}
		header := strings.Fields(string(out[:nl]))
		out = out[nl+1:]
		if len(header) != 3 || header[0] != want || header[1] != "blob" {
			return nil, repoUnavailable()
		}
		var n int
		if _, e := fmt.Sscanf(header[2], "%d", &n); e != nil || n < 0 || n > protocol.MaxDiffFileBytes || len(out) < n+1 || out[n] != '\n' {
			return nil, repoUnavailable()
		}
		content := out[:n]
		if !matchesObjectOID("blob", content, want) {
			return nil, repoUnavailable()
		}
		result[want] = blobEvidence{data: append([]byte(nil), content...)}
		out = out[n+1:]
	}
	storageAfter, storageErr := validateObjectStorage(r, ordered)
	if storageErr != nil {
		return nil, storageErr
	}
	if storageBefore != storageAfter {
		return nil, errRaced
	}
	return result, nil
}
func sideForTree(e treeEntry, ok bool) protocol.DiffSide {
	if !ok {
		return protocol.DiffSide{Kind: "absent"}
	}
	k := "regular"
	switch e.mode {
	case 0120000:
		k = "symlink"
	case 0160000:
		k = "submodule"
	default:
		if e.kind != "blob" {
			k = "other"
		}
	}
	return protocol.DiffSide{Kind: k, Mode: e.mode}
}
func sideForLive(e workspace.DiffEntry) protocol.DiffSide {
	k := e.Kind
	if k == "directory" {
		k = "other"
	}
	return protocol.DiffSide{Kind: k, Mode: e.Mode}
}
func transformReason(attrs map[string]string, config map[string]string, old []byte) string {
	if v := config["core.autocrlf"]; v != "" && v != "false" && v != "true" && v != "input" {
		return "text_conversion"
	}
	if v := config["core.eol"]; v != "" && v != "lf" && v != "crlf" && v != "native" {
		return "text_conversion"
	}
	if v := attrs["filter"]; v != "" && v != "unset" && v != "unspecified" {
		return "filter"
	}
	if v := attrs["working-tree-encoding"]; v != "" && v != "unset" && v != "unspecified" {
		return "encoding"
	}
	if v := attrs["ident"]; v != "" && v != "unset" && v != "unspecified" {
		return "ident"
	}
	text, eol := attrs["text"], attrs["eol"]
	if text == "unset" && eol != "" {
		return "text_conversion"
	}
	if text == "auto" || eol == "crlf" || text == "set" && (config["core.eol"] == "crlf" || config["core.autocrlf"] == "true") {
		return "text_conversion"
	}
	if (text == "set" || eol == "lf") && bytes.ContainsAny(old, "\r\x00") {
		return "text_conversion"
	}
	if text == "" && eol == "" {
		v := config["core.autocrlf"]
		if v != "" && v != "false" {
			return "text_conversion"
		}
	}
	return ""
}
func (s *Service) classify(c control, live workspace.DiffSnapshot, blobs map[string]blobEvidence) ([]retainedFile, map[string]workspace.DiffEntry, int64, bool, *protocol.DiffTruncation, []protocol.DiffOmission) {
	by := map[string]workspace.DiffEntry{}
	for _, e := range live.Entries {
		by[e.Path] = e
	}
	files := make([]retainedFile, 0)
	complete := true
	var trunc *protocol.DiffTruncation
	om := []protocol.DiffOmission{}
	if c.omitted > 0 {
		om = append(om, protocol.DiffOmission{Reason: "unsupported_path", Count: c.omitted})
		complete = false
	}
	if c.unexamined > 0 {
		om = append(om, protocol.DiffOmission{Reason: "unexamined_candidate", Count: c.unexamined})
		complete = false
		trunc = &protocol.DiffTruncation{Reason: "candidate_limit", Count: c.unexamined}
	}
	var size, admitted int64
	for _, p := range c.candidates {
		old, hasOld := c.tree[p]
		lv := by[p]
		summary := protocol.DiffFileSummary{Path: p, Old: sideForTree(old, hasOld), New: sideForLive(lv), ContentState: "text"}
		idx := c.index[p]
		rf := retainedFile{summary: summary, identity: lv.Identity}
		reason := ""
		exact := true
		if idx.conflict {
			summary.ContentState = "conflict"
			reason = "read_unavailable"
			exact = false
		} else if idx.ita {
			summary.ContentState = "intent_to_add"
			reason = "read_unavailable"
			exact = false
		} else if idx.mode == 0160000 || hasOld && old.mode == 0160000 {
			summary.ContentState = "unsupported_kind"
			reason = "submodule"
			exact = false
			complete = false
		} else if lv.Kind == "directory" {
			summary.ContentState = "unsupported_kind"
			reason = "nested_repository"
			exact = false
			complete = false
		} else if lv.Kind == "other" {
			summary.ContentState = "unsupported_kind"
			reason = "special"
			exact = false
		} else if lv.Unavailable {
			objectOID := lv.GitSHA1
			if c.config["extensions.objectformat"] == "sha256" {
				objectOID = lv.GitSHA256
			}
			if lv.Limit == "file_bytes" && hasOld && old.kind == "blob" && old.oid == objectOID && summary.Old.Kind == summary.New.Kind && summary.Old.Mode == summary.New.Mode {
				continue
			}
			exact, complete = false, false
			if lv.Limit == "read_unavailable" {
				summary.ContentState, reason = "unavailable", "read_unavailable"
			} else {
				summary.ContentState, reason = "too_large", "file_bytes"
			}
			if lv.Limit == "byte_limit" {
				if trunc == nil {
					trunc = &protocol.DiffTruncation{Reason: "byte_limit", Count: 1}
				} else {
					trunc.Count++
				}
			}
		} else if hasOld && old.kind == "blob" {
			b, ok := blobs[old.oid]
			if !ok {
				summary.ContentState = "unavailable"
				reason = "missing_object"
				exact = false
				complete = false
			} else if b.oversized {
				summary.ContentState, reason, exact, complete = "too_large", "file_bytes", false, false
			} else {
				rf.old = b.data
			}
		}
		rf.new = append([]byte(nil), lv.Data...)
		if exact && summary.Old.Kind == summary.New.Kind && summary.Old.Mode == summary.New.Mode && bytes.Equal(rf.old, rf.new) {
			continue
		}
		fileBytes := int64(len(rf.old) + len(rf.new))
		if admitted+fileBytes > 32<<20 && exact {
			summary.ContentState = "too_large"
			reason, exact, complete = "file_bytes", false, false
			if trunc == nil {
				trunc = &protocol.DiffTruncation{Reason: "byte_limit", Count: 1}
			} else {
				trunc.Count++
			}
			rf.old, rf.new = nil, nil
		} else {
			admitted += fileBytes
		}
		clean := by[p]
		clean.Data = nil
		by[p] = clean
		if reason == "" && attrsBinary(c.attrs[p]) {
			summary.ContentState = "binary"
			reason = "attribute_binary"
		}
		if reason == "" {
			if r := transformReason(c.attrs[p], c.config, rf.old); r != "" {
				summary.ContentState = "unsupported_transform"
				reason = r
				exact = false
			}
		}
		if reason == "" && (bytes.IndexByte(rf.old, 0) >= 0 || bytes.IndexByte(rf.new, 0) >= 0) {
			summary.ContentState = "binary"
			reason = "nul"
		}
		if reason == "" && (!utf8.Valid(rf.old) || !utf8.Valid(rf.new)) {
			summary.ContentState = "binary"
			reason = "malformed_utf8"
		}
		if reason == "" && (summary.Old.Kind == "symlink" || summary.New.Kind == "symlink") {
			summary.ContentState = "unsupported_kind"
			reason = "symlink"
		}
		if reason == "" {
			if _, r := splitLines(rf.old); r != "" {
				summary.ContentState, reason, exact, complete = "too_large", r, false, false
			}
			if _, r := splitLines(rf.new); r != "" {
				summary.ContentState, reason, exact, complete = "too_large", r, false, false
			}
		}
		summary.Reason = reason
		changed := false
		if exact {
			od := sha256.Sum256(rf.old)
			nd := sha256.Sum256(rf.new)
			changed = summary.Old.Kind != summary.New.Kind || summary.Old.Mode != summary.New.Mode || od != nd
			if !changed {
				continue
			}
			switch {
			case summary.Old.Kind == "absent":
				summary.Change = "added"
			case summary.New.Kind == "absent":
				summary.Change = "deleted"
			case od == nd && summary.Old.Mode != summary.New.Mode:
				summary.Change = "mode_changed"
			default:
				summary.Change = "modified"
			}
			summary.FileRevision = token("diff_file_", p, summary.Old.Kind, fmt.Sprint(summary.Old.Mode), base64.RawURLEncoding.EncodeToString(od[:]), summary.New.Kind, fmt.Sprint(summary.New.Mode), base64.RawURLEncoding.EncodeToString(nd[:]), "policy-1")
		} else {
			summary.Change = "unknown"
		}
		if summary.ContentState != "text" && summary.Reason == "" {
			summary.Reason = "read_unavailable"
		}
		if summary.ContentState != "text" {
			rf.old, rf.new = nil, nil
		} else {
			size += int64(len(rf.old) + len(rf.new))
		}
		rf.summary = summary
		files = append(files, rf)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].summary.Path < files[j].summary.Path })
	return files, by, size, complete, trunc, om
}
func attrsBinary(a map[string]string) bool { return a["diff"] == "unset" }
func manifestBytes(session, workspaceID, target string, c control, files []retainedFile, root string, complete bool, trunc *protocol.DiffTruncation, om []protocol.DiffOmission) []byte {
	type mf struct {
		Policy, Session, Workspace, Target, Head, Control, Root string
		Complete                                                bool
		Trunc                                                   *protocol.DiffTruncation
		Omissions                                               []protocol.DiffOmission
		Files                                                   []protocol.DiffFileSummary
	}
	x := mf{"diff-policy-1", session, workspaceID, target, c.head, base64.RawURLEncoding.EncodeToString(c.digest[:]), root, complete, trunc, om, make([]protocol.DiffFileSummary, len(files))}
	for i := range files {
		x.Files[i] = files[i].summary
	}
	b, _ := json.Marshal(x)
	return b
}
func pageSize(v int) int {
	if v == 0 {
		return protocol.DefaultDiffFilePageSize
	}
	return v
}
