// Package workspace owns bounded, session-scoped host filesystem exploration.
package workspace

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
)

type Error = protocol.WorkspaceError

const (
	InvalidPath      = protocol.WorkspaceErrorInvalidPath
	NotFound         = protocol.WorkspaceErrorNotFound
	NotDirectory     = protocol.WorkspaceErrorNotDirectory
	NotFile          = protocol.WorkspaceErrorNotFile
	PermissionDenied = protocol.WorkspaceErrorPermissionDenied
	OutsideWorkspace = protocol.WorkspaceErrorOutside
	SymlinkTraversal = protocol.WorkspaceErrorSymlink
	BinaryFile       = protocol.WorkspaceErrorBinary
	StaleWorkspace   = protocol.WorkspaceErrorStaleWorkspace
	StaleFile        = protocol.WorkspaceErrorStaleFile
	StaleCursor      = protocol.WorkspaceErrorStaleCursor
	LimitExceeded    = protocol.WorkspaceErrorLimit
	CapacityExceeded = protocol.WorkspaceErrorCapacity
	Unavailable      = protocol.WorkspaceErrorUnavailable
)

const (
	observationTTL               = 30 * time.Second
	maxDaemonPendingRequests     = 64
	maxSessionObservationEntries = 20_000
	maxDaemonObservationEntries  = 80_000
	maxSessionObservationBytes   = 16 << 20
	maxDaemonObservationBytes    = 64 << 20
)

type observation struct {
	sessionID, workspaceID, directory, revision string
	entries                                     []protocol.WorkspaceDirectoryEntry
	omissions                                   []protocol.WorkspaceOmission
	truncated                                   bool
	truncationReason                            string
	touched                                     time.Time
	bytes                                       int
}

type sessionGate struct {
	active  chan struct{}
	pending int
}

type Service struct {
	mu            sync.Mutex
	observations  map[string]*observation
	sessions      map[string]*sessionGate
	active        chan struct{}
	daemonPending int
	cursorKey     [32]byte
	limits        protocol.WorkspaceLimits
	now           func() time.Time
}

func NewService() *Service {
	limits := protocol.DefaultWorkspaceLimits()
	service := &Service{observations: map[string]*observation{}, sessions: map[string]*sessionGate{}, active: make(chan struct{}, limits.MaxActiveRequests*4), limits: limits, now: time.Now}
	if _, err := rand.Read(service.cursorKey[:]); err != nil {
		panic(fmt.Sprintf("initialize workspace cursor key: %v", err))
	}
	return service
}
func (s *Service) Limits() protocol.WorkspaceLimits { return s.limits }

// RemoveSession releases cached observations and admission state for a session.
func (s *Service) RemoveSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	for key, observation := range s.observations {
		if observation.sessionID == sessionID {
			delete(s.observations, key)
		}
	}
}

