package toolpresentation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// diffFixtures holds the shared diff fence fixtures: each `.diff` source and,
// in its `.txt`, the kind and text of each line as FencedDiff reads it. The
// macOS client checks its reading against the same records. Regenerate with
// `KIT_UPDATE_DIFF_FIXTURES=1 go test ./ffi/toolpresentation` in `apps/cli`.
const diffFixtures = "../../../../testdata/diffs"

func TestFencedDiffFixtures(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join(diffFixtures, "*.diff"))
	if err != nil || len(sources) == 0 {
		t.Fatalf("no diff fixtures in %s: %v", diffFixtures, err)
	}
	update := os.Getenv("KIT_UPDATE_DIFF_FIXTURES") == "1"
	for _, path := range sources {
		name := strings.TrimSuffix(filepath.Base(path), ".diff")
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got := diffRecord(FencedDiff(string(source)))
			record := strings.TrimSuffix(path, ".diff") + ".txt"
			if update {
				if err := os.WriteFile(record, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("FencedDiff(%s) =\n%s\nwant\n%s", name, got, want)
			}
		})
	}
}

// diffRecord writes one line per source line: its kind, a space, and the
// text shown after any marker.
func diffRecord(blocks []Block) string {
	kinds := map[string]string{
		BlockHeader:  "header",
		BlockHunk:    "hunk",
		BlockCode:    "context",
		BlockRemoved: "removed",
		BlockAdded:   "added",
	}
	var record strings.Builder
	for _, block := range blocks {
		for line := range strings.SplitSeq(block.Text, "\n") {
			record.WriteString(kinds[block.Kind] + " " + line + "\n")
		}
	}
	return record.String()
}

func TestFencedDiffOfNothingIsEmpty(t *testing.T) {
	if got := FencedDiff("\n"); len(got) != 0 {
		t.Errorf("FencedDiff(blank) = %v, want no blocks", got)
	}
}
