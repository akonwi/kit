// Package highlight defines renderer-neutral semantic syntax highlighting.
package highlight

import (
	"context"
	"path/filepath"
	"strings"
	"unicode"
)

// Role is a theme-independent semantic syntax role.
type Role string

const (
	Text          Role = "text"
	Heading       Role = "heading"
	Bold          Role = "bold"
	Italic        Role = "italic"
	Link          Role = "link"
	List          Role = "list"
	Quote         Role = "quote"
	CodeInline    Role = "codeInline"
	CodeBlock     Role = "codeBlock"
	Strikethrough Role = "strikethrough"
	Comment       Role = "comment"
	String        Role = "string"
	Escape        Role = "escape"
	Number        Role = "number"
	Keyword       Role = "keyword"
	KeywordType   Role = "keywordType"
	Function      Role = "function"
	Operator      Role = "operator"
	Variable      Role = "variable"
	Member        Role = "member"
	Builtin       Role = "builtin"
	Type          Role = "type"
	Punctuation   Role = "punctuation"
	Tag           Role = "tag"
	TagAttribute  Role = "tagAttribute"
	TagDelimiter  Role = "tagDelimiter"
	Attribute     Role = "attribute"
	Label         Role = "label"
)

// Span assigns Role to the half-open byte range [Start, End) in Result.Source.
type Span struct {
	Start int
	End   int
	Role  Role
}

// Request is independent of files and renderers so callers can use it for file
// panes, diff lines, fenced code, and tool activity.
type Request struct {
	Language string
	Path     string
	Source   string
	Revision string
}

// Result always contains safe readable Source. An unavailable language or any
// backend failure returns Fallback with no semantic spans.
type Result struct {
	Source   string
	Language string
	Spans    []Span
	Fallback bool
	Err      error
}

// Highlighter returns semantic spans without resolving theme colors.
type Highlighter interface {
	Highlight(context.Context, Request) Result
}

// Plain returns a sanitized readable fallback.
func Plain(request Request, err error) Result {
	return Result{Source: Sanitize(request.Source), Language: DetectLanguage(request.Language, request.Path), Fallback: true, Err: err}
}

// Sanitize neutralizes terminal controls while preserving newlines and tabs.
func Sanitize(source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\r' || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return '�'
		}
		return r
	}, source)
}

// DetectLanguage normalizes a fence language or infers one from a path.
func DetectLanguage(language, path string) string {
	name := strings.ToLower(strings.TrimSpace(language))
	name = strings.TrimPrefix(name, ".")
	aliases := map[string]string{
		"golang": "go", "js": "javascript", "jsx": "javascript", "ts": "typescript",
		"py": "python", "rs": "rust", "sh": "bash", "shell": "bash", "zsh": "bash",
		"htm": "html", "md": "markdown", "mdx": "markdown", "yml": "yaml",
	}
	if alias := aliases[name]; alias != "" {
		return alias
	}
	if name != "" {
		return name
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if alias := aliases[ext]; alias != "" {
		return alias
	}
	return ext
}
