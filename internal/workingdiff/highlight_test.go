package workingdiff

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/workspace"
)

// Each side is highlighted from its whole file, so context lines inside a
// block comment that opens above the page are colored as comment, and each
// page carries colors for its own lines only.
func TestReadFileHighlightsEachPageFromTheWholeFiles(t *testing.T) {
	d := t.TempDir()
	git(t, d, "init", "-q")
	git(t, d, "config", "user.name", "Test")
	git(t, d, "config", "user.email", "test@example.com")
	base := "package main\n\n/*\nfirst\nsecond\nthird\nfourth\nfifth\n*/\nvar x = 1\n"
	if err := os.WriteFile(filepath.Join(d, "main.go"), []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, d, "add", "main.go")
	git(t, d, "commit", "-qm", "base")
	changed := "package main\n\n/*\nfirst\nsecond\nthird\nfourth\nfifth\n*/\nvar y = \"😀\"\n"
	if err := os.WriteFile(filepath.Join(d, "main.go"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaces := workspace.NewService()
	service, err := NewService(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := service.Observe(t.Context(), "session_test", d, protocol.ObserveWorkingTreeInput{WorkspaceID: workspaces.Ref("session_test", d).WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.ReadFileDiffInput{TargetID: observed.Observation.Target.ID, TargetRevision: observed.Observation.Revision, Path: "main.go", PageSize: 2}

	plain, err := service.ReadFile(t.Context(), "session_test", d, input)
	if err != nil || plain.Highlight != nil {
		t.Fatalf("unrequested ReadFile() highlight = %+v, %v", plain.Highlight, err)
	}

	input.Highlight = true
	first, err := service.ReadFile(t.Context(), "session_test", d, input)
	if err != nil {
		t.Fatal(err)
	}
	comment := func(length int) []int { return []int{0, length, 0} }
	want := &protocol.FileDiffHighlight{
		Old: &protocol.Highlight{Language: "go", Lines: [][]int{comment(6), comment(5)}, Roles: []string{"comment"}},
		New: &protocol.Highlight{Language: "go", Lines: [][]int{comment(6), comment(5)}, Roles: []string{"comment"}},
	}
	if !reflect.DeepEqual(first.Highlight, want) {
		t.Fatalf("first page highlight = %s, want %s", describe(first.Highlight), describe(want))
	}

	input.Cursor = first.NextCursor
	second, err := service.ReadFile(t.Context(), "session_test", d, input)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, hunk := range second.Hunks {
		for _, line := range hunk.Lines {
			kinds = append(kinds, line.Kind+":"+line.Content)
		}
	}
	if want := []string{"context:*/", "deletion:var x = 1"}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("second page lines = %q, want %q", kinds, want)
	}
	want = &protocol.FileDiffHighlight{
		Old: &protocol.Highlight{Language: "go", Lines: [][]int{{0, 2, 0}, {0, 3, 1, 4, 1, 2, 6, 1, 3, 8, 1, 4}}, Roles: []string{"comment", "keyword", "variable", "operator", "number"}},
		New: &protocol.Highlight{Language: "go", Lines: [][]int{{0, 2, 0}}, Roles: []string{"comment"}},
	}
	if !reflect.DeepEqual(second.Highlight, want) {
		t.Fatalf("second page highlight = %s, want %s", describe(second.Highlight), describe(want))
	}

	// The last page has only an addition, so it has no old side. The
	// string's length counts "😀" as two UTF-16 code units.
	input.Cursor = second.NextCursor
	third, err := service.ReadFile(t.Context(), "session_test", d, input)
	if err != nil {
		t.Fatal(err)
	}
	want = &protocol.FileDiffHighlight{
		New: &protocol.Highlight{Language: "go", Lines: [][]int{{0, 3, 0, 4, 1, 1, 6, 1, 2, 8, 4, 3}}, Roles: []string{"keyword", "variable", "operator", "string"}},
	}
	if !reflect.DeepEqual(third.Highlight, want) {
		t.Fatalf("third page highlight = %s, want %s", describe(third.Highlight), describe(want))
	}
	for _, page := range []protocol.FileDiffPage{first, second, third} {
		if err := page.Validate(); err != nil {
			t.Fatalf("page is invalid: %v", err)
		}
	}
}

func describe(h *protocol.FileDiffHighlight) string {
	if h == nil {
		return "<nil>"
	}
	return fmt.Sprintf("old=%+v new=%+v", h.Old, h.New)
}
