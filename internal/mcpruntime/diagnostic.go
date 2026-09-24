package mcpruntime

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxDiagnosticBytes bounds a server's stderr tail as rendered into an error.
const maxDiagnosticBytes = 4 << 10

// secretNamePattern matches environment variable names whose values must never
// reach a diagnostic surface.
var secretNamePattern = regexp.MustCompile(`(?i)(KEY|TOKEN|SECRET|PASSWORD|PASSPHRASE|CREDENTIAL|AUTH)`)

// minRedactedValueLength avoids redacting short, low-entropy values such as "1"
// or "on", which would otherwise corrupt unrelated output.
const minRedactedValueLength = 8

// sanitizeDiagnostic prepares untrusted child stderr for a user-visible error.
//
// A stdio server inherits Kit's environment, including provider credentials, so
// any value a server echoes back could otherwise be copied into logs and UI
// error surfaces. Control characters are stripped because the same text reaches
// terminal renderers.
func sanitizeDiagnostic(text string, environment []string) string {
	text = redactSecrets(text, environment)
	text = stripControl(text)
	text = strings.TrimSpace(text)
	if len(text) > maxDiagnosticBytes {
		start := len(text) - maxDiagnosticBytes
		for start < len(text) && !utf8.RuneStart(text[start]) {
			start++
		}
		text = "..." + text[start:]
	}
	return text
}

// redactSecrets replaces occurrences of secret-looking environment values.
func redactSecrets(text string, environment []string) string {
	if text == "" {
		return text
	}
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if !found || len(value) < minRedactedValueLength {
			continue
		}
		if !secretNamePattern.MatchString(name) {
			continue
		}
		if strings.Contains(text, value) {
			text = strings.ReplaceAll(text, value, "[redacted "+name+"]")
		}
	}
	return text
}

// stripControl removes control characters other than newline and tab, which a
// terminal or log surface would otherwise interpret.
func stripControl(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == utf8.RuneError:
			return '\uFFFD'
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, text)
}
