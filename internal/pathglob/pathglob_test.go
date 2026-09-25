package pathglob

import (
	"strings"
	"testing"
)

func TestCompileMatchesSlashSeparatedPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern string
		braces  bool
		path    string
		want    bool
	}{
		{pattern: "*.go", path: "main.go", want: true},
		{pattern: "*.go", path: "src/main.go", want: false},
		{pattern: "**/*.go", path: "src/main.go", want: true},
		{pattern: "**/*.go", path: "main.go", want: true},
		{pattern: "src/**", path: "src/a/b.go", want: true},
		{pattern: "?.txt", path: "a.txt", want: true},
		{pattern: "?.txt", path: "ab.txt", want: false},
		{pattern: "[!a].txt", path: "b.txt", want: true},
		{pattern: "[!a].txt", path: "a.txt", want: false},
		{pattern: `\*.txt`, path: "*.txt", want: true},
		{pattern: `\*.txt`, path: "a.txt", want: false},
		{pattern: "./main.go", path: "main.go", want: true},
		{pattern: "*.{go,md}", braces: true, path: "README.md", want: true},
		{pattern: "*.{go,md}", braces: true, path: "main.rs", want: false},
		{pattern: "*.{go,md}", braces: false, path: "*.{go,md}", want: true},
	}
	for _, tc := range cases {
		matcher, err := Compile(tc.pattern, tc.braces)
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", tc.pattern, err)
		}
		if got := matcher.MatchString(tc.path); got != tc.want {
			t.Errorf("Compile(%q).MatchString(%q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestCompileRejectsInvalidAndUnboundedPatterns(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"", "[abc", "a}", "{a}", "{a,}", strings.Repeat("{a,b}", 9), strings.Repeat("{a,b}", 17)} {
		if _, err := Compile(pattern, true); err == nil {
			t.Errorf("Compile(%q) error = nil", pattern)
		}
	}
}
