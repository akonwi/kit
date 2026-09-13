package tui

import (
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPastedAttachmentPathsRecognizesOnlyCompleteSupportedPathPastes(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "notes.txt")
	second := filepath.Join(directory, "image.png")
	if err := os.WriteFile(first, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Content sniffing is intentionally sufficient here; structural image validation
	// remains authoritative at the daemon upload boundary.
	if err := os.WriteFile(second, []byte("\x89PNG\r\n\x1a\ncontent"), 0o600); err != nil {
		t.Fatal(err)
	}

	previous := "before after"
	inserted := first + "\n" + (&url.URL{Scheme: "file", Path: second}).String()
	next := "before " + inserted + "after"
	paths, composer, ok := pastedAttachmentPaths(previous, next)
	if !ok {
		t.Fatal("supported absolute path paste was not recognized")
	}
	if want := []string{first, second}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	if composer != previous {
		t.Fatalf("composer = %q, want %q", composer, previous)
	}

	if _, _, ok := pastedAttachmentPaths("", first+"\nordinary text"); ok {
		t.Fatal("mixed path and ordinary text paste was recognized")
	}
	if _, _, ok := pastedAttachmentPaths("", filepath.Join("relative", "notes.txt")); ok {
		t.Fatal("relative path paste was recognized")
	}
}

func TestAttachmentPathsForComposerChangeRecognizesSplitPaste(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := ""
	for index := 1; index < len(path); index++ {
		next := path[:index]
		if _, _, ok := attachmentPathsForComposerChange(previous, next); ok {
			t.Fatalf("partial path %q was recognized", next)
		}
		previous = next
	}
	paths, composer, ok := attachmentPathsForComposerChange(previous, path)
	if !ok || !reflect.DeepEqual(paths, []string{path}) || composer != "" {
		t.Fatalf("final split update = (%#v, %q, %t)", paths, composer, ok)
	}
}

func TestPastedAttachmentPathsRejectsUnsupportedRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary.dat")
	if err := os.WriteFile(path, []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := pastedAttachmentPaths("", path); ok {
		t.Fatal("unsupported regular file was recognized")
	}
}
