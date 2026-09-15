package tui

import "testing"

func TestMutableFileDescriptorIdentityUsesWorkspaceIncarnationAndCanonicalPath(t *testing.T) {
	t.Parallel()
	first := fileWorkspacePane("workspace_a", "internal/tui/app.go")
	same := fileWorkspacePane("workspace_a", "internal/tui/app.go")
	otherWorkspace := fileWorkspacePane("workspace_b", "internal/tui/app.go")
	otherPath := fileWorkspacePane("workspace_a", "internal/tui/shell.go")
	firstID, err := workspacePaneIdentityFor(first)
	if err != nil {
		t.Fatal(err)
	}
	for name, descriptor := range map[string]workspacePaneDescriptor{"same": same, "workspace": otherWorkspace, "path": otherPath} {
		identity, identityErr := workspacePaneIdentityFor(descriptor)
		if identityErr != nil {
			t.Fatalf("%s identity: %v", name, identityErr)
		}
		if name == "same" && identity != firstID {
			t.Fatalf("same resource identity = %q, want %q", identity, firstID)
		}
		if name != "same" && identity == firstID {
			t.Fatalf("%s unexpectedly shared identity %q", name, identity)
		}
	}
}

func TestOpeningSameMutableFileDeduplicatesWithoutReordering(t *testing.T) {
	t.Parallel()
	var controller workspaceController
	first := fileWorkspacePane("workspace_a", "a.go")
	second := fileWorkspacePane("workspace_a", "b.go")
	if _, added, err := controller.Open(first); err != nil || !added {
		t.Fatalf("open first = added:%v err:%v", added, err)
	}
	if _, added, err := controller.Open(second); err != nil || !added {
		t.Fatalf("open second = added:%v err:%v", added, err)
	}
	if _, added, err := controller.Open(first); err != nil || added {
		t.Fatalf("reopen first = added:%v err:%v", added, err)
	}
	panes := controller.Panes()
	if len(panes) != 2 || panes[0].Path != "a.go" || panes[1].Path != "b.go" {
		t.Fatalf("deduplicated pane order = %#v", panes)
	}
	identity, _ := workspacePaneIdentityFor(first)
	if controller.SelectedIdentity() != identity {
		t.Fatalf("selected identity = %q, want %q", controller.SelectedIdentity(), identity)
	}
}

func TestFilePaneDefinitionPreservesWhitespaceInCanonicalPath(t *testing.T) {
	t.Parallel()
	descriptor := fileWorkspacePane("workspace_a", " report.txt ")
	identity, err := workspacePaneIdentityFor(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if identity != workspacePaneIdentity("file:workspace_a: report.txt ") {
		t.Fatalf("whitespace path identity = %q", identity)
	}
}

func TestFilePaneDefinitionRejectsIncompleteOrNoncanonicalIdentity(t *testing.T) {
	t.Parallel()
	for _, descriptor := range []workspacePaneDescriptor{
		fileWorkspacePane("", "a.go"), fileWorkspacePane("workspace_a", ""),
		fileWorkspacePane("workspace_a", "a/../b"), fileWorkspacePane("workspace_a", "/absolute"),
	} {
		if _, err := workspacePaneIdentityFor(descriptor); err == nil {
			t.Fatalf("incomplete file descriptor was accepted: %+v", descriptor)
		}
	}
}
