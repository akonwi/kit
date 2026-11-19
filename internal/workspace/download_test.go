package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

func openTestDownload(t *testing.T, s *Service, sessionID, cwd, path string) *Download {
	t.Helper()
	ref := s.Ref(sessionID, cwd)
	download, err := s.OpenWorkspaceDownload(context.Background(), sessionID, cwd, protocol.DownloadFileInput{HasWorkspace: true, WorkspaceID: ref.WorkspaceID, Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(download.Close)
	return download
}

func TestWorkspaceDownloadServesTheOpenedFile(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "~"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "~", "notes.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewService()
	download := openTestDownload(t, s, "session_a", cwd, "~/notes.txt")
	info := download.Info
	read := s.Ref("session_a", cwd)
	preview, err := s.Read(context.Background(), "session_a", cwd, protocol.ReadWorkspaceFileInput{WorkspaceID: read.WorkspaceID, Path: "~/notes.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if info.WorkspaceID != read.WorkspaceID || info.Path != "~/notes.txt" || info.Size != 6 || info.Revision != preview.Revision || info.Validate() != nil {
		t.Fatalf("info = %+v, want the read's workspace, path, size 6, and revision %s", info, preview.Revision)
	}
	body, err := io.ReadAll(download.Section(0, info.Size))
	if err != nil || string(body) != "hello\n" {
		t.Fatalf("body = %q, %v", body, err)
	}
}

func TestDownloadKindIsTheRequestsNeverTheOther(t *testing.T) {
	cwd := t.TempDir()
	s := NewService()
	ref := s.Ref("session_a", cwd)
	for name, attempt := range map[string]func() error{
		"absolute path with a workspace": func() error {
			_, err := s.OpenWorkspaceDownload(context.Background(), "session_a", cwd, protocol.DownloadFileInput{HasWorkspace: true, WorkspaceID: ref.WorkspaceID, Path: "/etc/hosts"})
			return err
		},
		"relative path without a workspace": func() error {
			_, err := s.OpenHostDownload(context.Background(), "session_a", protocol.DownloadFileInput{Path: "notes.txt"})
			return err
		},
		"~user without a workspace": func() error {
			_, err := s.OpenHostDownload(context.Background(), "session_a", protocol.DownloadFileInput{Path: "~root/notes.txt"})
			return err
		},
	} {
		var typed *Error
		if err := attempt(); !errors.As(err, &typed) || typed.Code != InvalidPath {
			t.Fatalf("%s: error = %v, want invalid_path", name, err)
		}
	}
}

func TestHostDownloadNamesTheFileReadHostFileReads(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "notes.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewService()
	s.home = func() (string, error) { return home, nil }
	download, err := s.OpenHostDownload(context.Background(), "session_a", protocol.DownloadFileInput{Path: "~/./notes.txt"})
	if err != nil {
		t.Fatal(err)
	}
	defer download.Close()
	read, done, err := s.ReadHostFile(context.Background(), "session_a", protocol.ReadHostFileInput{Path: "~/./notes.txt"})
	done()
	if err != nil {
		t.Fatal(err)
	}
	info := download.Info
	if info.Path != read.Path() || info.Path != filepath.Join(home, "notes.txt") || info.WorkspaceID != "" || !strings.HasPrefix(info.Revision, "hostfile_") || info.Size != 6 || info.Validate() != nil {
		t.Fatalf("info = %+v, want %s with a host revision", info, read.Path())
	}
}

func TestEmptyFileDownloadHasNoLengthOrRevision(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "empty"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info := openTestDownload(t, NewService(), "session_a", cwd, "empty").Info
	if info.KnownLength() || info.Size != -1 || info.Revision != "" || info.Validate() != nil {
		t.Fatalf("info = %+v, want unknown length", info)
	}
}

func TestDownloadSlotsAreBoundedPerSessionAndDaemon(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewService()
	scope := func(err error) string {
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != CapacityExceeded {
			return fmt.Sprintf("error %v", err)
		}
		return typed.Details["scope"]
	}
	first := openTestDownload(t, s, "session_a", cwd, "file")
	second := openTestDownload(t, s, "session_a", cwd, "file")
	third := openTestDownload(t, s, "session_a", cwd, "file")
	if first.StartBody() != nil || second.StartBody() != nil {
		t.Fatal("the session's first two downloads were refused")
	}
	if got := scope(third.StartBody()); got != "session" {
		t.Fatalf("third session download = %s, want capacity_exceeded scope session", got)
	}
	first.Close()
	if err := third.StartBody(); err != nil {
		t.Fatalf("download after one closed = %v", err)
	}
	// Two sessions' slots are held; six more sessions fill the daemon.
	for i := range 6 {
		session := "session_" + string(rune('b'+i))
		if err := openTestDownload(t, s, session, cwd, "file").StartBody(); err != nil {
			t.Fatalf("%s = %v", session, err)
		}
	}
	if got := scope(openTestDownload(t, s, "session_z", cwd, "file").StartBody()); got != "daemon" {
		t.Fatalf("ninth download = %s, want capacity_exceeded scope daemon", got)
	}
}

func TestStartedDownloadsHoldNoReadAdmission(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewService()
	for range 2 {
		if err := openTestDownload(t, s, "session_a", cwd, "file").StartBody(); err != nil {
			t.Fatal(err)
		}
	}
	// Every read slot is free while two bodies are being sent.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for range s.limits.MaxActiveRequests {
		release, err := s.acquire(ctx, "session_a")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
	}
}
