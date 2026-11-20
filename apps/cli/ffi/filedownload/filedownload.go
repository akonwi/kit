// Package filedownload reads the text a File tab shows with one download of
// the file (ADR 0074), up to the limits a terminal pane scrolls smoothly at.
// A server that predates downloads, and a file the download can't show as
// text, get the bounded preview read instead, which classifies the file.
package filedownload

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"unicode/utf8"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

const (
	// MaxBytes is the most of a file a pane shows. Scrolling costs a frame
	// time that grows with the lines shown, and these limits hold it to
	// about 12 ms.
	MaxBytes = 2 << 20
	// MaxLines is the most lines a pane shows.
	MaxLines = 65_536
	// attempts is how many times a file that changes while it downloads is
	// downloaded before its last copy is kept.
	attempts = 2
	// sniffBytes is how much of a download is checked before the rest is
	// read.
	sniffBytes = 512
)

// errNotText reports a download that isn't UTF-8 text, or whose first line
// doesn't fit within MaxBytes.
var errNotText = errors.New("download is not text that fits")

// Read is a text file as a File tab shows it.
type Read struct {
	// File is the text as a preview read reports it: Truncated, with
	// "byte_limit" or "line_limit", when it stops before the file's end.
	File protocol.WorkspaceFileRead
	// Unchanged reports that the file still has the revision the caller
	// holds; File is empty.
	Unchanged bool
	// Changed reports that the file changed while it downloaded, so File
	// may not match any one revision of it.
	Changed bool
}

// Workspace reads a workspace file. held is the revision of the copy the
// caller shows, or empty for none.
func Workspace(session *kit.Session, ctx context.Context, input protocol.ReadWorkspaceFileInput, held string) (Read, error) {
	if session == nil {
		return Read{}, errors.New("session is unavailable")
	}
	return workspace(ctx, session, input, held)
}

// Download downloads a text file on the session's server by absolute, "~",
// or "~/…" path. An error that Fallback accepts asks for the bounded host
// read instead, which also shows images. held is as for Workspace.
func Download(ctx context.Context, source Downloader, path, held string) (Read, error) {
	return download(ctx, source, protocol.DownloadFileInput{Path: path}, held)
}

// Fallback reports whether err asks for the bounded read: the server
// predates downloads, or the file isn't text that fits.
func Fallback(err error) bool {
	var unsupported *kit.UnsupportedError
	return errors.Is(err, errNotText) || errors.As(err, &unsupported)
}

// Downloader is the part of a session downloads use; *kit.Session is one.
type Downloader interface {
	DownloadFile(context.Context, protocol.DownloadFileInput, protocol.DownloadConditions) (protocol.FileDownloadResponse, io.ReadCloser, error)
	CheckFile(context.Context, protocol.DownloadFileInput) (protocol.DownloadedFileInfo, error)
}

type workspaceSource interface {
	Downloader
	ReadWorkspaceFile(context.Context, protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error)
}

func workspace(ctx context.Context, source workspaceSource, input protocol.ReadWorkspaceFileInput, held string) (Read, error) {
	read, err := download(ctx, source, protocol.DownloadFileInput{HasWorkspace: true, WorkspaceID: input.WorkspaceID, Path: input.Path}, held)
	if Fallback(err) {
		file, err := source.ReadWorkspaceFile(ctx, input)
		return Read{File: file}, err
	}
	return read, err
}

// download fetches up to MaxBytes of a file and checks afterwards that the
// file still has the revision it downloaded, downloading it once more if
// not. A copy that changed while it downloaded keeps no revision: it can't
// be revalidated or annotated. It is kept if the next download fails.
func download(ctx context.Context, source Downloader, input protocol.DownloadFileInput, held string) (Read, error) {
	var kept Read
	for attempt := range attempts {
		read, known, err := fetch(ctx, source, input, held)
		if err != nil {
			if attempt > 0 && ctx.Err() == nil {
				return kept, nil
			}
			return Read{}, err
		}
		if read.Unchanged || !known {
			return read, nil
		}
		changed, err := changed(ctx, source, input, read.File.Revision)
		if err != nil {
			return Read{}, err
		}
		if !changed {
			return read, nil
		}
		read.Changed = true
		read.File.Revision = ""
		kept = read
		held = ""
	}
	return kept, nil
}

