package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	DefaultDiffFilePageSize = 100
	MaxDiffFilePageSize     = 200
	DefaultDiffLinePageSize = 500
	MaxDiffLinePageSize     = 1000
	DefaultDiffHunkPageSize = 10
	MaxDiffHunkPageSize     = 20
	MaxDiffResponseBytes    = 512 << 10
	MaxDiffCursorBytes      = 512
	MaxDiffCandidates       = 4000
	MaxDiffFileBytes        = 1 << 20
	MaxDiffFileLines        = 5000
	MaxDiffLineBytes        = 64 << 10
)

type DiffTarget struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspaceId"`
	Kind           string `json:"kind"`
	RepositoryPath string `json:"repositoryPath"`
}
type DiffHead struct {
	State string `json:"state"`
	OID   string `json:"oid,omitempty"`
}
type DiffTruncation struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}
type DiffOmission struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}
type DiffObservation struct {
	SessionID    string          `json:"sessionId"`
	Target       DiffTarget      `json:"target"`
	Revision     string          `json:"revision"`
	Head         DiffHead        `json:"head"`
	IndexSummary string          `json:"indexSummary"`
	Complete     bool            `json:"complete"`
	Truncation   *DiffTruncation `json:"truncation,omitempty"`
	Omissions    []DiffOmission  `json:"omissions"`
}
type DiffSide struct {
	Kind string `json:"kind"`
	Mode uint32 `json:"mode"`
}
type DiffFileSummary struct {
	Path         string   `json:"path"`
	FileRevision string   `json:"fileRevision,omitempty"`
	Change       string   `json:"change"`
	Old          DiffSide `json:"old"`
	New          DiffSide `json:"new"`
	ContentState string   `json:"contentState"`
	Reason       string   `json:"reason,omitempty"`
	Additions    *int     `json:"additions,omitempty"`
	Deletions    *int     `json:"deletions,omitempty"`
}
type ObserveWorkingTreeInput struct {
	WorkspaceID string `json:"workspaceId"`
	PageSize    int    `json:"pageSize,omitempty"`
	Cursor      string `json:"cursor,omitempty"`
}
type WorkingTreePage struct {
	Observation DiffObservation   `json:"observation"`
	Files       []DiffFileSummary `json:"files"`
	NextCursor  string            `json:"nextCursor,omitempty"`
}
type ReadFileDiffInput struct {
	TargetID             string `json:"targetId"`
	TargetRevision       string `json:"targetRevision"`
	Path                 string `json:"path"`
	ExpectedFileRevision string `json:"expectedFileRevision,omitempty"`
	PageSize             int    `json:"pageSize,omitempty"`
	MaxHunks             int    `json:"maxHunks,omitempty"`
	Cursor               string `json:"cursor,omitempty"`
}
type DiffComputation struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}
type DiffLine struct {
	Kind             string `json:"kind"`
	OldLine          *int   `json:"oldLine,omitempty"`
	NewLine          *int   `json:"newLine,omitempty"`
	Content          string `json:"content"`
	HasTerminatingLF bool   `json:"hasTerminatingLF"`
}
type DiffHunk struct {
	OldStart        int        `json:"oldStart"`
	OldCount        int        `json:"oldCount"`
	NewStart        int        `json:"newStart"`
	NewCount        int        `json:"newCount"`
	ContinuedBefore bool       `json:"continuedBefore"`
	ContinuedAfter  bool       `json:"continuedAfter"`
	Lines           []DiffLine `json:"lines"`
}
type FileDiffPage struct {
	Observation DiffObservation `json:"observation"`
	File        DiffFileSummary `json:"file"`
	Computation DiffComputation `json:"computation"`
	Hunks       []DiffHunk      `json:"hunks"`
	NextCursor  string          `json:"nextCursor,omitempty"`
}

