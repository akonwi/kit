package ignorefile

import (
	"context"
	"strings"
	"testing"
)

func parseRules(t *testing.T, base, content string) []Rule {
	t.Helper()
	rules, err := Parse(context.Background(), strings.NewReader(content), base)
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

func TestIgnoredAppliesGitignoreSemantics(t *testing.T) {
	t.Parallel()
	rules := parseRules(t, "", strings.Join([]string{
		"# comment",
		"",
		"*.tmp",
		"build/",
		"/root-only.txt",
		"docs/*.md",
		"!docs/keep.md",
		`\#literal`,
		`\!bang`,
		"trailing.txt   ",
	}, "\n"))
	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{path: "a.tmp", want: true},
		{path: "nested/b.tmp", want: true},
		{path: "build", isDir: true, want: true},
		{path: "build", isDir: false, want: false},
		{path: "root-only.txt", want: true},
		{path: "nested/root-only.txt", want: false},
		{path: "docs/a.md", want: true},
		{path: "docs/keep.md", want: false},
		{path: "#literal", want: true},
		{path: "!bang", want: true},
		{path: "trailing.txt", want: true},
		{path: "kept.go", want: false},
	}
	for _, tc := range cases {
		if got := Ignored(tc.path, tc.isDir, false, rules); got != tc.want {
			t.Errorf("Ignored(%q, dir=%v) = %v, want %v", tc.path, tc.isDir, got, tc.want)
		}
	}
}

func TestIgnoredScopesRulesToDeclaringDirectory(t *testing.T) {
	t.Parallel()
	rules := append(parseRules(t, "", "*.log\n"), parseRules(t, "sub", "!keep.log\n*.txt\n")...)
	cases := []struct {
		path string
		want bool
	}{
		{path: "a.log", want: true},
		{path: "sub/a.log", want: true},
		{path: "sub/keep.log", want: false},
		{path: "sub/a.txt", want: true},
		{path: "other/a.txt", want: false},
		{path: "sub", want: false},
	}
	for _, tc := range cases {
		if got := Ignored(tc.path, false, false, rules); got != tc.want {
			t.Errorf("Ignored(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIgnoredInheritsParentStateUnlessNegated(t *testing.T) {
	t.Parallel()
	rules := parseRules(t, "", "!vendor/keep.go\n")
	if !Ignored("vendor/drop.go", false, true, rules) {
		t.Fatal("inherited ignore should apply")
	}
	if Ignored("vendor/keep.go", false, true, rules) {
		t.Fatal("negation should override inherited ignore")
	}
}
