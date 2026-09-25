package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestObserveDiffPathsCachesExactNameChecksAcrossBulkPaths(t *testing.T) {
	root := t.TempDir()
	paths := make([]string, 0, 300)
	for index := range 300 {
		name := fmt.Sprintf("file-%03d.txt", index)
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, name)
	}
	service := NewService()
	ref := service.Ref("session_test", root)
	aggregate := int64(0)
	snapshot, err := service.ObserveDiffPaths(context.Background(), "session_test", root, ref.WorkspaceID, paths, 1024, &aggregate)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != len(paths) {
		t.Fatalf("entries = %d, want %d", len(snapshot.Entries), len(paths))
	}
	for index, entry := range snapshot.Entries {
		if entry.Path != paths[index] || entry.Kind != "regular" || entry.Unavailable {
			t.Fatalf("entry %d = %+v", index, entry)
		}
	}
}
