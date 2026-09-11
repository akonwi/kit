// Package ignorefile parses and evaluates gitignore-style rule files.
//
// Rules are scoped to the directory that declared them and evaluated in
// declaration order, so a later rule (including a negation) overrides an
// earlier one. Walkers accumulate rules from the root down and evaluate each
// path against the full chain.
package ignorefile

import (
	"bufio"
	"context"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/akonwi/kit/internal/pathglob"
)

const (
	maxLineBytes = 1 << 20
)

// Rule is one parsed ignore pattern scoped to the directory that declared it.
type Rule struct {
	base          string
	negated       bool
	directoryOnly bool
	basenameOnly  bool
	pattern       *regexp.Regexp
}

// Parse reads ignore rules from reader. base is the slash-separated path of
// the declaring directory relative to the walk root; the root uses "".
func Parse(ctx context.Context, reader io.Reader, base string) ([]Rule, error) {
	var rules []Rule
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxLineBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rule, ok := parseLine(scanner.Text(), base)
		if ok {
			rules = append(rules, rule)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rules, nil
}

func parseLine(line, base string) (Rule, bool) {
	line = strings.TrimSuffix(line, "\r")
	line = strings.TrimRight(line, " ")
	if line == "" || strings.HasPrefix(line, "#") {
		return Rule{}, false
	}
	if strings.HasPrefix(line, `\#`) {
		line = line[1:]
	}
	negated := false
	if strings.HasPrefix(line, "!") {
		negated = true
		line = line[1:]
	} else if strings.HasPrefix(line, `\!`) {
		line = line[1:]
	}
	if line == "" {
		return Rule{}, false
	}
	directoryOnly := strings.HasSuffix(line, "/")
	line = strings.TrimSuffix(line, "/")
	anchored := strings.HasPrefix(line, "/")
	line = strings.TrimPrefix(line, "/")
	if line == "" {
		return Rule{}, false
	}
	basenameOnly := !anchored && !strings.Contains(line, "/")
	pattern, err := pathglob.Compile(line, false)
	if err != nil {
		pattern = regexp.MustCompile("^" + regexp.QuoteMeta(line) + "$")
	}
	return Rule{
		base: base, negated: negated, directoryOnly: directoryOnly,
		basenameOnly: basenameOnly, pattern: pattern,
	}, true
}

// Ignored reports whether the slash-separated path relative to the walk root
// is ignored. inherited is the ignored state of the parent directory, and
// rules are evaluated in order so later rules win.
func Ignored(relative string, isDir, inherited bool, rules []Rule) bool {
	ignored := inherited
	for _, rule := range rules {
		local, ok := relativeToBase(relative, rule.base)
		if !ok || local == "" {
			continue
		}
		candidate := local
		if rule.basenameOnly {
			candidate = path.Base(local)
		}
		if rule.directoryOnly && !isDir {
			continue
		}
		if rule.pattern.MatchString(candidate) {
			ignored = !rule.negated
		}
	}
	return ignored
}

func relativeToBase(relative, base string) (string, bool) {
	if base == "" {
		return relative, true
	}
	if relative == base {
		return "", true
	}
	prefix := base + "/"
	if !strings.HasPrefix(relative, prefix) {
		return "", false
	}
	return strings.TrimPrefix(relative, prefix), true
}
