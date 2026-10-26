package workspace

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

func hostPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func hostReadError(t *testing.T, service *Service, path string) *Error {
	t.Helper()
	_, done, err := service.ReadHostFile(t.Context(), "session_test", protocol.ReadHostFileInput{Path: path})
	done()
	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("read %q error = %v, want a workspace error", path, err)
	}
	if err := typed.Validate(); err != nil {
		t.Fatalf("read %q error %+v is not a valid wire error: %v", path, typed, err)
	}
	return typed
}

func TestReadHostFileReadsTextOutsideAnyWorkspace(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(name, []byte("# Notes\nline two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	read, done, err := NewService().ReadHostFile(t.Context(), "session_test", protocol.ReadHostFileInput{Path: name})
	defer done()
	if err != nil {
		t.Fatal(err)
	}
	text := read.Text()
	if text == nil || text.SessionID != "session_test" || text.Path != name || text.Content != "# Notes\nline two\n" ||
		text.Size != 17 || text.ReturnedLines != 2 || text.Truncated || text.ModifiedAt == "" {
		t.Fatalf("read = %+v", read)
	}
	if err := read.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestReadHostFileExpandsTheServerHome(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("export A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	service.home = func() (string, error) { return home, nil }
	read, done, err := service.ReadHostFile(t.Context(), "session_test", protocol.ReadHostFileInput{Path: "~/.profile"})
	defer done()
	if err != nil || read.Path() != filepath.Join(home, ".profile") || read.Text().Content != "export A=1\n" {
		t.Fatalf("read = %+v, %v", read, err)
	}
	service.home = func() (string, error) { return "", errors.New("no home") }
	if got := hostReadError(t, service, "~/.profile"); got.Code != Unavailable {
		t.Fatalf("unknown home = %+v", got)
	}
}

func TestReadHostFileCleansPathsAndFollowsSymlinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("linked"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	read, done, err := NewService().ReadHostFile(t.Context(), "session_test", protocol.ReadHostFileInput{Path: root + "/sub/../link.txt"})
	defer done()
	if err != nil || read.Path() != link || read.Text().Content != "linked" {
		t.Fatalf("read = %+v, %v", read, err)
	}
}

func TestReadHostFileReturnsCompleteImages(t *testing.T) {
	t.Parallel()
	data := hostPNG(t, 3, 2)
	name := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
	read, done, err := NewService().ReadHostFile(t.Context(), "session_test", protocol.ReadHostFileInput{Path: name})
	defer done()
	if err != nil {
		t.Fatal(err)
	}
	picture := read.Image()
	if picture == nil || picture.MediaType != "image/png" || picture.Width != 3 || picture.Height != 2 ||
		!bytes.Equal(picture.Data, data) || picture.Size != int64(len(data)) || picture.Path != name {
		t.Fatalf("read = %+v", read)
	}
	if err := read.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestReadHostFileRejectsWhatItCannotShow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := NewService()
	write := func(name string, data []byte) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if got := hostReadError(t, service, write("archive.bin", []byte{0x7f, 'E', 'L', 'F', 0, 1})); got.Code != BinaryFile ||
		got.Message != "only text files and PNG, JPEG, GIF, or WebP images can be shown" {
		t.Fatalf("binary = %+v", got)
	}
	damaged := hostPNG(t, 2, 2)
	if got := hostReadError(t, service, write("damaged.png", damaged[:len(damaged)-8])); got.Code != BinaryFile ||
		got.Message != "image is damaged or not a supported format" {
		t.Fatalf("damaged image = %+v", got)
	}
	if got := hostReadError(t, service, write("huge.png", hostPNG(t, 6000, 4001))); got.Code != LimitExceeded || got.Details["limit"] != "image_pixels" {
		t.Fatalf("huge image = %+v", got)
	}
	oversized := append(hostPNG(t, 1, 1), make([]byte, protocol.MaxHostFileImageBytes)...)
	if got := hostReadError(t, service, write("oversized.png", oversized)); got.Code != LimitExceeded || got.Details["limit"] != "image_bytes" {
		t.Fatalf("oversized image = %+v", got)
	}
	if got := hostReadError(t, service, root); got.Code != NotFile {
		t.Fatalf("directory = %+v", got)
	}
	if got := hostReadError(t, service, filepath.Join(root, "missing.txt")); got.Code != NotFound {
		t.Fatalf("missing = %+v", got)
	}
	for _, path := range []string{"relative.txt", "/a\nb"} {
		if got := hostReadError(t, service, path); got.Code != InvalidPath {
			t.Fatalf("%q = %+v", path, got)
		}
	}
	if got := hostReadError(t, service, "/"+strings.Repeat("a", protocol.MaxWorkspacePathBytes)); got.Code != LimitExceeded || got.Details["limit"] != "path_bytes" {
		t.Fatalf("long path = %+v", got)
	}
}

func TestReadHostFileTruncatesLargeText(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "large.log")
	if err := os.WriteFile(name, []byte(strings.Repeat("x\n", protocol.MaxWorkspacePreviewLines+10)), 0o600); err != nil {
		t.Fatal(err)
	}
	read, done, err := NewService().ReadHostFile(t.Context(), "session_test", protocol.ReadHostFileInput{Path: name})
	defer done()
	if err != nil {
		t.Fatal(err)
	}
	text := read.Text()
	if !text.Truncated || text.TruncationReason != "line_limit" || text.ReturnedLines != protocol.MaxWorkspacePreviewLines ||
		text.Size != int64(2*(protocol.MaxWorkspacePreviewLines+10)) {
		t.Fatalf("read = %+v", text)
	}
	if err := read.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestReadHostFileDoesNotBlockOnFIFOs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs")
	}
	t.Parallel()
	name := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(name, 0o600); err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, _, err := NewService().ReadHostFile(ctx, "session_test", protocol.ReadHostFileInput{Path: name})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != NotFile {
		t.Fatalf("fifo = %v", err)
	}
}

func TestReadHostFileImageDecodesAreAdmittedCancellably(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(name, hostPNG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	for range cap(service.images) {
		service.images <- struct{}{}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := service.ReadHostFile(ctx, "session_test", protocol.ReadHostFileInput{Path: name}); !errors.Is(err, context.Canceled) {
		t.Fatalf("saturated image read = %v, want cancellation", err)
	}
}

func TestReadHostFileHoldsAnImageAdmissionUntilDone(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(name, hostPNG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	read, done, err := service.ReadHostFile(t.Context(), "session_test", protocol.ReadHostFileInput{Path: name})
	if err != nil || read.Image() == nil {
		t.Fatalf("read = %+v, %v", read, err)
	}
	if held := len(service.images); held != 1 {
		t.Fatalf("admissions held while the image is in use = %d, want 1", held)
	}
	done()
	done()
	if held := len(service.images); held != 0 {
		t.Fatalf("admissions held after done = %d, want 0", held)
	}
	text := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(text, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, done, err := service.ReadHostFile(t.Context(), "session_test", protocol.ReadHostFileInput{Path: text}); err != nil || len(service.images) != 0 {
		t.Fatalf("a text read holds an image admission: %v", err)
	} else {
		done()
	}
}

func TestReadHostFileRefusesSocketsWithoutOpeningThem(t *testing.T) {
	t.Parallel()
	directory, err := os.MkdirTemp("", "kit-sock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	name := filepath.Join(directory, "s")
	listener, err := net.Listen("unix", name)
	if err != nil {
		t.Skip(err)
	}
	defer listener.Close()
	if got := hostReadError(t, NewService(), name); got.Code != NotFile {
		t.Fatalf("socket = %+v", got)
	}
}
