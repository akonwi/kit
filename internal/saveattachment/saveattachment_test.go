package saveattachment

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/attachment"
)

type fixture struct {
	store  *attachment.Filesystem
	home   string
	cwd    string
	opts   Options
	record attachment.Record
	data   []byte
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 4, 3))
	picture.Set(1, 1, color.RGBA{R: 200, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "kit-home")
	store, err := attachment.NewFilesystem(filepath.Join(home, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	data := testPNG(t)
	input, err := attachment.InspectImage("browser__screenshot.png", bytes.NewReader(data), attachment.ImageLimits{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	input.SessionID = "session_1"
	record, err := store.Put(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "repo")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	opts := Options{SessionID: "session_1", CWD: func() string { return cwd }, Store: store, Protected: []string{home}}
	if _, err := New(opts); err != nil {
		t.Fatal(err)
	}
	return fixture{store: store, home: home, cwd: cwd, opts: opts, record: record, data: data}
}

func (f fixture) run(t *testing.T, args arguments) droids.ToolResult {
	t.Helper()
	result, err := execute(context.Background(), f.opts, args)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func resultText(t *testing.T, result droids.ToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v, want one text block", result.Content)
	}
	text, ok := result.Content[0].(droids.TextContent)
	if !ok {
		t.Fatalf("content = %#v, want text", result.Content)
	}
	return text.Text
}

func TestSaveAttachmentCopiesTheBytesToARelativePath(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	result := f.run(t, arguments{Attachment: f.record.ID, Path: "docs/images/screenshot.png"})
	want := filepath.Join(f.cwd, "docs", "images", "screenshot.png")
	if result.IsError || resultText(t, result) != "Saved "+f.record.ID+" (image/png, "+formatSize(f.record.Size)+") to "+want {
		t.Fatalf("result = %#v", result)
	}
	saved, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, f.data) {
		t.Fatal("saved bytes differ from the attachment")
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", info.Mode().Perm())
	}
	var details Details
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details != (Details{AttachmentID: f.record.ID, Path: want, MediaType: "image/png", Size: f.record.Size}) {
		t.Fatalf("details = %#v", details)
	}
	entries, err := os.ReadDir(filepath.Dir(want))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "screenshot.png" {
		t.Fatalf("directory = %v, want only the saved file", entries)
	}
}

func TestSaveAttachmentRequiresOverwriteToReplaceAFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	path := filepath.Join(f.cwd, "shot.png")
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := f.run(t, arguments{Attachment: f.record.ID, Path: path})
	if !result.IsError || resultText(t, result) != "Error: path already exists; pass overwrite to replace it" {
		t.Fatalf("result = %#v", result)
	}
	if saved, _ := os.ReadFile(path); string(saved) != "existing" {
		t.Fatalf("file = %q, want it untouched", saved)
	}
	result = f.run(t, arguments{Attachment: f.record.ID, Path: path, Overwrite: true})
	if result.IsError {
		t.Fatalf("result = %#v", result)
	}
	if saved, _ := os.ReadFile(path); !bytes.Equal(saved, f.data) {
		t.Fatal("overwrite did not replace the file")
	}
}

func TestSaveAttachmentRefusesSymbolicLinksAndDirectories(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	target := filepath.Join(f.cwd, "target.txt")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.cwd, "link.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, overwrite := range []bool{false, true} {
		result := f.run(t, arguments{Attachment: f.record.ID, Path: link, Overwrite: overwrite})
		if !result.IsError || resultText(t, result) != "Error: path is a symbolic link" {
			t.Fatalf("overwrite %v: result = %#v", overwrite, result)
		}
	}
	if saved, _ := os.ReadFile(target); string(saved) != "target" {
		t.Fatalf("link target = %q, want it untouched", saved)
	}
	result := f.run(t, arguments{Attachment: f.record.ID, Path: f.cwd, Overwrite: true})
	if !result.IsError || resultText(t, result) != "Error: path exists and is not a regular file" {
		t.Fatalf("directory: result = %#v", result)
	}
}

func TestSaveAttachmentRefusesTheKitHome(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	want := "Error: refusing to write inside " + f.home + ", which Kit manages"
	result := f.run(t, arguments{Attachment: f.record.ID, Path: filepath.Join(f.home, "auth.json"), Overwrite: true})
	if !result.IsError || resultText(t, result) != want {
		t.Fatalf("direct: result = %#v", result)
	}
	// A link in the cwd cannot redirect the write into the home either.
	if err := os.Symlink(f.home, filepath.Join(f.cwd, "home")); err != nil {
		t.Fatal(err)
	}
	result = f.run(t, arguments{Attachment: f.record.ID, Path: "home/new/shot.png"})
	if !result.IsError || resultText(t, result) != want {
		t.Fatalf("through a link: result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(f.home, "new")); !os.IsNotExist(err) {
		t.Fatalf("the refused write created a directory in the home: %v", err)
	}
}

func TestSaveAttachmentOnlyReadsTheSessionsAttachments(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := f
	other.opts.SessionID = "session_2"
	result := other.run(t, arguments{Attachment: f.record.ID, Path: "shot.png"})
	if !result.IsError || resultText(t, result) != "Error: attachment "+f.record.ID+" is not available in this session" {
		t.Fatalf("result = %#v", result)
	}
	result = f.run(t, arguments{Attachment: "../manifest.json", Path: "shot.png"})
	if !result.IsError || resultText(t, result) != `Error: "../manifest.json" is not an attachment ID` {
		t.Fatalf("invalid ID: result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(f.cwd, "shot.png")); !os.IsNotExist(err) {
		t.Fatalf("a refused save wrote a file: %v", err)
	}
}

func TestReferenceNamesTheAttachmentForTheModel(t *testing.T) {
	t.Parallel()
	record := attachment.Record{ID: "attachment_0123456789abcdef0123456789abcdef", MediaType: "image/png", Size: 319_488, Width: 1600, Height: 1000}
	want := "Attachment attachment_0123456789abcdef0123456789abcdef: image/png, 1600×1000, 312 KiB. save_attachment copies it to a file."
	if got := Reference(record); got != want {
		t.Fatalf("Reference = %q, want %q", got, want)
	}
	record.Width, record.Height, record.MediaType, record.Size = 0, 0, "text/plain", 12
	want = "Attachment attachment_0123456789abcdef0123456789abcdef: text/plain, 12 bytes. save_attachment copies it to a file."
	if got := Reference(record); got != want {
		t.Fatalf("Reference = %q, want %q", got, want)
	}
}
