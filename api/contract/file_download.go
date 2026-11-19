package contract

import (
	"fmt"
	"path/filepath"
)

// DownloadFileInput names one file to download (ADR 0074). A request that
// names a workspace downloads a workspace file, and Path is a canonical
// workspace path. A request without one downloads a host file, and Path is
// absolute, "~", or "~/…".
type DownloadFileInput struct {
	// HasWorkspace is whether the request named a workspace, even an empty one.
	HasWorkspace bool
	WorkspaceID  string
	Path         string
}

// Validate checks the request as the kind it names, never as the other.
func (in DownloadFileInput) Validate() error {
	if in.HasWorkspace {
		return ReadWorkspaceFileInput{WorkspaceID: in.WorkspaceID, Path: in.Path}.Validate()
	}
	if in.WorkspaceID != "" {
		return fmt.Errorf("download request is inconsistent")
	}
	return ValidateHostFilePath(in.Path, true)
}

// DownloadedFileInfo is the metadata a download's headers carry.
type DownloadedFileInfo struct {
	SessionID string
	// WorkspaceID is set for a workspace file and empty for a host file.
	WorkspaceID string
	// Path is the canonical workspace path, or the cleaned absolute host path.
	Path string
	// Revision is the file's revision, or empty for a file that reports a
	// size of zero and is served to end of file.
	Revision string
	// Size is the file's size at open, or -1 when the response doesn't report
	// it: always for a file with no Revision, and for a not-modified response.
	Size       int64
	ModifiedAt string
}

// KnownLength reports whether the download has a length and a revision.
func (i DownloadedFileInfo) KnownLength() bool { return i.Revision != "" }

// Validate checks download metadata crossing a transport boundary.
func (i DownloadedFileInfo) Validate() error {
	if i.SessionID == "" || i.ModifiedAt != "" && parseWorkspaceTime(i.ModifiedAt) != nil {
		return fmt.Errorf("downloaded file metadata is invalid")
	}
	if i.WorkspaceID != "" {
		if !validWorkspaceToken(i.WorkspaceID, "workspace_") || ValidateWorkspacePath(i.Path, false) != nil || i.Revision != "" && !validWorkspaceToken(i.Revision, "file_") {
			return fmt.Errorf("downloaded workspace file metadata is invalid")
		}
	} else if ValidateHostFilePath(i.Path, false) != nil || filepath.Clean(i.Path) != i.Path || i.Revision != "" && !validWorkspaceToken(i.Revision, "hostfile_") {
		return fmt.Errorf("downloaded host file metadata is invalid")
	}
	if i.Revision == "" && i.Size != -1 || i.Revision != "" && (i.Size == 0 || i.Size < -1) {
		return fmt.Errorf("downloaded file size is invalid")
	}
	return nil
}

// DownloadConditions are the HTTP conditions of a download request.
type DownloadConditions struct {
	// CachedRevision names a copy the client holds; the response is not
	// modified, with no body, while the file still has that revision.
	CachedRevision string
	// ResumeRevision and ResumeOffset resume an interrupted download: the
	// body starts at ResumeOffset if the file still has ResumeRevision, and
	// is the whole file otherwise.
	ResumeRevision string
	ResumeOffset   int64
}

// Validate checks the conditions before they cross the boundary.
func (c DownloadConditions) Validate() error {
	if c.CachedRevision != "" && !validRevisionToken(c.CachedRevision) || c.ResumeOffset < 0 || (c.ResumeOffset > 0) != (c.ResumeRevision != "") || c.ResumeRevision != "" && !validRevisionToken(c.ResumeRevision) {
		return fmt.Errorf("download conditions are invalid")
	}
	return nil
}

func validRevisionToken(value string) bool {
	return validWorkspaceToken(value, "file_") || validWorkspaceToken(value, "hostfile_")
}

// FileDownloadResponse describes one download response.
type FileDownloadResponse struct {
	Info DownloadedFileInfo
	// NotModified reports that CachedRevision is current; there is no body.
	NotModified bool
	// Offset is where the body starts in the file: ResumeOffset for a
	// resumed download, and zero for the whole file.
	Offset int64
}
