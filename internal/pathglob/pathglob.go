// Package pathglob compiles shell-style path globs into regular expressions.
//
// Patterns use slash-separated paths. `*` and `?` never cross a slash, `**`
// matches any number of path segments, character classes follow `[...]`
// syntax with `!` negation, and backslash escapes the next character. Brace
// expansion is optional and bounded so untrusted patterns stay cheap.
package pathglob

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxPatternBytes = 4096
	maxBraceGroups  = 16
	maxVariants     = 256
)

// Compile turns pattern into an anchored regular expression. When
// braceExpansion is true, `{a,b}` groups expand into alternatives.
func Compile(pattern string, braceExpansion bool) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(strings.TrimPrefix(pattern, "./"))
	if pattern == "" {
		return nil, fmt.Errorf("glob pattern is empty")
	}
	if len(pattern) > maxPatternBytes {
		return nil, fmt.Errorf("glob pattern exceeds %d bytes", maxPatternBytes)
	}
	if braceExpansion && strings.Count(pattern, "{") > maxBraceGroups {
		return nil, fmt.Errorf("glob pattern contains more than %d brace groups", maxBraceGroups)
	}
	variants := []string{pattern}
	var err error
	if braceExpansion {
		variants, err = expandBraces(pattern)
		if err != nil {
			return nil, err
		}
	}
	expressions := make([]string, 0, len(variants))
	for _, variant := range variants {
		expression, err := expression(variant)
		if err != nil {
			return nil, err
		}
		expressions = append(expressions, expression)
	}
	return regexp.Compile("^(?:" + strings.Join(expressions, "|") + ")$")
}

func expandBraces(pattern string) ([]string, error) {
	start := strings.IndexByte(pattern, '{')
	if start < 0 {
		if strings.ContainsRune(pattern, '}') {
			return nil, fmt.Errorf("invalid glob pattern %q", pattern)
		}
		return []string{pattern}, nil
	}
	end := strings.IndexByte(pattern[start+1:], '}')
	if end < 0 {
		return nil, fmt.Errorf("invalid glob pattern %q", pattern)
	}
	end += start + 1
	choices := strings.Split(pattern[start+1:end], ",")
	if len(choices) < 2 {
		return nil, fmt.Errorf("invalid glob pattern %q", pattern)
	}
	var expanded []string
	for _, choice := range choices {
		if choice == "" {
			return nil, fmt.Errorf("invalid glob pattern %q", pattern)
		}
		variants, err := expandBraces(pattern[:start] + choice + pattern[end+1:])
		if err != nil {
			return nil, err
		}
		if len(expanded)+len(variants) > maxVariants {
			return nil, fmt.Errorf("glob pattern expands to more than %d variants", maxVariants)
		}
		expanded = append(expanded, variants...)
	}
	return expanded, nil
}

func expression(pattern string) (string, error) {
	var expression strings.Builder
	for index := 0; index < len(pattern); {
		character := pattern[index]
		switch character {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index += 2
				if index < len(pattern) && pattern[index] == '/' {
					expression.WriteString("(?:.*/)?")
					index++
				} else {
					expression.WriteString(".*")
				}
				continue
			}
			expression.WriteString("[^/]*")
			index++
		case '?':
			expression.WriteString("[^/]")
			index++
		case '[':
			end := index + 1
			if end < len(pattern) && (pattern[end] == '!' || pattern[end] == '^') {
				end++
			}
			if end < len(pattern) && pattern[end] == ']' {
				end++
			}
			for end < len(pattern) && pattern[end] != ']' {
				end++
			}
			if end >= len(pattern) {
				return "", fmt.Errorf("invalid glob pattern %q", pattern)
			}
			class := pattern[index+1 : end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			expression.WriteByte('[')
			expression.WriteString(class)
			expression.WriteByte(']')
			index = end + 1
		case '\\':
			if index+1 < len(pattern) {
				expression.WriteString(regexp.QuoteMeta(pattern[index+1 : index+2]))
				index += 2
			} else {
				expression.WriteString(`\\`)
				index++
			}
		default:
			expression.WriteString(regexp.QuoteMeta(pattern[index : index+1]))
			index++
		}
	}
	return expression.String(), nil
}
