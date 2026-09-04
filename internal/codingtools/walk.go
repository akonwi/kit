package codingtools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var errStopWalk = errors.New("stop walk")

type walkEntry struct {
	Absolute string
	Relative string
	IsDir    bool
}

type ignoreRule struct {
	base          string
	negated       bool
	directoryOnly bool
	basenameOnly  bool
	pattern       *regexp.Regexp
}

func walkWorkspace(ctx context.Context, root string, visit func(walkEntry) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return visit(walkEntry{Absolute: root, Relative: filepath.Base(root)})
	}

	var walkDirectory func(string, string, []ignoreRule, bool) error
	walkDirectory = func(directory, relativeDirectory string, inherited []ignoreRule, parentIgnored bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		rules := inherited
		if !parentIgnored {
			local, err := readIgnoreRules(ctx, filepath.Join(directory, ".gitignore"), filepath.ToSlash(relativeDirectory))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if len(local) > 0 {
				rules = append(append([]ignoreRule(nil), inherited...), local...)
			}
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.Name() == ".git" && entry.IsDir() {
				continue
			}
			relative := entry.Name()
			if relativeDirectory != "" {
				relative = filepath.Join(relativeDirectory, entry.Name())
			}
			relative = filepath.ToSlash(relative)
			ignored := ignoredPath(relative, entry.IsDir(), parentIgnored, rules)
			item := walkEntry{
				Absolute: filepath.Join(directory, entry.Name()),
				Relative: relative,
				IsDir:    entry.IsDir(),
			}
			if !ignored {
				if err := visit(item); err != nil {
					return err
				}
			}
			if entry.IsDir() && !ignored {
				if err := walkDirectory(item.Absolute, relative, rules, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walkDirectory(root, "", nil, false)
}

func readIgnoreRules(ctx context.Context, filePath, base string) ([]ignoreRule, error) {
	file, err := openRegularFile(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rules []ignoreRule
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		line = strings.TrimRight(line, " ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
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
			continue
		}
		directoryOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		basenameOnly := !anchored && !strings.Contains(line, "/")
		pattern, err := compileGlob(line, false)
		if err != nil {
			pattern = regexp.MustCompile("^" + regexp.QuoteMeta(line) + "$")
		}
		rules = append(rules, ignoreRule{
			base: base, negated: negated, directoryOnly: directoryOnly,
			basenameOnly: basenameOnly, pattern: pattern,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rules, nil
}

func ignoredPath(relative string, isDir, inherited bool, rules []ignoreRule) bool {
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

func compileGlob(pattern string, braceExpansion bool) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(strings.TrimPrefix(pattern, "./"))
	if pattern == "" {
		return nil, fmt.Errorf("glob pattern is empty")
	}
	if len(pattern) > 4096 {
		return nil, fmt.Errorf("glob pattern exceeds 4096 bytes")
	}
	if braceExpansion && strings.Count(pattern, "{") > 16 {
		return nil, fmt.Errorf("glob pattern contains more than 16 brace groups")
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
		expression, err := globExpression(variant)
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
		if len(expanded)+len(variants) > 256 {
			return nil, fmt.Errorf("glob pattern expands to more than 256 variants")
		}
		expanded = append(expanded, variants...)
	}
	return expanded, nil
}

func globExpression(pattern string) (string, error) {
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
