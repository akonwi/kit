package tui

import (
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestPastedAttachmentPathsAcceptsEscapedAndQuotedSpaces(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "Application Support")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(directory, "screen shot.png")
	second := filepath.Join(directory, "release notes.txt")
	if err := os.WriteFile(first, []byte("\x89PNG\r\n\x1a\ncontent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(first, " ", "\\ ")

	for name, pasted := range map[string]string{
		"backslash escaped":  escaped,
		"single quoted":      "'" + first + "'",
		"double quoted":      `"` + first + `"`,
		"unescaped and bare": first,
	} {
		paths, composer, ok := pastedAttachmentPaths("", pasted)
		if !ok {
			t.Fatalf("%s path paste was not recognized", name)
		}
		if want := []string{first}; !reflect.DeepEqual(paths, want) {
			t.Fatalf("%s paths = %#v, want %#v", name, paths, want)
		}
		if composer != "" {
			t.Fatalf("%s composer = %q, want empty", name, composer)
		}
	}

	// Terminals report a multi-file drag-and-drop as one space-separated line.
	paths, _, ok := pastedAttachmentPaths("", "'"+first+"' '"+second+"'")
	if !ok {
		t.Fatal("quoted multi-path drag-and-drop was not recognized")
	}
	if want := []string{first, second}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestPastedAttachmentPathsRejectsProseAndUnterminatedQuotes(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "notes.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, pasted := range map[string]string{
		"prose around a path": "look at " + path + " please",
		"unterminated quote":  "'" + path,
		"trailing escape":     path + " \\",
		"plain prose":         "just some words",
	} {
		if _, _, ok := pastedAttachmentPaths("", pasted); ok {
			t.Fatalf("%s was recognized as an attachment paste", name)
		}
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
