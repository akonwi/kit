package fileindex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func paths(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Path)
	}
	return out
}

func TestScanListsDirectoriesThenFilesRespectingGitIgnore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "*.tmp\nbuild/\nsecret.txt\n")
	writeFile(t, root, ".kitignore", "!secret.txt\nprivate/\n")
	writeFile(t, root, "README.md", "")
	writeFile(t, root, "secret.txt", "")
	writeFile(t, root, "scratch.tmp", "")
	writeFile(t, root, "build/out.js", "")
	writeFile(t, root, "private/notes.md", "")
	writeFile(t, root, "node_modules/pkg/index.js", "")
	writeFile(t, root, ".git/HEAD", "")
	writeFile(t, root, "src/main.go", "")
	writeFile(t, root, "src/.gitignore", "gen/\n!keep.tmp\n")
	writeFile(t, root, "src/keep.tmp", "")
	writeFile(t, root, "src/gen/x.go", "")
	writeFile(t, root, "src/sub/.git", "gitdir: ../../.git/modules/sub")
	writeFile(t, root, "src/sub/lib.go", "")

	entries, err := Scan(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"private/",
		"src/",
		"src/sub/",
		".kitignore",
		"README.md",
		"private/notes.md",
		"src/keep.tmp",
		"src/main.go",
		"src/sub/lib.go",
	}
	if got := paths(entries); !reflect.DeepEqual(got, want) {
		t.Fatalf("scan paths = %q, want %q", got, want)
	}
	if !entries[0].IsDir || entries[3].IsDir {
		t.Fatalf("entry kinds = %+v", entries)
	}
}

func TestScanIncludesSymlinkedFilesButNotSymlinkedDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "target.txt", "")
	writeFile(t, outside, "dir/inner.txt", "")
	writeFile(t, root, "real.txt", "")
	if err := os.Symlink(filepath.Join(outside, "target.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "dir"), filepath.Join(root, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling.txt")); err != nil {
		t.Fatal(err)
	}

	entries, err := Scan(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"link.txt", "real.txt"}
	if got := paths(entries); !reflect.DeepEqual(got, want) {
		t.Fatalf("scan paths = %q, want %q", got, want)
	}
}

func TestScanBoundsEntriesInDepthFirstNameOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "a/one.txt", "")
	writeFile(t, root, "a/two.txt", "")
	writeFile(t, root, "b/three.txt", "")
	writeFile(t, root, "zed.txt", "")

	entries, err := Scan(context.Background(), root, Options{MaxEntries: 4})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a/", "b/", "a/one.txt", "a/two.txt"}
	if got := paths(entries); !reflect.DeepEqual(got, want) {
		t.Fatalf("bounded scan paths = %q, want %q", got, want)
	}
}

func TestScanResultReportsTruncationOnlyAfterOneExtraIndexableEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "a.txt", "")
	writeFile(t, root, "b.txt", "")

	exact, err := ScanResult(context.Background(), root, Options{MaxEntries: 2})
	if err != nil || exact.Truncated || len(exact.Entries) != 2 {
		t.Fatalf("exact-bound scan = %+v, %v", exact, err)
	}
	truncated, err := ScanResult(context.Background(), root, Options{MaxEntries: 1})
	if err != nil || !truncated.Truncated || len(truncated.Entries) != 1 || truncated.Entries[0].Path != "a.txt" {
		t.Fatalf("lookahead scan = %+v, %v", truncated, err)
	}
}

func TestScanStopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "a.txt", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, root, Options{}); err == nil {
		t.Fatal("cancelled scan error = nil")
	}
}

func TestScanOfMissingRootReturnsNoEntries(t *testing.T) {
	t.Parallel()
	entries, err := Scan(context.Background(), filepath.Join(t.TempDir(), "missing"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want none", entries)
	}
}
