package toolpresentation

import (
	"encoding/json"
	"path/filepath"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
)

// FileTarget is the file a tool row's summary opens.
type FileTarget struct {
	// Path is the call's path, absolute or relative to the session's working
	// directory; it is empty when the call opens no file.
	Path string
	// Line is the first line a read call asked for, or 0.
	Line int
}

// OpensFile returns the file a read, write, or edit call names, from its
// original arguments. A finished call's absolute path in its details is
// preferred, so a later change of directory doesn't move the target. Only
// reads carry a line: an edit or write doesn't anchor one in the current
// file.
func OpensFile(call Call, result Result) FileTarget {
	kind := strings.ToLower(call.Name)
	if call.ArgumentsTruncated || (kind != "read" && kind != "write" && kind != "edit") {
		return FileTarget{}
	}
	var args struct {
		Path   string `json:"path"`
		Offset *int   `json:"offset"`
	}
	if json.Unmarshal([]byte(call.Arguments), &args) != nil || strings.TrimSpace(args.Path) == "" || strings.ContainsRune(args.Path, 0) {
		return FileTarget{}
	}
	target := FileTarget{Path: args.Path}
	if kind == "read" {
		target.Line = 1
		if args.Offset != nil {
			if *args.Offset < 1 {
				return FileTarget{}
			}
			target.Line = *args.Offset
		}
	}
	var details struct {
		Path string `json:"path"`
	}
	if result.Details != "" && json.Unmarshal([]byte(result.Details), &details) == nil &&
		filepath.IsAbs(details.Path) && !strings.ContainsRune(details.Path, 0) {
		target.Path = details.Path
	}
	return target
}

// WorkspacePath returns path relative to the working directory cwd, as
// workspace files are named, or "" when it lies outside cwd.
func WorkspacePath(cwd, path string) string {
	if cwd == "" || !filepath.IsAbs(cwd) || strings.TrimSpace(path) == "" {
		return ""
	}
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(cwd, absolute)
	}
	relative, err := filepath.Rel(filepath.Clean(cwd), filepath.Clean(absolute))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	canonical := filepath.ToSlash(relative)
	if protocol.ValidateWorkspacePath(canonical, false) != nil {
		return ""
	}
	return canonical
}
