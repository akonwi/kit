package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
	"syscall"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

// Download admission (ADR 0074). A download holds a read admission while it
// resolves and opens its file, then trades it for a download slot before it
// sends a body, so a slow download never holds a read slot.
const (
	maxSessionDownloads = 2
	maxDaemonDownloads  = 8
)

// Download is one opened regular file, ready to serve. Close releases its
// descriptor and every admission it holds, once.
type Download struct {
	File *os.File
	Info protocol.DownloadedFileInfo

	service      *Service
	releaseRead  func()
	releaseSlots func()
	closeOnce    sync.Once
}

// OpenWorkspaceDownload resolves and opens a workspace file to download.
// The caller closes the download.
func (s *Service) OpenWorkspaceDownload(ctx context.Context, sessionID, cwd string, input protocol.DownloadFileInput) (*Download, error) {
	if !input.HasWorkspace {
		return nil, &Error{Code: InvalidPath, Message: "workspace download requires a workspace"}
	}
	if err := input.Validate(); err != nil {
		return nil, classifyInput(err, input.Path, 0)
	}
	release, err := s.acquire(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	ref, err := s.check(sessionID, cwd, input.WorkspaceID)
	if err != nil {
		release()
		return nil, err
	}
	nameChecks := 0
	_, file, info, closeRoots, err := openWorkspaceFile(cwd, input.Path, &nameChecks)
	if err != nil {
		release()
		return nil, err
	}
	// The descriptor alone serves the download.
	closeRoots()
	download := &Download{File: file, service: s, releaseRead: sync.OnceFunc(release), releaseSlots: func() {}}
	download.Info = protocol.DownloadedFileInfo{SessionID: sessionID, WorkspaceID: ref.WorkspaceID, Path: input.Path, ModifiedAt: info.ModTime().UTC().Format(time.RFC3339Nano)}
	download.setLength(info, fileRevision(ref.WorkspaceID, info))
	return download, nil
}

// OpenHostDownload opens a file on this machine to download. The path is
// normalized as ReadHostFile normalizes it, so both name the same file. The
// caller closes the download.
func (s *Service) OpenHostDownload(ctx context.Context, sessionID string, input protocol.DownloadFileInput) (*Download, error) {
	if input.HasWorkspace {
		return nil, &Error{Code: InvalidPath, Message: "host download names no workspace"}
	}
	if err := input.Validate(); err != nil {
		return nil, hostInputError(err, input.Path)
	}
	name, err := s.expandHome(input.Path)
	if err != nil {
		return nil, err
	}
	release, err := s.acquire(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Download, error) { release(); return nil, err }
	// Only a regular file is opened: opening a device or socket can have
	// side effects, or block, even without reading it. The descriptor's
	// own type and identity are checked again after opening.
	initial, err := os.Stat(name)
	if err != nil {
		return fail(classifyHost(err))
	}
	if !initial.Mode().IsRegular() {
		return fail(&Error{Code: NotFile, Message: "path is not a regular file"})
	}
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return fail(classifyHost(err))
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fail(classifyHost(err))
	}
	if !info.Mode().IsRegular() || !os.SameFile(initial, info) {
		_ = file.Close()
		return fail(&Error{Code: Unavailable, Message: "the file moved while it was opened; try again"})
	}
	download := &Download{File: file, service: s, releaseRead: sync.OnceFunc(release), releaseSlots: func() {}}
	download.Info = protocol.DownloadedFileInfo{SessionID: sessionID, Path: name, ModifiedAt: info.ModTime().UTC().Format(time.RFC3339Nano)}
	download.setLength(info, hostFileRevision(name, info))
	return download, nil
}

// setLength records a file's size and revision, or neither for a file that
// reports a size of zero: such files are often generated as they're read,
// so their metadata can't validate their content.
func (d *Download) setLength(info fs.FileInfo, revision string) {
	if info.Size() <= 0 {
		d.Info.Size, d.Info.Revision = -1, ""
		return
	}
	d.Info.Size, d.Info.Revision = info.Size(), revision
}

// StartBody trades the download's read admission for a download slot, per
// session and then for the daemon, without waiting. When either is full it
// fails with capacity_exceeded naming the first full gate, and the download
// keeps only its read admission until it is closed.
func (d *Download) StartBody() error {
	s := d.service
	s.mu.Lock()
	if s.downloads == nil {
		s.downloads = map[string]int{}
	}
	sessionID := d.Info.SessionID
	switch {
	case s.downloads[sessionID] >= maxSessionDownloads:
		s.mu.Unlock()
		return &Error{Code: CapacityExceeded, Message: "too many downloads for this session", Details: map[string]string{"scope": "session"}}
	case s.daemonDownloads >= maxDaemonDownloads:
		s.mu.Unlock()
		return &Error{Code: CapacityExceeded, Message: "too many downloads", Details: map[string]string{"scope": "daemon"}}
	}
	s.downloads[sessionID]++
	s.daemonDownloads++
	s.mu.Unlock()
	d.releaseSlots = sync.OnceFunc(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.daemonDownloads--
		if s.downloads[sessionID]--; s.downloads[sessionID] <= 0 {
			delete(s.downloads, sessionID)
		}
	})
	d.releaseRead()
	return nil
}

// Close closes the descriptor and releases every admission, once.
func (d *Download) Close() {
	d.closeOnce.Do(func() {
		_ = d.File.Close()
		d.releaseSlots()
		d.releaseRead()
	})
}

// Section returns a reader of length bytes from offset start of a
// known-length download, or of the whole file to end of file for a download
// with no known length.
func (d *Download) Section(start, length int64) io.Reader {
	if !d.Info.KnownLength() {
		return d.File
	}
	return io.NewSectionReader(d.File, start, length)
}

func hostFileRevision(name string, info fs.FileInfo) string {
	h := sha256.New()
	_, _ = io.WriteString(h, "kit-hostfile-v1\x00"+name)
	fmt.Fprintf(h, "\x00%d\x00%d", info.Size(), info.ModTime().UnixNano())
	device, inode, ctime := hostFileIdentity(info)
	fmt.Fprintf(h, "\x00%d\x00%d\x00%d", device, inode, ctime)
	return "hostfile_" + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
