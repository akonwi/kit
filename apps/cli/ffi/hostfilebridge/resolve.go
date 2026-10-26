package hostfilebridge

import (
	"path/filepath"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/apps/cli/ffi/toolpresentation"
)

// Resolve decides where path opens for a session in cwd (ADR 0050): a path
// in the working directory opens its workspace file, any other absolute or
// "~/" path its host file. A relative path is resolved against cwd. The
// zero Target means path names nothing that opens.
func Resolve(cwd, path string) Target {
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, 0) {
		return Target{}
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		return Target{Host: path}
	}
	absolute := path
	if !filepath.IsAbs(absolute) {
		if cwd == "" || !filepath.IsAbs(cwd) {
			return Target{}
		}
		absolute = filepath.Join(cwd, absolute)
	}
	absolute = filepath.Clean(absolute)
	if relative := toolpresentation.WorkspacePath(cwd, absolute); relative != "" {
		return Target{Workspace: relative}
	}
	if protocol.ValidateHostFilePath(absolute, false) != nil {
		return Target{}
	}
	return Target{Host: absolute}
}

// Typed returns the path in text typed or pasted into the file picker,
// without surrounding whitespace or quotes or shell escapes, or "" when it is
// empty.
func Typed(text string) string {
	value := strings.TrimSpace(text)
	if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	if strings.Contains(value, `\`) {
		var unescaped strings.Builder
		escaping := false
		for _, character := range value {
			if character == '\\' && !escaping {
				escaping = true
				continue
			}
			escaping = false
			unescaped.WriteRune(character)
		}
		value = unescaped.String()
	}
	if strings.ContainsAny(value, "\x00\n") {
		return ""
	}
	return value
}

// IsPath reports whether text typed into the file picker is a path to open
// rather than a query: absolute or home-relative.
func IsPath(text string) bool {
	value := Typed(text)
	return value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "/")
}
