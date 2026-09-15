package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
)

func TestFileRevisionIncludesHostChangeTime(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(name, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	first := fileRevision("workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", before)
	time.Sleep(2 * time.Millisecond)
	if err := os.Chmod(name, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	second := fileRevision("workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", after)
	if first == second {
		t.Fatal("revision did not include ctime")
	}
}

func TestWorkspaceIdentityUsesSessionAndCleanCWD(t *testing.T) {
	t.Parallel()
	service := NewService()
	root := t.TempDir()
	first := service.Ref("session_one", root)
	if first.State != protocol.WorkspaceReady || first.WorkspaceID == "" {
		t.Fatalf("ref = %+v", first)
	}
	if again := service.Ref("session_one", filepath.Join(root, ".")); again.WorkspaceID != first.WorkspaceID {
		t.Fatalf("clean cwd changed identity: %q != %q", again.WorkspaceID, first.WorkspaceID)
	}
	if other := service.Ref("session_two", root); other.WorkspaceID == first.WorkspaceID {
		t.Fatal("workspace identity did not include session")
	}
}

func TestListDirectoryPaginatesStableObservation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"c.txt", "a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService()
	ref := service.Ref("session_test", root)
	first, err := service.List(t.Context(), ref.SessionID, root, protocol.ListDirectoryInput{WorkspaceID: ref.WorkspaceID, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{first.Entries[0].Name, first.Entries[1].Name}; strings.Join(got, ",") != "a.txt,b.txt" || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("first page validation: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "c.txt")); err != nil {
		t.Fatal(err)
	}
	second, err := service.List(t.Context(), ref.SessionID, root, protocol.ListDirectoryInput{WorkspaceID: ref.WorkspaceID, Path: "", PageSize: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 1 || second.Entries[0].Name != "c.txt" || second.Revision != first.Revision {
		t.Fatalf("second page = %+v", second)
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("second page validation: %v", err)
	}
}

func TestListEmptyDirectoryReturnsValidEmptyCollections(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := NewService()
	ref := service.Ref("session_test", root)
	page, err := service.List(t.Context(), ref.SessionID, root, protocol.ListDirectoryInput{WorkspaceID: ref.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if page.Entries == nil || page.Omissions == nil || len(page.Entries) != 0 || len(page.Omissions) != 0 {
		t.Fatalf("empty page collections = entries:%#v omissions:%#v", page.Entries, page.Omissions)
	}
	if err := page.Validate(); err != nil {
		t.Fatalf("empty page validation: %v", err)
	}
}

func TestWorkspaceRejectsStaleAndSymlinkTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	ref := service.Ref("session_test", root)
	_, err := service.List(t.Context(), ref.SessionID, root, protocol.ListDirectoryInput{WorkspaceID: "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	var workspaceErr *Error
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != StaleWorkspace {
		t.Fatalf("stale error = %v", err)
	}
	_, err = service.Read(t.Context(), ref.SessionID, root, protocol.ReadWorkspaceFileInput{WorkspaceID: ref.WorkspaceID, Path: "linked/secret"})
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != SymlinkTraversal {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestSinglePageDirectoryIsNotCached(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "one"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	ref := service.Ref("session_test", root)
	page, err := service.List(t.Context(), ref.SessionID, root, protocol.ListDirectoryInput{WorkspaceID: ref.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != "" || len(service.observations) != 0 {
		t.Fatalf("single page cursor/cache = %q/%d", page.NextCursor, len(service.observations))
	}
}

func TestDaemonPendingAdmissionIsBoundedAndCancellationSafe(t *testing.T) {
	service := NewService()
	for range cap(service.active) {
		service.active <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, maxDaemonPendingRequests)
	for i := range maxDaemonPendingRequests {
		go func() {
			release, err := service.acquire(ctx, fmt.Sprintf("session_%d", i))
			if release != nil {
				release()
			}
			results <- err
		}()
	}
	deadline := time.Now().Add(time.Second)
	for {
		service.mu.Lock()
		pending := service.daemonPending
		service.mu.Unlock()
		if pending == maxDaemonPendingRequests {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon pending = %d", pending)
		}
		runtime.Gosched()
	}
	_, err := service.acquire(t.Context(), "overflow")
	var workspaceErr *Error
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != CapacityExceeded || workspaceErr.Details["scope"] != "daemon" {
		t.Fatalf("overflow error = %v", err)
	}
	cancel()
	for range maxDaemonPendingRequests {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter = %v", err)
		}
	}
	service.mu.Lock()
	pending := service.daemonPending
	service.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending after cancellation = %d", pending)
	}
	for range cap(service.active) {
		<-service.active
	}
}

func TestObservationCacheEnforcesAggregateBudgets(t *testing.T) {
	service := NewService()
	for i := range 12 {
		entries := make([]protocol.WorkspaceDirectoryEntry, 10_000)
		obs := &observation{sessionID: fmt.Sprintf("session_%d", i%3), entries: entries, bytes: 8 << 20, touched: time.Now()}
		service.storeObservation(service.newCursorID(), obs)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	for _, sessionID := range []string{"session_0", "session_1", "session_2"} {
		entries, bytes, _, _ := observationUsage(service.observations, sessionID)
		if entries > maxSessionObservationEntries || bytes > maxSessionObservationBytes {
			t.Fatalf("session cache = %d entries/%d bytes", entries, bytes)
		}
	}
	_, _, entries, bytes := observationUsage(service.observations, "")
	if entries > maxDaemonObservationEntries || bytes > maxDaemonObservationBytes {
		t.Fatalf("daemon cache = %d entries/%d bytes", entries, bytes)
	}
}

func TestDirectoryCursorRejectsTampering(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService()
	ref := service.Ref("session_test", root)
	first, err := service.List(t.Context(), ref.SessionID, root, protocol.ListDirectoryInput{WorkspaceID: ref.WorkspaceID, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	cursor := first.NextCursor
	cursor = strings.Replace(cursor, ".1.1.", ".0.1.", 1)
	_, err = service.List(t.Context(), ref.SessionID, root, protocol.ListDirectoryInput{WorkspaceID: ref.WorkspaceID, PageSize: 1, Cursor: cursor})
	var workspaceErr *Error
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != StaleCursor {
		t.Fatalf("tampered cursor error = %v", err)
	}
}

func TestReadFinalSymlinkStaysInsideWorkspace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "target"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "inside-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../target", filepath.Join(root, "dir", "parent-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(root, "outside-link")); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	ref := service.Ref("session_test", root)
	read, err := service.Read(t.Context(), ref.SessionID, root, protocol.ReadWorkspaceFileInput{WorkspaceID: ref.WorkspaceID, Path: "inside-link"})
	if err != nil || read.Content != "inside" {
		t.Fatalf("inside link = %+v, %v", read, err)
	}
	read, err = service.Read(t.Context(), ref.SessionID, root, protocol.ReadWorkspaceFileInput{WorkspaceID: ref.WorkspaceID, Path: "dir/parent-link"})
	if err != nil || read.Content != "inside" {
		t.Fatalf("parent link = %+v, %v", read, err)
	}
	_, err = service.Read(t.Context(), ref.SessionID, root, protocol.ReadWorkspaceFileInput{WorkspaceID: ref.WorkspaceID, Path: "outside-link"})
	var workspaceErr *Error
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != OutsideWorkspace {
		t.Fatalf("outside link error = %v", err)
	}
}

func TestReadFileReportsBinaryTruncationAndStaleness(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := NewService()
	ref := service.Ref("session_test", root)
	if err := os.WriteFile(filepath.Join(root, "binary"), []byte{1, 0, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := service.Read(t.Context(), ref.SessionID, root, protocol.ReadWorkspaceFileInput{WorkspaceID: ref.WorkspaceID, Path: "binary"})
	var workspaceErr *Error
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != BinaryFile {
		t.Fatalf("binary error = %v", err)
	}
	content := strings.Repeat("x", protocol.MaxWorkspacePreviewBytes+1)
	if err := os.WriteFile(filepath.Join(root, "large"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := service.Read(t.Context(), ref.SessionID, root, protocol.ReadWorkspaceFileInput{WorkspaceID: ref.WorkspaceID, Path: "large"})
	if err != nil || !read.Truncated || read.TruncationReason != "byte_limit" || len(read.Content) != protocol.MaxWorkspacePreviewBytes {
		t.Fatalf("large read = %+v, %v", read, err)
	}
	_, err = service.Read(t.Context(), ref.SessionID, root, protocol.ReadWorkspaceFileInput{WorkspaceID: ref.WorkspaceID, Path: "large", ExpectedFileRevision: "file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != StaleFile {
		t.Fatalf("stale file error = %v", err)
	}
}
