package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxWorkspacePathBytes        = 4096
	MaxWorkspacePathComponents   = 64
	DefaultDirectoryPageSize     = 100
	MaxDirectoryPageSize         = 200
	MaxDirectoryEntries          = 10_000
	MaxDirectoryResponseBytes    = 512 << 10
	MaxDirectoryObservationBytes = 8 << 20
	MaxWorkspacePreviewBytes     = 1_000_000
	MaxWorkspacePreviewLines     = 5_000
	MaxWorkspaceCursorBytes      = 512
	MaxWorkspaceRevisionBytes    = 128
	MaxWorkspaceActiveRequests   = 4
	MaxWorkspacePendingRequests  = 16
)

// WorkspaceState describes whether a session's published cwd can be explored.
type WorkspaceState string

const (
	WorkspaceReady       WorkspaceState = "ready"
	WorkspaceUnavailable WorkspaceState = "unavailable"
)

// WorkspaceLimits are immutable client-relevant workspace capability bounds.
type WorkspaceLimits struct {
	MaxPathBytes                 int `json:"maxPathBytes"`
	MaxPathComponents            int `json:"maxPathComponents"`
	DefaultDirectoryPageSize     int `json:"defaultDirectoryPageSize"`
	MaxDirectoryPageSize         int `json:"maxDirectoryPageSize"`
	MaxDirectoryEntries          int `json:"maxDirectoryEntries"`
	MaxDirectoryResponseBytes    int `json:"maxDirectoryResponseBytes"`
	MaxDirectoryObservationBytes int `json:"maxDirectoryObservationBytes"`
	MaxPreviewBytes              int `json:"maxPreviewBytes"`
	MaxPreviewLines              int `json:"maxPreviewLines"`
	MaxActiveRequests            int `json:"maxActiveRequests"`
	MaxPendingRequests           int `json:"maxPendingRequests"`
}

// DefaultWorkspaceLimits returns the local daemon's advertised limits.
func DefaultWorkspaceLimits() WorkspaceLimits {
	return WorkspaceLimits{
		MaxPathBytes: MaxWorkspacePathBytes, MaxPathComponents: MaxWorkspacePathComponents,
		DefaultDirectoryPageSize: DefaultDirectoryPageSize, MaxDirectoryPageSize: MaxDirectoryPageSize,
		MaxDirectoryEntries: MaxDirectoryEntries, MaxDirectoryResponseBytes: MaxDirectoryResponseBytes,
		MaxDirectoryObservationBytes: MaxDirectoryObservationBytes,
		MaxPreviewBytes:              MaxWorkspacePreviewBytes, MaxPreviewLines: MaxWorkspacePreviewLines,
		MaxActiveRequests: MaxWorkspaceActiveRequests, MaxPendingRequests: MaxWorkspacePendingRequests,
	}
}

// WorkspaceRef identifies the logical workspace for one bound session.
type WorkspaceRef struct {
	SessionID   string          `json:"sessionId"`
	CWD         string          `json:"cwd"`
	WorkspaceID string          `json:"workspaceId"`
	State       WorkspaceState  `json:"state"`
	Limits      WorkspaceLimits `json:"limits"`
}

// ChangeWorkspaceCWDResult projects a cwd mutation and its new workspace.
type ChangeWorkspaceCWDResult struct {
	Session   SessionInfo  `json:"session"`
	Workspace WorkspaceRef `json:"workspace"`
}

func (r ChangeWorkspaceCWDResult) Validate() error {
	if err := r.Session.Validate(); err != nil {
		return err
	}
	if err := r.Workspace.Validate(); err != nil {
		return err
	}
	if r.Workspace.SessionID != r.Session.ID || r.Workspace.CWD != r.Session.CWD {
		return fmt.Errorf("cwd workspace result is inconsistent")
	}
	return nil
}

// ListDirectoryInput requests one bounded page of immediate children.
type ListDirectoryInput struct {
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path"`
	PageSize    int    `json:"pageSize,omitempty"`
	Cursor      string `json:"cursor,omitempty"`
}

// WorkspaceEntryKind identifies one host directory entry without following it.
type WorkspaceEntryKind string

const (
	WorkspaceEntryFile      WorkspaceEntryKind = "file"
	WorkspaceEntryDirectory WorkspaceEntryKind = "directory"
	WorkspaceEntrySymlink   WorkspaceEntryKind = "symlink"
	WorkspaceEntryOther     WorkspaceEntryKind = "other"
)

