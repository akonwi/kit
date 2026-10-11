package hyperlink

import (
	"strings"
	"testing"
)

func TestSafeExternalAllowsOnlyNavigablePublicSchemes(t *testing.T) {
	t.Parallel()

	for _, target := range []string{
		"https://example.test/docs",
		"http://example.test/docs",
		"mailto:hello@example.test",
	} {
		if got := SafeExternal(target); got != target {
			t.Errorf("SafeExternal(%q) = %q", target, got)
		}
	}
	for _, target := range []string{
		"file:///tmp/private",
		"javascript:alert(1)",
		"https://user@example.test/docs",
		" https://example.test/docs",
		"https://example.test/\u009bunsafe",
		"https://example.test/\u202eunsafe",
		"https://example.test/\x9c\x9b2J",
		"not a URL",
		strings.Repeat("x", 4097),
	} {
		if got := SafeExternal(target); got != "" {
			t.Errorf("SafeExternal(%q) = %q, want empty", target, got)
		}
	}
}
