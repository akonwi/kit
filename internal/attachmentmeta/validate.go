// Package attachmentmeta validates attachment metadata shared by storage and wire contracts.
package attachmentmeta

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxFilenameBytes = 255

func ValidFilename(filename string) bool {
	if filename == "" || len(filename) > MaxFilenameBytes || !utf8.ValidString(filename) || strings.TrimSpace(filename) != filename ||
		filename == "." || filename == ".." || filepath.IsAbs(filename) || filepath.Base(filename) != filename {
		return false
	}
	for _, character := range filename {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func SupportedMediaType(mediaType string) bool {
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "text/plain":
		return true
	default:
		return false
	}
}