func workspaceID(sessionID, cwd string) string {
	h := sha256.New()
	io.WriteString(h, "kit-workspace-v1\x00")
	fmt.Fprintf(h, "%d:", len(sessionID))
	io.WriteString(h, sessionID)
	fmt.Fprintf(h, "%d:", len(cwd))
	io.WriteString(h, cwd)
	return "workspace_" + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (s *Service) Ref(sessionID, cwd string) protocol.WorkspaceRef {
	cwd = filepath.Clean(cwd)
	ref := protocol.WorkspaceRef{SessionID: sessionID, CWD: cwd, WorkspaceID: workspaceID(sessionID, cwd), State: protocol.WorkspaceReady, Limits: s.limits}
	r, err := os.OpenRoot(cwd)
	if err != nil {
		ref.State = protocol.WorkspaceUnavailable
	} else {
		_ = r.Close()
	}
	return ref
}

func (s *Service) acquire(ctx context.Context, sessionID string) (func(), error) {
	s.mu.Lock()
	gate := s.sessions[sessionID]
	if gate == nil {
		gate = &sessionGate{active: make(chan struct{}, s.limits.MaxActiveRequests)}
		s.sessions[sessionID] = gate
	}
	if gate.pending >= s.limits.MaxPendingRequests {
		s.mu.Unlock()
		return nil, &Error{Code: CapacityExceeded, Message: "workspace request queue is full", Details: map[string]string{"scope": "session"}}
	}
	if s.daemonPending >= maxDaemonPendingRequests {
		s.mu.Unlock()
		return nil, &Error{Code: CapacityExceeded, Message: "daemon workspace request queue is full", Details: map[string]string{"scope": "daemon"}}
	}
	gate.pending++
	s.daemonPending++
	s.mu.Unlock()
	finishPending := func() { s.mu.Lock(); gate.pending--; s.daemonPending--; s.mu.Unlock() }
	select {
	case gate.active <- struct{}{}:
	case <-ctx.Done():
		finishPending()
		return nil, ctx.Err()
	}
	select {
	case s.active <- struct{}{}:
		finishPending()
		return func() { <-s.active; <-gate.active }, nil
	case <-ctx.Done():
		<-gate.active
		finishPending()
		return nil, ctx.Err()
	}
}

func (s *Service) check(sessionID, cwd, expected string) (protocol.WorkspaceRef, error) {
	ref := s.Ref(sessionID, cwd)
	if expected != ref.WorkspaceID {
		return ref, &Error{Code: StaleWorkspace, Message: "the session workspace changed", Details: map[string]string{"currentWorkspaceId": ref.WorkspaceID, "currentWorkspaceState": string(ref.State)}}
	}
	if ref.State != protocol.WorkspaceReady {
		_, err := os.OpenRoot(cwd)
		return ref, classify(err, NotDirectory)
	}
	return ref, nil
}

func (s *Service) List(ctx context.Context, sessionID, cwd string, input protocol.ListDirectoryInput) (protocol.DirectoryPage, error) {
	if err := input.Validate(); err != nil {
		return protocol.DirectoryPage{}, classifyInput(err, input.Path, input.PageSize)
	}
	release, err := s.acquire(ctx, sessionID)
	if err != nil {
		return protocol.DirectoryPage{}, err
	}
	defer release()
	ref, err := s.check(sessionID, cwd, input.WorkspaceID)
	if err != nil {
		return protocol.DirectoryPage{}, err
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = s.limits.DefaultDirectoryPageSize
	}
	if input.Cursor != "" {
		return s.pageCursor(ref, input.Path, input.Cursor, pageSize)
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return protocol.DirectoryPage{}, classify(err, NotDirectory)
	}
	defer root.Close()
	nameChecks := 0
	targetRoot, closeTarget, err := openDirectoryPath(root, input.Path, &nameChecks)
	if err != nil {
		return protocol.DirectoryPage{}, err
	}
	defer closeTarget()
	dir, err := targetRoot.Open(".")
	if err != nil {
		return protocol.DirectoryPage{}, classify(err, NotDirectory)
	}
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil {
		return protocol.DirectoryPage{}, classify(err, NotDirectory)
	}
	if !info.IsDir() {
		return protocol.DirectoryPage{}, &Error{Code: NotDirectory, Message: "workspace path is not a directory"}
	}
	raw, err := dir.ReadDir(s.limits.MaxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return protocol.DirectoryPage{}, classify(err, NotDirectory)
	}
	truncated := len(raw) > s.limits.MaxDirectoryEntries
	truncationReason := ""
	if truncated {
		truncationReason = "entry_limit"
		raw = raw[:s.limits.MaxDirectoryEntries]
	}
	entries := make([]protocol.WorkspaceDirectoryEntry, 0, len(raw))
	observationBytes := 0
	omissions := map[string]int{}
	for _, entry := range raw {
		if err := ctx.Err(); err != nil {
			return protocol.DirectoryPage{}, err
		}
		child := entry.Name()
		if input.Path != "" {
			child = input.Path + "/" + child
		}
		if protocol.ValidateWorkspacePath(child, false) != nil {
			omissions["unsupported_name"]++
			continue
		}
		kind := kindOf(entry.Type())
		entryInfo, statErr := entry.Info()
		if statErr == nil {
			kind = kindOf(entryInfo.Mode())
		}
		if kind == protocol.WorkspaceEntryDirectory && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			continue
		}
		out := protocol.WorkspaceDirectoryEntry{Path: child, Name: entry.Name(), Kind: kind}
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				omissions["entry_raced"]++
			} else {
				omissions["metadata_unavailable"]++
			}
			continue
		} else {
			size := entryInfo.Size()
			out.Size = &size
			out.ModifiedAt = entryInfo.ModTime().UTC().Format(time.RFC3339Nano)
			if kind == protocol.WorkspaceEntryFile {
				out.FileRevision = fileRevision(ref.WorkspaceID, entryInfo)
			}
		}
		encodedEntry, _ := json.Marshal(out)
		if observationBytes+len(encodedEntry) > s.limits.MaxDirectoryObservationBytes {
			truncated = true
			truncationReason = "observation_limit"
			break
		}
		observationBytes += len(encodedEntry)
		entries = append(entries, out)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	current, err := s.check(sessionID, cwd, input.WorkspaceID)
	if err != nil {
		return protocol.DirectoryPage{}, err
	}
	revision := directoryRevision(ref.WorkspaceID, input.Path, entries, truncated, truncationReason, omissions)
	obs := &observation{sessionID: sessionID, workspaceID: ref.WorkspaceID, directory: input.Path, revision: revision, entries: entries, omissions: projectOmissions(omissions), truncated: truncated, truncationReason: truncationReason, touched: s.now(), bytes: observationBytes}
	ref = current
	cursor := s.newCursorID()
	page := s.page(obs, ref, 0, pageSize, cursor)
	if page.NextCursor != "" {
		s.storeObservation(cursor, obs)
	}
	return page, nil
}