// fetch downloads up to MaxBytes of a file, and reports whether the file
// has a known length and revision.
func fetch(ctx context.Context, source Downloader, input protocol.DownloadFileInput, held string) (Read, bool, error) {
	response, body, err := source.DownloadFile(ctx, input, protocol.DownloadConditions{CachedRevision: held})
	if err != nil {
		return Read{}, false, err
	}
	if response.NotModified {
		return Read{Unchanged: true}, false, nil
	}
	data, err := readText(body)
	body.Close()
	if err != nil {
		return Read{}, false, err
	}
	content, reason, err := text(data)
	if err != nil {
		return Read{}, false, err
	}
	info := response.Info
	file := protocol.WorkspaceFileRead{
		SessionID: info.SessionID, Path: info.Path, Revision: info.Revision, Size: info.Size,
		ModifiedAt: info.ModifiedAt, Encoding: "utf-8", Content: string(content),
		ReturnedBytes: len(content), ReturnedLines: lineCount(content),
		Truncated: reason != "", TruncationReason: reason,
	}
	if input.HasWorkspace {
		file.Workspace = protocol.WorkspaceRef{SessionID: info.SessionID, WorkspaceID: info.WorkspaceID}
	}
	if info.Size < 0 {
		// A file of unknown length that was read to its end has the length
		// read; one cut short has at least what shows.
		file.Size = int64(len(data))
		if reason == "byte_limit" {
			file.Size = int64(len(content))
		}
	}
	return Read{File: file}, info.KnownLength(), nil
}

// changed reports whether a file no longer has revision, as the server
// sees it now: its revision differs, or it or its workspace is gone. A
// check that fails for another reason says nothing, unless ctx ended.
func changed(ctx context.Context, source Downloader, input protocol.DownloadFileInput, revision string) (bool, error) {
	current, err := source.CheckFile(ctx, input)
	if err == nil {
		return current.Revision != revision, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	var failure *protocol.WorkspaceError
	if errors.As(err, &failure) {
		return failure.Code == protocol.WorkspaceErrorStaleWorkspace || failure.Code == protocol.WorkspaceErrorNotFound, nil
	}
	return false, nil
}

// readText reads up to one byte past MaxBytes of body, stopping after its
// first bytes when they aren't text, so an image or other binary file costs
// little before the bounded read classifies it.
func readText(body io.Reader) ([]byte, error) {
	reader := bufio.NewReader(io.LimitReader(body, MaxBytes+1))
	head, err := reader.Peek(sniffBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(head) == sniffBytes {
		// The head may end inside a character.
		for trim := 0; trim < utf8.UTFMax-1 && !utf8.Valid(head); trim++ {
			head = head[:len(head)-1]
		}
	}
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(head) {
		return nil, errNotText
	}
	return io.ReadAll(reader)
}

// text is the part of data a pane shows, and why it stops early, if it
// does. data past MaxBytes is cut at its last line's end.
func text(data []byte) ([]byte, string, error) {
	reason := ""
	if len(data) > MaxBytes {
		end := bytes.LastIndexByte(data[:MaxBytes], '\n')
		if end < 0 {
			return nil, "", errNotText
		}
		data = data[:end+1]
		reason = "byte_limit"
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, "", errNotText
	}
	if lineCount(data) > MaxLines {
		end := 0
		for range MaxLines {
			end += bytes.IndexByte(data[end:], '\n') + 1
		}
		data = data[:end]
		reason = "line_limit"
	}
	return data, reason, nil
}

// lineCount counts lines as preview reads do: a final newline ends the last
// line rather than starting another.
func lineCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	count := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		count++
	}
	return count
}
