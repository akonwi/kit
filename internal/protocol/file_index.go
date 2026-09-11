package protocol

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
)

const (
	MaxFileIndexEntries = 4000
	MaxFileIndexPathLen = 4096
)

// FileIndexEntry is one project-relative file or directory available to a session.
type FileIndexEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"isDir,omitempty"`
}

// SessionFileIndex is an authoritative file listing from the session host.
type SessionFileIndex struct {
	SessionID string           `json:"sessionId"`
	CWD       string           `json:"cwd"`
	Entries   []FileIndexEntry `json:"entries"`
}

// Validate checks the file index's identity, bounds, and portable relative paths.
func (index SessionFileIndex) Validate() error {
	if !identifier.Valid(index.SessionID, "session_") || !validFileIndexString(index.CWD) || len(index.CWD) > MaxFileIndexPathLen || !filepath.IsAbs(index.CWD) || filepath.Clean(index.CWD) != index.CWD {
		return fmt.Errorf("file index identity is invalid")
	}
	if index.Entries == nil || len(index.Entries) > MaxFileIndexEntries {
		return fmt.Errorf("file index entry count is invalid")
	}
	for position, entry := range index.Entries {
		value := strings.TrimSuffix(entry.Path, "/")
		if value == "" || len(entry.Path) > MaxFileIndexPathLen || !validFileIndexString(entry.Path) || value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "/") || path.Clean(value) != value || strings.Contains(value, "\\") || entry.IsDir != strings.HasSuffix(entry.Path, "/") {
			return fmt.Errorf("file index entry %d path is invalid", position)
		}
	}
	return nil
}

func validFileIndexString(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return false
		}
	}
	return true
}
