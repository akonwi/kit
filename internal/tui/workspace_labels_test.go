package tui

import (
	"strings"
	"testing"

	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestWorkspaceFileLabelsDisambiguateDuplicateBasenamesAndIncarnations(t *testing.T) {
	t.Parallel()
	panes := []workspacePaneDescriptor{
		fileWorkspacePane("workspace_aaaaaaaa", "a/main.go"),
		fileWorkspacePane("workspace_aaaaaaaa", "b/main.go"),
		fileWorkspacePane("workspace_bbbbbbbb", "a/main.go"),
	}
	labels := workspacePaneLabels(shellSnapshot{}, panes)
	want := []string{"a/main.go · aaaaaaaa", "b/main.go", "a/main.go · bbbbbbbb"}
	for index := range want {
		if labels[index] != want[index] {
			t.Fatalf("label %d = %q, want %q; all=%#v", index, labels[index], want[index], labels)
		}
	}
}

func TestDuplicateFileLabelsAppearInTabsAndPanePicker(t *testing.T) {
	t.Parallel()
	first := fileWorkspacePane("workspace_aaaaaaaa", "a/main.go")
	second := fileWorkspacePane("workspace_aaaaaaaa", "b/main.go")
	firstID, _ := workspacePaneIdentityFor(first)
	snapshot := shellSnapshot{
		Phase:               phaseReady,
		Workspace:           workspaceControllerSnapshot{Panes: []workspacePaneDescriptor{first, second}, Selected: firstID},
		WorkspacePickerOpen: true, WorkspacePickerScroll: &ui.ScrollController{},
	}
	application := uitest.New(shellView{Snapshot: snapshot})
	application.Pump(100, 24)
	text := strings.Join(paintedRows(application, 100, 24), "\n")
	for _, label := range []string{"a/main.go", "b/main.go"} {
		if strings.Count(text, label) < 2 {
			t.Fatalf("disambiguated label %q missing from tab and picker:\n%s", label, text)
		}
	}
}