// WorkspaceDirectoryEntry is one canonical immediate child.
type WorkspaceDirectoryEntry struct {
	Path         string             `json:"path"`
	Name         string             `json:"name"`
	Kind         WorkspaceEntryKind `json:"kind"`
	Size         *int64             `json:"size,omitempty"`
	ModifiedAt   string             `json:"modifiedAt,omitempty"`
	FileRevision string             `json:"fileRevision,omitempty"`
}

// WorkspaceOmission summarizes entries omitted without exposing unsafe names.
type WorkspaceOmission struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// DirectoryPage is one stable page from a bounded directory observation.
type DirectoryPage struct {
	SessionID        string                    `json:"sessionId"`
	Workspace        WorkspaceRef              `json:"workspace"`
	Path             string                    `json:"path"`
	Revision         string                    `json:"revision"`
	Entries          []WorkspaceDirectoryEntry `json:"entries"`
	NextCursor       string                    `json:"nextCursor,omitempty"`
	Truncated        bool                      `json:"truncated"`
	TruncationReason string                    `json:"truncationReason,omitempty"`
	Omissions        []WorkspaceOmission       `json:"omissions"`
}

// ReadWorkspaceFileInput requests a guarded bounded UTF-8 preview.
type ReadWorkspaceFileInput struct {
	WorkspaceID          string `json:"workspaceId"`
	Path                 string `json:"path"`
	ExpectedFileRevision string `json:"expectedFileRevision,omitempty"`
}

// WorkspaceFileRead is one bounded current-file observation.
type WorkspaceFileRead struct {
	SessionID        string       `json:"sessionId"`
	Workspace        WorkspaceRef `json:"workspace"`
	Path             string       `json:"path"`
	Revision         string       `json:"revision"`
	Size             int64        `json:"size"`
	ModifiedAt       string       `json:"modifiedAt,omitempty"`
	Encoding         string       `json:"encoding"`
	Content          string       `json:"content"`
	ReturnedBytes    int          `json:"returnedBytes"`
	ReturnedLines    int          `json:"returnedLines"`
	Truncated        bool         `json:"truncated"`
	TruncationReason string       `json:"truncationReason,omitempty"`
}

func ValidateWorkspacePath(value string, allowRoot bool) error {
	if value == "" {
		if allowRoot {
			return nil
		}
		return fmt.Errorf("workspace file path is required")
	}
	if len(value) > MaxWorkspacePathBytes || !utf8.ValidString(value) || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "\\") || path.Clean(value) != value {
		return fmt.Errorf("workspace path is not canonical")
	}
	parts := strings.Split(value, "/")
	if len(parts) > MaxWorkspacePathComponents {
		return fmt.Errorf("workspace path has too many components")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("workspace path is not canonical")
		}
		for _, r := range part {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return fmt.Errorf("workspace path is not renderer-safe")
			}
		}
	}
	return nil
}

func (r WorkspaceRef) Validate() error {
	if r.SessionID == "" || !filepath.IsAbs(r.CWD) || filepath.Clean(r.CWD) != r.CWD || !validPathText(r.CWD) || !validWorkspaceToken(r.WorkspaceID, "workspace_") || (r.State != WorkspaceReady && r.State != WorkspaceUnavailable) {
		return fmt.Errorf("workspace reference is invalid")
	}
	if r.Limits.MaxPathBytes != MaxWorkspacePathBytes || r.Limits.MaxPathComponents != MaxWorkspacePathComponents || r.Limits.DefaultDirectoryPageSize <= 0 || r.Limits.MaxDirectoryPageSize < r.Limits.DefaultDirectoryPageSize || r.Limits.MaxDirectoryPageSize > MaxDirectoryPageSize || r.Limits.MaxDirectoryEntries <= 0 || r.Limits.MaxDirectoryEntries > MaxDirectoryEntries || r.Limits.MaxDirectoryResponseBytes <= 0 || r.Limits.MaxDirectoryResponseBytes > MaxDirectoryResponseBytes || r.Limits.MaxDirectoryObservationBytes <= 0 || r.Limits.MaxDirectoryObservationBytes > MaxDirectoryObservationBytes || r.Limits.MaxPreviewBytes <= 0 || r.Limits.MaxPreviewBytes > MaxWorkspacePreviewBytes || r.Limits.MaxPreviewLines <= 0 || r.Limits.MaxPreviewLines > MaxWorkspacePreviewLines || r.Limits.MaxActiveRequests <= 0 || r.Limits.MaxActiveRequests > MaxWorkspaceActiveRequests || r.Limits.MaxPendingRequests < 0 || r.Limits.MaxPendingRequests > MaxWorkspacePendingRequests {
		return fmt.Errorf("workspace limits are invalid")
	}
	return nil
}

