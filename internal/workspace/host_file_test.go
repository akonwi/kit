package workspace

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

func hostDownloadError(t *testing.T, service *Service, path string) *Error {
	t.Helper()
	download, err := service.OpenHostDownload(t.Context(), "session_test", protocol.DownloadFileInput{Path: path})
	if err == nil {
		download.Close()
	}
	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("download %q error = %v, want a workspace error", path, err)
	}
	if err := typed.Validate(); err != nil {
		t.Fatalf("download %q error %+v is not a valid wire error: %v", path, typed, err)
	}
	return typed
}

func TestHostDownloadUsesTheServerHome(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("export A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	service.home = func() (string, error) { return home, nil }
	download, err := service.OpenHostDownload(t.Context(), "session_test", protocol.DownloadFileInput{Path: "~/.profile"})
	if err != nil {
		t.Fatal(err)
	}
	defer download.Close()
	if download.Info.Path != filepath.Join(home, ".profile") {
		t.Fatalf("path = %q", download.Info.Path)
	}
	service.home = func() (string, error) { return "", errors.New("no home") }
	if got := hostDownloadError(t, service, "~/.profile"); got.Code != Unavailable {
		t.Fatalf("unknown home = %+v", got)
	}
}

func TestHostDownloadCleansPathsAndFollowsSymlinks(t *testing.T) {
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
	download, err := NewService().OpenHostDownload(t.Context(), "session_test", protocol.DownloadFileInput{Path: root + "/sub/../link.txt"})
	if err != nil {
		t.Fatal(err)
	}
	defer download.Close()
	body, err := io.ReadAll(download.Section(0, download.Info.Size))
	if err != nil || download.Info.Path != link || string(body) != "linked" {
		t.Fatalf("download = %+v %q, %v", download.Info, body, err)
	}
}

func TestHostDownloadRefusesWhatIsNotAFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := NewService()
	if got := hostDownloadError(t, service, root); got.Code != NotFile {
		t.Fatalf("directory = %+v", got)
	}
	if got := hostDownloadError(t, service, filepath.Join(root, "missing.txt")); got.Code != NotFound {
		t.Fatalf("missing = %+v", got)
	}
	for _, path := range []string{"relative.txt", "/a\nb"} {
		if got := hostDownloadError(t, service, path); got.Code != InvalidPath {
			t.Fatalf("%q = %+v", path, got)
		}
	}
	if got := hostDownloadError(t, service, "/"+strings.Repeat("a", protocol.MaxWorkspacePathBytes)); got.Code != LimitExceeded || got.Details["limit"] != "path_bytes" {
		t.Fatalf("long path = %+v", got)
	}
}

func TestHostDownloadDoesNotBlockOnFIFOs(t *testing.T) {
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
	_, err := NewService().OpenHostDownload(ctx, "session_test", protocol.DownloadFileInput{Path: name})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != NotFile {
		t.Fatalf("fifo = %v", err)
	}
}

func TestHostDownloadRefusesSocketsWithoutOpeningThem(t *testing.T) {
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
	if got := hostDownloadError(t, NewService(), name); got.Code != NotFile {
		t.Fatalf("socket = %+v", got)
	}
}
