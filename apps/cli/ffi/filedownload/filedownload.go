// Package filedownload reads the text a File tab shows with one download of
// the file (ADR 0074), up to the limits a terminal pane scrolls smoothly at.
// It classifies what it downloads: UTF-8 text, a supported image where the
// caller shows images, or a binary file it refuses.
package filedownload

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"unicode/utf8"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/apps/cli/ffi/attachmentbridge"
)

const (
	// MaxBytes is the most of a text file a pane shows. Scrolling costs a
	// frame time that grows with the lines shown, and these limits hold it
	// to about 12 ms.
	MaxBytes = 2 << 20
	// MaxLines is the most lines a pane shows.
	MaxLines = 65_536
	// MaxImageBytes and MaxImagePixels bound the images a pane shows.
	MaxImageBytes  = 16 << 20
	MaxImagePixels = 24_000_000
	// attempts is how many times a file that changes while it downloads is
	// downloaded before its last copy is kept.
	attempts = 2
	// sniffBytes is how much of a download is checked before the rest is
	// read.
	sniffBytes = 512
)

// File is the text of a file a File tab shows. Truncated, with
// "byte_limit" or "line_limit", says it stops before the file's end. A
// workspace file's Workspace names only its session and workspace; a host
// file's is empty.
type File struct {
	SessionID        string
	Workspace        protocol.WorkspaceRef
	Path             string
	Revision         string
	Size             int64
	ModifiedAt       string
	Encoding         string
	Content          string
	ReturnedBytes    int
	ReturnedLines    int
	Truncated        bool
	TruncationReason string
}

// Read is a file as a File tab shows it.
type Read struct {
	// File is the file's text. An image's File carries only its path and
	// size.
	File File
	// Image is a supported image's encoded bytes, when the caller asked for
	// images and the file is one; MediaType names its format, Width and
	// Height its size in pixels, and Pixels the image decoded to fit the
	// caller's bounds.
	Image         []byte
	MediaType     string
	Width, Height int
	Pixels        attachmentbridge.Pixels
	// Unchanged reports that the file still has the revision the caller
	// holds; File is empty.
	Unchanged bool
	// Changed reports that the file changed while it downloaded, so File
	// may not match any one revision of it. It has no revision.
	Changed bool
}

// Workspace reads a workspace file. held is the revision of the copy the
// caller shows, or empty for none. A binary file fails with a
// binary_file WorkspaceError, and a server that predates downloads with an
// UnsupportedError.
func Workspace(session *kit.Session, ctx context.Context, input protocol.DownloadFileInput, held string) (Read, error) {
	if session == nil {
		return Read{}, errors.New("session is unavailable")
	}
	return Download(ctx, session, protocol.DownloadFileInput{HasWorkspace: true, WorkspaceID: input.WorkspaceID, Path: input.Path}, held, nil)
}

// Unsupported reports whether err says the server predates downloads.
func Unsupported(err error) bool {
	return errors.Is(err, kit.ErrUnsupported)
}

// ImageBounds are the pixels a caller shows an image within.
type ImageBounds struct {
	Width, Height int
}

// Downloader is the part of a session downloads use; *kit.Session is one.
type Downloader interface {
	DownloadFile(context.Context, protocol.DownloadFileInput, protocol.DownloadConditions) (protocol.FileDownloadResponse, io.ReadCloser, error)
	CheckFile(context.Context, protocol.DownloadFileInput) (protocol.DownloadedFileInfo, error)
}