func (s *Service) pageCursor(ref protocol.WorkspaceRef, directory, cursor string, pageSize int) (protocol.DirectoryPage, error) {
	parts := strings.Split(cursor, ".")
	if len(parts) != 4 {
		return protocol.DirectoryPage{}, staleCursor()
	}
	var offset, boundSize int
	if _, err := fmt.Sscanf(parts[1], "%d", &offset); err != nil {
		return protocol.DirectoryPage{}, staleCursor()
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &boundSize); err != nil || boundSize != pageSize {
		return protocol.DirectoryPage{}, staleCursor()
	}
	payload := strings.Join(parts[:3], ".")
	want := s.signCursor(payload)
	if !hmac.Equal([]byte(parts[3]), []byte(want)) {
		return protocol.DirectoryPage{}, staleCursor()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked()
	obs := s.observations[parts[0]]
	if obs == nil || obs.workspaceID != ref.WorkspaceID || obs.directory != directory || offset < 0 || offset >= len(obs.entries) {
		return protocol.DirectoryPage{}, staleCursor()
	}
	obs.touched = s.now()
	return s.page(obs, ref, offset, pageSize, parts[0]), nil
}
func (s *Service) newCursorID() string {
	var token [18]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic(fmt.Sprintf("create workspace cursor: %v", err))
	}
	return "cursor_" + base64.RawURLEncoding.EncodeToString(token[:])
}
func (s *Service) storeObservation(id string, obs *observation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked()
	for {
		sessionEntries, sessionBytes, totalEntries, totalBytes := observationUsage(s.observations, obs.sessionID)
		sessionOver := sessionEntries+len(obs.entries) > maxSessionObservationEntries || sessionBytes+obs.bytes > maxSessionObservationBytes
		if !sessionOver && totalEntries+len(obs.entries) <= maxDaemonObservationEntries && totalBytes+obs.bytes <= maxDaemonObservationBytes {
			break
		}
		scope := ""
		if sessionOver {
			scope = obs.sessionID
		}
		oldest := oldestObservation(s.observations, scope, "")
		if oldest == "" {
			return
		}
		delete(s.observations, oldest)
	}
	s.observations[id] = obs
}
func (s *Service) signCursor(payload string) string {
	mac := hmac.New(sha256.New, s.cursorKey[:])
	io.WriteString(mac, payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func staleCursor() error {
	return &Error{Code: StaleCursor, Message: "directory cursor is unavailable"}
}
func (s *Service) page(obs *observation, ref protocol.WorkspaceRef, offset, size int, base string) protocol.DirectoryPage {
	end := min(offset+size, len(obs.entries))
	result := protocol.DirectoryPage{SessionID: obs.sessionID, Workspace: ref, Path: obs.directory, Revision: obs.revision, Truncated: obs.truncated, TruncationReason: obs.truncationReason, Omissions: append([]protocol.WorkspaceOmission{}, obs.omissions...)}
	for {
		result.Entries = append([]protocol.WorkspaceDirectoryEntry{}, obs.entries[offset:end]...)
		result.NextCursor = ""
		if end < len(obs.entries) {
			payload := fmt.Sprintf("%s.%d.%d", base, end, size)
			result.NextCursor = payload + "." + s.signCursor(payload)
		}
		encoded, _ := json.Marshal(result)
		if len(encoded) <= protocol.MaxDirectoryResponseBytes || end == offset {
			break
		}
		end--
	}
	return result
}
func observationUsage(all map[string]*observation, sessionID string) (sessionEntries, sessionBytes, totalEntries, totalBytes int) {
	for _, obs := range all {
		totalEntries += len(obs.entries)
		totalBytes += obs.bytes
		if obs.sessionID == sessionID {
			sessionEntries += len(obs.entries)
			sessionBytes += obs.bytes
		}
	}
	return
}
func oldestObservation(all map[string]*observation, sessionID, except string) string {
	var key string
	var at time.Time
	for candidate, obs := range all {
		if candidate == except || sessionID != "" && obs.sessionID != sessionID {
			continue
		}
		if key == "" || obs.touched.Before(at) {
			key, at = candidate, obs.touched
		}
	}
	return key
}

func (s *Service) evictLocked() {
	cutoff := s.now().Add(-observationTTL)
	for k, v := range s.observations {
		if v.touched.Before(cutoff) {
			delete(s.observations, k)
		}
	}
	for len(s.observations) >= 64 {
		var oldest string
		var at time.Time
		for k, v := range s.observations {
			if oldest == "" || v.touched.Before(at) {
				oldest, at = k, v.touched
			}
		}
		delete(s.observations, oldest)
	}
}

func (s *Service) Read(ctx context.Context, sessionID, cwd string, input protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	if err := input.Validate(); err != nil {
		return protocol.WorkspaceFileRead{}, classifyInput(err, input.Path, 0)
	}
	release, err := s.acquire(ctx, sessionID)
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	defer release()
	ref, err := s.check(sessionID, cwd, input.WorkspaceID)
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return protocol.WorkspaceFileRead{}, classify(err, NotFile)
	}
	defer root.Close()
	nameChecks := 0
	parentRoot, finalName, closeParent, err := openParentPath(root, input.Path, &nameChecks)
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	defer closeParent()
	finalInfo, err := parentRoot.Lstat(finalName)
	if err != nil {
		return protocol.WorkspaceFileRead{}, classify(err, NotFile)
	}
	finalSymlink := finalInfo.Mode()&os.ModeSymlink != 0
	openName := finalName
	openRoot := parentRoot
	if finalSymlink {
		openName, err = resolveWorkspaceSymlink(root, input.Path)
		if err != nil {
			return protocol.WorkspaceFileRead{}, err
		}
		openRoot = root
	}
	if !finalInfo.Mode().IsRegular() && !finalSymlink {
		return protocol.WorkspaceFileRead{}, &Error{Code: NotFile, Message: "workspace path is not a regular file"}
	}
	file, err := openRoot.OpenFile(openName, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil && finalSymlink {
		return protocol.WorkspaceFileRead{}, classifyFinalSymlink(err)
	}
	if err != nil {
		return protocol.WorkspaceFileRead{}, classify(err, NotFile)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return protocol.WorkspaceFileRead{}, classify(err, NotFile)
	}
	if !before.Mode().IsRegular() {
		return protocol.WorkspaceFileRead{}, &Error{Code: NotFile, Message: "workspace path is not a regular file"}
	}
	revision := fileRevision(ref.WorkspaceID, before)
	if input.ExpectedFileRevision != "" && input.ExpectedFileRevision != revision {
		return protocol.WorkspaceFileRead{}, &Error{Code: StaleFile, Message: "workspace file changed", Details: map[string]string{"currentFileRevision": revision}}
	}
	data, reason, err := readPreview(ctx, file, s.limits.MaxPreviewBytes, s.limits.MaxPreviewLines)
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	after, err := file.Stat()
	if err != nil || fileRevision(ref.WorkspaceID, after) != revision {
		return protocol.WorkspaceFileRead{}, &Error{Code: StaleFile, Message: "workspace file changed while it was read"}
	}
	currentParent, currentName, closeCurrent, err := openParentPath(root, input.Path, &nameChecks)
	if err != nil {
		var typed *Error
		if errors.As(err, &typed) && typed.Code == LimitExceeded {
			return protocol.WorkspaceFileRead{}, typed
		}
		return protocol.WorkspaceFileRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
	}
	defer closeCurrent()
	currentOpenName := currentName
	currentOpenRoot := currentParent
	currentLstat, lstatErr := currentParent.Lstat(currentName)
	if lstatErr != nil {
		return protocol.WorkspaceFileRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
	}
	if currentLstat.Mode()&os.ModeSymlink != 0 {
		currentOpenName, err = resolveWorkspaceSymlink(root, input.Path)
		if err != nil {
			return protocol.WorkspaceFileRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
		}
		currentOpenRoot = root
	}
	currentFile, err := currentOpenRoot.OpenFile(currentOpenName, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return protocol.WorkspaceFileRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
	}
	currentInfo, currentErr := currentFile.Stat()
	_ = currentFile.Close()
	if currentErr != nil || !os.SameFile(before, currentInfo) || fileRevision(ref.WorkspaceID, currentInfo) != revision {
		return protocol.WorkspaceFileRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
	}
	current, err := s.check(sessionID, cwd, input.WorkspaceID)
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	return protocol.WorkspaceFileRead{SessionID: sessionID, Workspace: current, Path: input.Path, Revision: revision, Size: before.Size(), ModifiedAt: before.ModTime().UTC().Format(time.RFC3339Nano), Encoding: "utf-8", Content: string(data), ReturnedBytes: len(data), ReturnedLines: lineCount(data), Truncated: reason != "", TruncationReason: reason}, nil
}

// LineRangeInput requests complete inclusive lines from a pinned workspace file.
type LineRangeInput struct {
	WorkspaceID          string
	Path                 string
	ExpectedFileRevision string
	StartLine            int
	EndLine              int
}

// LineRangeRead is an authoritative complete-line read.
type LineRangeRead struct {
	Content  string
	Revision string
}

// ReadLineRange securely reads complete requested lines without relying on the
// bounded prefix preview contract.
func (s *Service) ReadLineRange(ctx context.Context, sessionID, cwd string, input LineRangeInput) (LineRangeRead, error) {
	validation := protocol.ReadWorkspaceFileInput{WorkspaceID: input.WorkspaceID, Path: input.Path, ExpectedFileRevision: input.ExpectedFileRevision}
	if err := validation.Validate(); err != nil || input.StartLine <= 0 || input.EndLine < input.StartLine || input.EndLine-input.StartLine+1 > 200 {
		return LineRangeRead{}, classifyInput(fmt.Errorf("workspace line range is invalid"), input.Path, 0)
	}
	release, err := s.acquire(ctx, sessionID)
	if err != nil {
		return LineRangeRead{}, err
	}
	defer release()
	ref, err := s.check(sessionID, cwd, input.WorkspaceID)
	if err != nil {
		return LineRangeRead{}, err
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return LineRangeRead{}, classify(err, NotFile)
	}
	defer root.Close()
	nameChecks := 0
	parentRoot, finalName, closeParent, err := openParentPath(root, input.Path, &nameChecks)
	if err != nil {
		return LineRangeRead{}, err
	}
	defer closeParent()
	finalInfo, err := parentRoot.Lstat(finalName)
	if err != nil {
		return LineRangeRead{}, classify(err, NotFile)
	}
	openName, openRoot := finalName, parentRoot
	if finalInfo.Mode()&os.ModeSymlink != 0 {
		openName, err = resolveWorkspaceSymlink(root, input.Path)
		if err != nil {
			return LineRangeRead{}, err
		}
		openRoot = root
	} else if !finalInfo.Mode().IsRegular() {
		return LineRangeRead{}, &Error{Code: NotFile, Message: "workspace path is not a regular file"}
	}
	file, err := openRoot.OpenFile(openName, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return LineRangeRead{}, classify(err, NotFile)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return LineRangeRead{}, classify(err, NotFile)
	}
	if !before.Mode().IsRegular() {
		return LineRangeRead{}, &Error{Code: NotFile, Message: "workspace path is not a regular file"}
	}
	revision := fileRevision(ref.WorkspaceID, before)
	if input.ExpectedFileRevision != "" && input.ExpectedFileRevision != revision {
		return LineRangeRead{}, &Error{Code: StaleFile, Message: "workspace file changed", Details: map[string]string{"currentFileRevision": revision}}
	}
	reader := bufio.NewReaderSize(file, 32<<10)
	selected := make([]string, 0, input.EndLine-input.StartLine+1)
	totalRead := 0
	for lineNumber := 1; lineNumber <= input.EndLine; lineNumber++ {
		if err := ctx.Err(); err != nil {
			return LineRangeRead{}, err
		}
		line, readErr := reader.ReadString('\n')
		totalRead += len(line)
		if totalRead > 64<<20 || len(line) > 1<<20 {
			return LineRangeRead{}, &Error{Code: LimitExceeded, Message: "workspace line range exceeds read limits"}
		}
		if strings.IndexByte(line, 0) >= 0 || !utf8.ValidString(line) {
			return LineRangeRead{}, &Error{Code: BinaryFile, Message: "binary files cannot be read as lines"}
		}
		if lineNumber >= input.StartLine {
			selected = append(selected, strings.TrimSuffix(line, "\n"))
		}
		if readErr == io.EOF {
			if line == "" || lineNumber < input.EndLine {
				return LineRangeRead{}, &Error{Code: NotFound, Message: "requested workspace lines are unavailable"}
			}
			break
		}
		if readErr != nil {
			return LineRangeRead{}, classify(readErr, NotFile)
		}
	}
	after, err := file.Stat()
	if err != nil || fileRevision(ref.WorkspaceID, after) != revision {
		return LineRangeRead{}, &Error{Code: StaleFile, Message: "workspace file changed while it was read"}
	}
	currentParent, currentName, closeCurrent, err := openParentPath(root, input.Path, &nameChecks)
	if err != nil {
		return LineRangeRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
	}
	defer closeCurrent()
	currentOpenName, currentOpenRoot := currentName, currentParent
	currentLstat, err := currentParent.Lstat(currentName)
	if err != nil {
		return LineRangeRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
	}
	if currentLstat.Mode()&os.ModeSymlink != 0 {
		currentOpenName, err = resolveWorkspaceSymlink(root, input.Path)
		if err != nil {
			return LineRangeRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
		}
		currentOpenRoot = root
	}
	currentFile, err := currentOpenRoot.OpenFile(currentOpenName, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return LineRangeRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
	}
	currentInfo, currentErr := currentFile.Stat()
	_ = currentFile.Close()
	if currentErr != nil || !os.SameFile(before, currentInfo) || fileRevision(ref.WorkspaceID, currentInfo) != revision {
		return LineRangeRead{}, &Error{Code: StaleFile, Message: "workspace file path changed while it was read"}
	}
	if _, err := s.check(sessionID, cwd, input.WorkspaceID); err != nil {
		return LineRangeRead{}, err
	}
	return LineRangeRead{Content: strings.Join(selected, "\n"), Revision: revision}, nil
}

func exactChild(root *os.Root, name string, checked *int) (fs.FileInfo, error) {
	remaining := 20_000 - *checked
	if remaining <= 0 {
		return nil, &Error{Code: LimitExceeded, Message: "workspace path name checks exceeded limit", Details: map[string]string{"limit": "path_name_checks"}}
	}
	f, err := root.Open(".")
	if err != nil {
		return nil, classify(err, NotFound)
	}
	names, readErr := f.Readdirnames(remaining + 1)
	_ = f.Close()
	if readErr != nil && readErr != io.EOF {
		return nil, classify(readErr, NotFound)
	}
	*checked += len(names)
	for _, candidate := range names[:min(len(names), remaining)] {
		if candidate == name {
			info, statErr := root.Lstat(name)
			if statErr != nil {
				return nil, classify(statErr, NotFound)
			}
			return info, nil
		}
	}
	if len(names) > remaining {
		return nil, &Error{Code: LimitExceeded, Message: "workspace path name checks exceeded limit", Details: map[string]string{"limit": "path_name_checks"}}
	}
	return nil, &Error{Code: NotFound, Message: "workspace path does not exist"}
}

func openDirectoryPath(root *os.Root, name string, checked *int) (*os.Root, func(), error) {
	if name == "" {
		return root, func() {}, nil
	}
	current := root
	opened := make([]*os.Root, 0, strings.Count(name, "/")+1)
	cleanup := func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = opened[i].Close()
		}
	}
	for _, component := range strings.Split(name, "/") {
		before, err := exactChild(current, component, checked)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
		if before.Mode()&os.ModeSymlink != 0 {
			cleanup()
			return nil, func() {}, &Error{Code: SymlinkTraversal, Message: "workspace path traverses a symbolic link"}
		}
		if !before.IsDir() {
			cleanup()
			return nil, func() {}, &Error{Code: NotDirectory, Message: "workspace path is not a directory"}
		}
		next, err := current.OpenRoot(component)
		if err != nil {
			cleanup()
			return nil, func() {}, classify(err, NotDirectory)
		}
		handle, err := next.Open(".")
		if err != nil {
			_ = next.Close()
			cleanup()
			return nil, func() {}, classify(err, NotDirectory)
		}
		after, statErr := handle.Stat()
		_ = handle.Close()
		if statErr != nil || !os.SameFile(before, after) {
			_ = next.Close()
			cleanup()
			return nil, func() {}, &Error{Code: SymlinkTraversal, Message: "workspace directory changed during resolution"}
		}
		opened = append(opened, next)
		current = next
	}
	return current, cleanup, nil
}

