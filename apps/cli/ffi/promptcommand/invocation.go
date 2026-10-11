// Package promptcommand parses prompt command invocations typed in the
// composer.
package promptcommand

import (
	"strings"
	"unicode"
)

// Invocation is composer text that may invoke a prompt command: "/<name>"
// followed by optional arguments after whitespace.
type Invocation struct {
	Name      string
	Arguments string
	// OK reports whether the text has the invocation's shape. Whether Name is
	// a discovered command is for the caller to decide.
	OK bool
}

// Parse reads an invocation from composer text, as the vaxis client does.
// Arguments keep their quotes and line breaks; the server parses them.
func Parse(text string) Invocation {
	rest, slash := strings.CutPrefix(text, "/")
	if !slash {
		return Invocation{}
	}
	end := strings.IndexFunc(rest, unicode.IsSpace)
	if end < 0 {
		end = len(rest)
	}
	name := rest[:end]
	if name == "" {
		return Invocation{}
	}
	return Invocation{Name: name, Arguments: strings.TrimLeftFunc(rest[end:], unicode.IsSpace), OK: true}
}