func validHexOID(v string) bool {
	if len(v) != 40 && len(v) != 64 {
		return false
	}
	for _, r := range v {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func validDiffToken(v, prefix string) bool {
	if len(v) > 128 || len(v) <= len(prefix) || v[:len(prefix)] != prefix {
		return false
	}
	b, e := base64.RawURLEncoding.DecodeString(v[len(prefix):])
	return e == nil && len(b) == sha256.Size
}
func (in ObserveWorkingTreeInput) Validate() error {
	if !validWorkspaceToken(in.WorkspaceID, "workspace_") || in.PageSize < 0 || in.PageSize > MaxDiffFilePageSize || len(in.Cursor) > MaxDiffCursorBytes {
		return fmt.Errorf("working-tree observation request is invalid")
	}
	return nil
}
func (in ReadFileDiffInput) Validate() error {
	if !validDiffToken(in.TargetID, "difftarget_") || !validDiffToken(in.TargetRevision, "diffrev_") || in.ExpectedFileRevision != "" && !validDiffToken(in.ExpectedFileRevision, "diff_file_") || ValidateWorkspacePath(in.Path, false) != nil || in.PageSize < 0 || in.PageSize > MaxDiffLinePageSize || in.MaxHunks < 0 || in.MaxHunks > MaxDiffHunkPageSize || len(in.Cursor) > MaxDiffCursorBytes {
		return fmt.Errorf("file diff request is invalid")
	}
	return nil
}
func (o DiffObservation) Validate() error {
	if o.SessionID == "" || !validDiffToken(o.Target.ID, "difftarget_") || !validWorkspaceToken(o.Target.WorkspaceID, "workspace_") || o.Target.Kind != "working_tree" || o.Target.RepositoryPath != "" || !validDiffToken(o.Revision, "diffrev_") || (o.Head.State != "commit" && o.Head.State != "unborn") || (o.Head.State == "commit" && !validHexOID(o.Head.OID)) || (o.Head.State == "unborn" && o.Head.OID != "") || (o.IndexSummary != "clean" && o.IndexSummary != "diverged" && o.IndexSummary != "conflicted") || o.Omissions == nil {
		return fmt.Errorf("diff observation is invalid")
	}
	if o.Complete && (o.Truncation != nil || len(o.Omissions) != 0) {
		return fmt.Errorf("diff completeness is invalid")
	}
	if o.Truncation != nil {
		switch o.Truncation.Reason {
		case "candidate_limit", "byte_limit", "observation_limit", "deadline":
		default:
			return fmt.Errorf("diff truncation is invalid")
		}
		if o.Truncation.Count < 1 {
			return fmt.Errorf("diff truncation count is invalid")
		}
	}
	previous := 0
	for _, x := range o.Omissions {
		rank := map[string]int{"unsupported_path": 1, "unexamined_candidate": 2}[x.Reason]
		if rank <= previous || x.Count < 1 {
			return fmt.Errorf("diff omission is invalid")
		}
		previous = rank
		if (x.Reason != "unsupported_path" && x.Reason != "unexamined_candidate") || x.Count < 1 {
			return fmt.Errorf("diff omission is invalid")
		}
	}
	return nil
}
func (f DiffFileSummary) Validate() error {
	if ValidateWorkspacePath(f.Path, false) != nil || f.FileRevision != "" && !validDiffToken(f.FileRevision, "diff_file_") {
		return fmt.Errorf("diff file is invalid")
	}
	switch f.Change {
	case "added", "deleted", "modified", "mode_changed", "unknown":
	default:
		return fmt.Errorf("diff change is invalid")
	}
	for _, s := range []DiffSide{f.Old, f.New} {
		switch s.Kind {
		case "absent":
			if s.Mode != 0 {
				return fmt.Errorf("diff side is invalid")
			}
		case "regular":
			if s.Mode != 0100644 && s.Mode != 0100755 {
				return fmt.Errorf("diff side is invalid")
			}
		case "symlink":
			if s.Mode != 0120000 {
				return fmt.Errorf("diff side is invalid")
			}
		case "submodule":
			if s.Mode != 0160000 {
				return fmt.Errorf("diff side is invalid")
			}
		case "other":
		default:
			return fmt.Errorf("diff side is invalid")
		}
	}
	states := map[string]bool{"text": true, "binary": true, "conflict": true, "intent_to_add": true, "unsupported_transform": true, "unsupported_kind": true, "too_large": true, "unavailable": true}
	if !states[f.ContentState] {
		return fmt.Errorf("diff content state is invalid")
	}
	reasons := map[string]map[string]bool{
		"text": {"": true}, "binary": {"nul": true, "malformed_utf8": true, "attribute_binary": true},
		"conflict": {"read_unavailable": true}, "intent_to_add": {"read_unavailable": true},
		"unsupported_transform": {"filter": true, "encoding": true, "ident": true, "text_conversion": true},
		"unsupported_kind":      {"symlink": true, "submodule": true, "special": true, "nested_repository": true},
		"too_large":             {"file_bytes": true, "line_count": true, "line_bytes": true},
		"unavailable":           {"missing_object": true, "read_unavailable": true},
	}
	if !reasons[f.ContentState][f.Reason] {
		return fmt.Errorf("diff reason is invalid")
	}
	if f.Additions != nil && *f.Additions < 0 || f.Deletions != nil && *f.Deletions < 0 {
		return fmt.Errorf("diff counts are invalid")
	}
	return nil
}
func (p WorkingTreePage) Validate() error {
	if p.Observation.Validate() != nil || p.Files == nil || len(p.Files) > MaxDiffFilePageSize || len(p.NextCursor) > MaxDiffCursorBytes {
		return fmt.Errorf("working-tree page is invalid")
	}
	prev := ""
	for _, f := range p.Files {
		if f.Validate() != nil || prev != "" && prev >= f.Path {
			return fmt.Errorf("working-tree files are invalid")
		}
		prev = f.Path
	}
	b, _ := json.Marshal(p)
	if len(b) > MaxDiffResponseBytes {
		return fmt.Errorf("working-tree response is too large")
	}
	return nil
}
func (p FileDiffPage) Validate() error {
	if p.Observation.Validate() != nil || p.File.Validate() != nil || p.Hunks == nil || len(p.Hunks) > MaxDiffHunkPageSize || len(p.NextCursor) > MaxDiffCursorBytes || (p.Computation.State != "complete" && p.Computation.State != "too_complex") || (p.Computation.State == "complete" && p.Computation.Reason != "") || (p.Computation.State == "too_complex" && !map[string]bool{"diff_work": true, "diff_memory": true, "edit_limit": true, "hunk_limit": true}[p.Computation.Reason]) {
		return fmt.Errorf("file diff page is invalid")
	}
	lines := 0
	for _, h := range p.Hunks {
		if h.OldStart < 0 || h.NewStart < 0 || h.OldCount < 0 || h.NewCount < 0 || h.OldCount > 0 && h.OldStart < 1 || h.NewCount > 0 && h.NewStart < 1 {
			return fmt.Errorf("diff hunk is invalid")
		}
		for _, l := range h.Lines {
			lines++
			if l.OldLine != nil && *l.OldLine < 1 || l.NewLine != nil && *l.NewLine < 1 {
				return fmt.Errorf("diff line coordinates are invalid")
			}
			if !utf8.ValidString(l.Content) {
				return fmt.Errorf("diff line is invalid")
			}
			switch l.Kind {
			case "context":
				if l.OldLine == nil || l.NewLine == nil {
					return fmt.Errorf("diff line coordinates are invalid")
				}
			case "deletion":
				if l.OldLine == nil || l.NewLine != nil {
					return fmt.Errorf("diff line coordinates are invalid")
				}
			case "addition":
				if l.OldLine != nil || l.NewLine == nil {
					return fmt.Errorf("diff line coordinates are invalid")
				}
			default:
				return fmt.Errorf("diff line kind is invalid")
			}
		}
	}
	if lines > MaxDiffLinePageSize {
		return fmt.Errorf("diff page has too many lines")
	}
	b, _ := json.Marshal(p)
	if len(b) > MaxDiffResponseBytes {
		return fmt.Errorf("diff response is too large")
	}
	return nil
}