// Download fetches a file and checks afterwards that it still has the
// revision it downloaded, downloading it once more if not. A copy that
// changed while it downloaded keeps no revision: it can't be revalidated or
// annotated. It is kept if the next download fails. images asks for
// supported images, decoded to fit its bounds, rather than refusing them as
// binary.
func Download(ctx context.Context, source Downloader, input protocol.DownloadFileInput, held string, images *ImageBounds) (Read, error) {
	var kept Read
	for attempt := range attempts {
		read, known, err := fetch(ctx, source, input, held, images)
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

// fetch downloads a file, and reports whether it has a known length and
// revision.
func fetch(ctx context.Context, source Downloader, input protocol.DownloadFileInput, held string, images *ImageBounds) (Read, bool, error) {
	response, body, err := source.DownloadFile(ctx, input, protocol.DownloadConditions{CachedRevision: held})
	if err != nil {
		return Read{}, false, err
	}
	if response.NotModified {
		return Read{Unchanged: true}, false, nil
	}
	data, mediaType, err := readBody(body, images != nil)
	body.Close()
	if err != nil {
		return Read{}, false, err
	}
	info := response.Info
	file := File{
		SessionID: info.SessionID, Path: info.Path, Revision: info.Revision, Size: info.Size, ModifiedAt: info.ModifiedAt,
	}
	if input.HasWorkspace {
		file.Workspace = protocol.WorkspaceRef{SessionID: info.SessionID, WorkspaceID: info.WorkspaceID}
	}
	if mediaType != "" {
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return Read{}, false, errBinary()
		}
		if config.Width <= 0 || config.Height <= 0 || config.Width > MaxImagePixels/config.Height {
			return Read{}, false, &protocol.WorkspaceError{Code: protocol.WorkspaceErrorLimit, Message: "image is too large", Details: map[string]string{"limit": "image_pixels"}}
		}
		// The whole image is decoded here, so a copy that can't show fails
		// the download rather than replacing one that can.
		pixels, err := attachmentbridge.DecodeBytes(data, images.Width, images.Height)
		if err != nil {
			return Read{}, false, errBinary()
		}
		if info.Size < 0 {
			file.Size = int64(len(data))
		}
		return Read{File: file, Image: data, MediaType: mediaType, Width: config.Width, Height: config.Height, Pixels: pixels}, info.KnownLength(), nil
	}
	content, reason := text(data)
	file.Encoding, file.Content = "utf-8", string(content)
	file.ReturnedBytes, file.ReturnedLines = len(content), lineCount(content)
	file.Truncated, file.TruncationReason = reason != "", reason
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

// readBody reads a download, judging it by its first bytes: a supported
// image, when images is set, up to one byte past MaxImageBytes; otherwise
// UTF-8 text up to one byte past MaxBytes. Anything else stops after its
// first bytes as binary. It returns the image's media type, or "" for text.
func readBody(body io.Reader, images bool) ([]byte, string, error) {
	reader := bufio.NewReaderSize(body, sniffBytes)
	head, err := reader.Peek(sniffBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, "", err
	}
	if mediaType := imageType(head); images && mediaType != "" {
		data, err := io.ReadAll(io.LimitReader(reader, MaxImageBytes+1))
		if err != nil {
			return nil, "", err
		}
		if len(data) > MaxImageBytes {
			return nil, "", &protocol.WorkspaceError{Code: protocol.WorkspaceErrorLimit, Message: "image is too large", Details: map[string]string{"limit": "image_bytes"}}
		}
		return data, mediaType, nil
	}
	if len(head) == sniffBytes {
		// The head may end inside a character.
		for trim := 0; trim < utf8.UTFMax-1 && !utf8.Valid(head); trim++ {
			head = head[:len(head)-1]
		}
	}
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(head) {
		return nil, "", errBinary()
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	// Only what can show is judged; a copy cut at the limit may end inside
	// a character.
	shown := data
	if len(shown) > MaxBytes {
		shown = cutAtCharacter(shown[:MaxBytes])
	}
	if bytes.IndexByte(shown, 0) >= 0 || !utf8.Valid(shown) {
		return nil, "", errBinary()
	}
	return data, "", nil
}

func errBinary() error {
	return &protocol.WorkspaceError{Code: protocol.WorkspaceErrorBinary, Message: "binary files cannot be shown"}
}

// imageType is the supported image format data begins, or "".
func imageType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// cutAtCharacter drops a character that data's last bytes begin but don't
// finish, as a download cut at a byte limit may leave.
func cutAtCharacter(data []byte) []byte {
	for trim := 0; trim < utf8.UTFMax-1 && len(data) > 0 && !utf8.Valid(data); trim++ {
		data = data[:len(data)-1]
	}
	return data
}

// text is the part of valid UTF-8 data a pane shows, and why it stops
// early, if it does. data past MaxBytes is cut at its last line's end in
// MaxBytes, or, when its first line doesn't fit, at its last whole
// character there.
func text(data []byte) ([]byte, string) {
	reason := ""
	if len(data) > MaxBytes {
		data = data[:MaxBytes]
		if end := bytes.LastIndexByte(data, '\n'); end >= 0 {
			data = data[:end+1]
		} else {
			data = cutAtCharacter(data)
		}
		reason = "byte_limit"
	}
	if lineCount(data) > MaxLines {
		end := 0
		for range MaxLines {
			end += bytes.IndexByte(data[end:], '\n') + 1
		}
		data = data[:end]
		reason = "line_limit"
	}
	return data, reason
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