func openParentPath(root *os.Root, name string, checked *int) (*os.Root, string, func(), error) {
	parts := strings.Split(name, "/")
	parent, cleanup, err := openDirectoryPath(root, strings.Join(parts[:len(parts)-1], "/"), checked)
	if err != nil {
		return nil, "", func() {}, err
	}
	if _, err := exactChild(parent, parts[len(parts)-1], checked); err != nil {
		cleanup()
		return nil, "", func() {}, err
	}
	return parent, parts[len(parts)-1], cleanup, nil
}
func kindOf(m fs.FileMode) protocol.WorkspaceEntryKind {
	if m&os.ModeSymlink != 0 {
		return protocol.WorkspaceEntrySymlink
	}
	if m.IsRegular() {
		return protocol.WorkspaceEntryFile
	}
	if m.IsDir() {
		return protocol.WorkspaceEntryDirectory
	}
	return protocol.WorkspaceEntryOther
}
func fileRevision(workspaceID string, info fs.FileInfo) string {
	h := sha256.New()
	io.WriteString(h, workspaceID)
	fmt.Fprintf(h, "\x00%d\x00%d", info.Size(), info.ModTime().UnixNano())
	device, inode, ctime := hostFileIdentity(info)
	fmt.Fprintf(h, "\x00%d\x00%d\x00%d", device, inode, ctime)
	return "file_" + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func directoryRevision(workspaceID, p string, entries []protocol.WorkspaceDirectoryEntry, truncated bool, truncationReason string, om map[string]int) string {
	h := sha256.New()
	io.WriteString(h, "explore-v1\x00"+workspaceID+"\x00"+p)
	for _, e := range entries {
		io.WriteString(h, "\x00"+e.Name+"\x00"+string(e.Kind)+"\x00"+e.FileRevision+"\x00"+e.ModifiedAt)
		if e.Size != nil {
			fmt.Fprintf(h, "\x00%d", *e.Size)
		}
	}
	fmt.Fprintf(h, "\x00%t\x00%s", truncated, truncationReason)
	for _, omission := range projectOmissions(om) {
		fmt.Fprintf(h, "\x00%s:%d", omission.Reason, omission.Count)
	}
	return "directory_" + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func projectOmissions(m map[string]int) []protocol.WorkspaceOmission {
	out := make([]protocol.WorkspaceOmission, 0, 3)
	for _, reason := range []string{"unsupported_name", "entry_raced", "metadata_unavailable"} {
		if m[reason] > 0 {
			out = append(out, protocol.WorkspaceOmission{Reason: reason, Count: m[reason]})
		}
	}
	return out
}
func readPreview(ctx context.Context, r io.Reader, maxBytes, maxLines int) ([]byte, string, error) {
	limit := maxBytes + utf8.UTFMax + 1
	data := make([]byte, 0, min(limit, 64<<10))
	buf := make([]byte, 32<<10)
	complete := false
	for len(data) < limit {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		n, err := r.Read(buf[:min(len(buf), limit-len(data))])
		data = append(data, buf[:n]...)
		if err == io.EOF {
			complete = true
			break
		}
		if err != nil {
			return nil, "", classify(err, NotFile)
		}
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		return nil, "", &Error{Code: BinaryFile, Message: "binary files cannot be previewed"}
	}
	reason := ""
	if len(data) > maxBytes {
		data = data[:maxBytes]
		reason = "byte_limit"
	}
	if !utf8.Valid(data) {
		if complete {
			return nil, "", &Error{Code: BinaryFile, Message: "binary files cannot be previewed"}
		}
		trimmed := false
		for range utf8.UTFMax - 1 {
			if len(data) == 0 {
				break
			}
			data = data[:len(data)-1]
			if utf8.Valid(data) {
				trimmed = true
				break
			}
		}
		if !trimmed {
			return nil, "", &Error{Code: BinaryFile, Message: "binary files cannot be previewed"}
		}
		reason = "byte_limit"
	}
	if lineCount(data) > maxLines {
		idx := 0
		for n := 0; n < maxLines; n++ {
			next := strings.IndexByte(string(data[idx:]), '\n')
			if next < 0 {
				break
			}
			idx += next + 1
		}
		data = data[:idx]
		reason = "line_limit"
	}
	return data, reason, nil
}
func lineCount(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := strings.Count(string(b), "\n")
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}
func resolveWorkspaceSymlink(root *os.Root, name string) (string, error) {
	pending := strings.Split(name, "/")
	resolved := make([]string, 0, len(pending))
	links := 0
	for len(pending) > 0 {
		component := pending[0]
		pending = pending[1:]
		switch component {
		case "", ".":
			continue
		case "..":
			if len(resolved) == 0 {
				return "", &Error{Code: OutsideWorkspace, Message: "symbolic link leaves the workspace"}
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		candidate := strings.Join(append(append([]string(nil), resolved...), component), "/")
		info, err := root.Lstat(candidate)
		if err != nil {
			return "", classifyFinalSymlink(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 40 {
				return "", &Error{Code: SymlinkTraversal, Message: "symbolic link resolution exceeded limit"}
			}
			target, err := root.Readlink(candidate)
			if err != nil {
				return "", classifyFinalSymlink(err)
			}
			if path.IsAbs(target) {
				return "", &Error{Code: OutsideWorkspace, Message: "symbolic link leaves the workspace"}
			}
			pending = append(strings.Split(target, "/"), pending...)
			continue
		}
		if len(pending) > 0 && !info.IsDir() {
			return "", &Error{Code: NotFile, Message: "symbolic link target has a non-directory component"}
		}
		resolved = append(resolved, component)
	}
	if len(resolved) == 0 {
		return "", &Error{Code: NotFile, Message: "symbolic link does not resolve to a file"}
	}
	return strings.Join(resolved, "/"), nil
}

func classifyFinalSymlink(err error) *Error {
	if errors.Is(err, os.ErrNotExist) {
		return &Error{Code: NotFound, Message: "symbolic-link target does not exist"}
	}
	if errors.Is(err, os.ErrPermission) {
		return &Error{Code: PermissionDenied, Message: "permission denied"}
	}
	return &Error{Code: Unavailable, Message: "workspace filesystem is temporarily unavailable"}
}

func classifyInput(err error, value string, pageSize int) *Error {
	detail := ""
	switch {
	case len(value) > protocol.MaxWorkspacePathBytes:
		detail = "path_bytes"
	case len(strings.Split(value, "/")) > protocol.MaxWorkspacePathComponents:
		detail = "path_components"
	case pageSize > protocol.MaxDirectoryPageSize:
		detail = "page_size"
	}
	if detail != "" {
		return &Error{Code: LimitExceeded, Message: err.Error(), Details: map[string]string{"limit": detail}}
	}
	return &Error{Code: InvalidPath, Message: err.Error()}
}

func classify(err error, wrong protocol.WorkspaceErrorCode) *Error {
	if err == nil {
		return &Error{Code: Unavailable, Message: "workspace is unavailable"}
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &Error{Code: NotFound, Message: "workspace path does not exist"}
	case errors.Is(err, os.ErrPermission):
		return &Error{Code: PermissionDenied, Message: "permission denied"}
	case errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.EISDIR):
		return &Error{Code: wrong, Message: "workspace path has the wrong type"}
	default:
		return &Error{Code: Unavailable, Message: "workspace filesystem is temporarily unavailable"}
	}
}
