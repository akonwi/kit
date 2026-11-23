package contract

import (
	"strings"
	"testing"
)

func TestHostFilePathsAreAbsoluteOrHome(t *testing.T) {
	for _, path := range []string{"/etc/hosts", "/", "~", "~/notes.md", "/tmp/with space.txt"} {
		if err := ValidateHostFilePath(path, true); err != nil {
			t.Errorf("%q: %v", path, err)
		}
	}
	for _, path := range []string{"", "notes.md", "./a", "~other/a", "/a\nb", "/a\x00b", "/" + strings.Repeat("a", MaxWorkspacePathBytes)} {
		if ValidateHostFilePath(path, true) == nil {
			t.Errorf("%q was accepted", path)
		}
	}
}