func (in ListDirectoryInput) Validate() error {
	if !validWorkspaceToken(in.WorkspaceID, "workspace_") || in.PageSize < 0 || in.PageSize > MaxDirectoryPageSize || len(in.Cursor) > MaxWorkspaceCursorBytes || in.Cursor != "" && !validRendererText(in.Cursor, MaxWorkspaceCursorBytes) {
		return fmt.Errorf("directory request is invalid")
	}
	return ValidateWorkspacePath(in.Path, true)
}

func (in ReadWorkspaceFileInput) Validate() error {
	if !validWorkspaceToken(in.WorkspaceID, "workspace_") || in.ExpectedFileRevision != "" && !validWorkspaceToken(in.ExpectedFileRevision, "file_") {
		return fmt.Errorf("file request is invalid")
	}
	return ValidateWorkspacePath(in.Path, false)
}

func validWorkspaceToken(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) > MaxWorkspaceRevisionBytes {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(raw) == sha256.Size
}

func (p DirectoryPage) Validate() error {
	encoded, marshalErr := json.Marshal(p)
	if err := p.Workspace.Validate(); err != nil || marshalErr != nil || len(encoded) > MaxDirectoryResponseBytes || p.Workspace.State != WorkspaceReady || p.SessionID != p.Workspace.SessionID || !validWorkspaceToken(p.Revision, "directory_") || ValidateWorkspacePath(p.Path, true) != nil || p.Entries == nil || len(p.Entries) > MaxDirectoryPageSize || len(p.NextCursor) > MaxWorkspaceCursorBytes || p.Omissions == nil || len(p.Omissions) > 3 {
		return fmt.Errorf("directory page is invalid")
	}
	if p.Truncated != (p.TruncationReason != "") || (p.TruncationReason != "" && p.TruncationReason != "entry_limit" && p.TruncationReason != "observation_limit") {
		return fmt.Errorf("directory truncation is invalid")
	}
	previous := ""
	for _, e := range p.Entries {
		expected := e.Name
		if p.Path != "" {
			expected = p.Path + "/" + e.Name
		}
		if ValidateWorkspacePath(e.Path, false) != nil || strings.Contains(e.Name, "/") || e.Path != expected || !validRendererText(e.Name, MaxWorkspacePathBytes) || previous >= e.Name && previous != "" || (e.Kind != WorkspaceEntryFile && e.Kind != WorkspaceEntryDirectory && e.Kind != WorkspaceEntrySymlink && e.Kind != WorkspaceEntryOther) || e.Size != nil && *e.Size < 0 || e.ModifiedAt != "" && parseWorkspaceTime(e.ModifiedAt) != nil || e.FileRevision != "" && !validWorkspaceToken(e.FileRevision, "file_") {
			return fmt.Errorf("directory entry is invalid")
		}
		previous = e.Name
	}
	previousOmission := 0
	for _, o := range p.Omissions {
		rank := map[string]int{"unsupported_name": 1, "entry_raced": 2, "metadata_unavailable": 3}[o.Reason]
		if o.Count <= 0 || rank == 0 || rank <= previousOmission {
			return fmt.Errorf("directory omission is invalid")
		}
		previousOmission = rank
	}
	return nil
}

func (r WorkspaceFileRead) Validate() error {
	if err := r.Workspace.Validate(); err != nil || r.Workspace.State != WorkspaceReady || r.SessionID != r.Workspace.SessionID || ValidateWorkspacePath(r.Path, false) != nil || !validWorkspaceToken(r.Revision, "file_") || r.Size < 0 || r.Encoding != "utf-8" || !utf8.ValidString(r.Content) || len(r.Content) != r.ReturnedBytes || workspaceLineCount(r.Content) != r.ReturnedLines || r.ReturnedBytes > MaxWorkspacePreviewBytes || r.ReturnedLines < 0 || r.ReturnedLines > MaxWorkspacePreviewLines || r.ModifiedAt != "" && parseWorkspaceTime(r.ModifiedAt) != nil {
		return fmt.Errorf("workspace file read is invalid")
	}
	if r.Truncated != (r.TruncationReason != "") || (r.TruncationReason != "" && r.TruncationReason != "byte_limit" && r.TruncationReason != "line_limit") {
		return fmt.Errorf("file truncation is invalid")
	}
	return nil
}

func workspaceLineCount(value string) int {
	if value == "" {
		return 0
	}
	count := strings.Count(value, "\n")
	if !strings.HasSuffix(value, "\n") {
		count++
	}
	return count
}

func parseWorkspaceTime(value string) error {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err
}
