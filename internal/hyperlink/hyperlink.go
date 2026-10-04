// Package hyperlink validates external targets before exposing them to terminal
// hyperlink metadata or platform URL openers.
package hyperlink

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SafeExternal returns target when it is a validated HTTP, HTTPS, or mailto
// destination suitable for external navigation. It returns an empty string for
// unsupported, malformed, or terminal-unsafe targets.
func SafeExternal(target string) string {
	if target == "" || len(target) > 4096 || !utf8.ValidString(target) || target != strings.TrimSpace(target) {
		return ""
	}
	for _, character := range target {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) ||
			unicode.Is(unicode.Zl, character) || unicode.Is(unicode.Zp, character) {
			return ""
		}
	}
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.User != nil {
		return ""
	}
	switch parsed.Scheme {
	case "http", "https":
		if parsed.Host == "" {
			return ""
		}
	case "mailto":
		if parsed.Opaque == "" {
			return ""
		}
	default:
		return ""
	}
	return target
}
