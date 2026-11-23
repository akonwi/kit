package contract

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateHostFilePath checks a requested host path: absolute, or "~" or
// "~/…" when home is allowed, renderer-safe, and within the path bound.
func ValidateHostFilePath(value string, allowHome bool) error {
	if len(value) > MaxWorkspacePathBytes {
		return fmt.Errorf("host file path is too long")
	}
	if !validPathText(value) {
		return fmt.Errorf("host file path is invalid")
	}
	if allowHome && (value == "~" || strings.HasPrefix(value, "~/")) {
		return nil
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("host file path must be absolute")
	}
	return nil
}
