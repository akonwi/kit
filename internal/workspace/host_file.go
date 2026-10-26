package workspace

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/attachment"
)

// Host file reads view one file anywhere on the server's machine, outside the
// session's working directory. Any authenticated caller may already run
// commands as the server's user, so the read grants no new authority; it is
// bounded, view-only, and admitted through the same per-session and daemon
// gates as workspace reads. Decoding a full image is admitted separately so a
// burst of image reads cannot multiply its memory cost.

// maxConcurrentImageReads bounds images being read, decoded, or encoded
// into a response across the daemon.
const maxConcurrentImageReads = 2

// maxImageHeaderBytes bounds the bytes read to find an image's dimensions,
// as attachment.InspectImage does.
const maxImageHeaderBytes = 1 << 20

// ReadHostFile returns a bounded text preview or a complete supported image
// for an absolute or home-relative path on this machine. The path is cleaned
// as text, as filepath.Clean does, before symbolic links are followed; the
// final file must be regular. The caller calls done once it has finished
// with the read: an image keeps its decode admission until then, so a
// response encoding it stays within the bound.
func (s *Service) ReadHostFile(ctx context.Context, sessionID string, input protocol.ReadHostFileInput) (protocol.HostFileRead, func(), error) {
	done := func() {}
	if err := input.Validate(); err != nil {
		return protocol.HostFileRead{}, done, hostInputError(err, input.Path)
	}
	name, err := s.expandHome(input.Path)
	if err != nil {
		return protocol.HostFileRead{}, done, err
	}
	release, err := s.acquire(ctx, sessionID)
	if err != nil {
		return protocol.HostFileRead{}, done, err
	}
	defer release()
	// Only a regular file is opened: opening a device or socket can have
	// side effects, or block, even without reading it.
	initial, err := os.Stat(name)
	if err != nil {
		return protocol.HostFileRead{}, done, classifyHost(err)
	}
	if !initial.Mode().IsRegular() {
		return protocol.HostFileRead{}, done, &Error{Code: NotFile, Message: "path is not a regular file"}
	}
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return protocol.HostFileRead{}, done, classifyHost(err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return protocol.HostFileRead{}, done, classifyHost(err)
	}
	if !before.Mode().IsRegular() || !os.SameFile(initial, before) {
		return protocol.HostFileRead{}, done, &Error{Code: Unavailable, Message: "the file moved while it was read; try again"}
	}
	reader := bufio.NewReaderSize(file, 32<<10)
	header, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return protocol.HostFileRead{}, done, classifyHost(err)
	}
	modified := before.ModTime().UTC().Format(time.RFC3339Nano)
	var result protocol.HostFileRead
	if mediaType := strings.Split(http.DetectContentType(header), ";")[0]; len(header) > 0 && supportedImage(mediaType) {
		image, admitted, err := s.readHostImage(ctx, reader, before.Size(), filepath.Base(name), mediaType)
		if err != nil {
			return protocol.HostFileRead{}, done, err
		}
		// The image stays admitted until the caller is done with it, or
		// until the checks below fail.
		done = admitted
		image.SessionID, image.Path, image.ModifiedAt = sessionID, name, modified
		result = protocol.HostFileRead{Payload: image}
	} else {
		data, reason, err := readPreview(ctx, reader, s.limits.MaxPreviewBytes, s.limits.MaxPreviewLines)
		if err != nil {
			var typed *Error
			if errors.As(err, &typed) && typed.Code == BinaryFile {
				return protocol.HostFileRead{}, done, &Error{Code: BinaryFile, Message: "only text files and PNG, JPEG, GIF, or WebP images can be shown"}
			}
			return protocol.HostFileRead{}, done, err
		}
		result = protocol.HostFileRead{Payload: protocol.HostTextFile{
			SessionID: sessionID, Path: name, Size: before.Size(), ModifiedAt: modified, Encoding: "utf-8",
			Content: string(data), ReturnedBytes: len(data), ReturnedLines: lineCount(data), Truncated: reason != "", TruncationReason: reason,
		}}
	}
	after, err := file.Stat()
	if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		done()
		return protocol.HostFileRead{}, func() {}, &Error{Code: Unavailable, Message: "the file changed while it was read; try again"}
	}
	if current, err := os.Stat(name); err != nil || !os.SameFile(before, current) {
		done()
		return protocol.HostFileRead{}, func() {}, &Error{Code: Unavailable, Message: "the file moved while it was read; try again"}
	}
	return result, done, nil
}

// readHostImage reads and validates a complete image under the daemon's
// image admission, which the returned release frees.
func (s *Service) readHostImage(ctx context.Context, reader io.Reader, size int64, filename, mediaType string) (protocol.HostImageFile, func(), error) {
	tooLarge := &Error{Code: LimitExceeded, Message: "image exceeds the size Kit shows", Details: map[string]string{"limit": "image_bytes"}}
	if size > protocol.MaxHostFileImageBytes {
		return protocol.HostImageFile{}, nil, tooLarge
	}
	select {
	case s.images <- struct{}{}:
	case <-ctx.Done():
		return protocol.HostImageFile{}, nil, ctx.Err()
	}
	release := func() { <-s.images }
	image, err := readImage(ctx, reader, filename, mediaType, tooLarge)
	if err != nil {
		release()
		return protocol.HostImageFile{}, nil, err
	}
	return image, sync.OnceFunc(release), nil
}

func readImage(ctx context.Context, reader io.Reader, filename, mediaType string, tooLarge *Error) (protocol.HostImageFile, error) {
	data, err := io.ReadAll(io.LimitReader(reader, protocol.MaxHostFileImageBytes+1))
	if err != nil {
		return protocol.HostImageFile{}, classifyHost(err)
	}
	if len(data) > protocol.MaxHostFileImageBytes {
		return protocol.HostImageFile{}, tooLarge
	}
	if err := ctx.Err(); err != nil {
		return protocol.HostImageFile{}, err
	}
	damaged := &Error{Code: BinaryFile, Message: "image is damaged or not a supported format"}
	// The same header bound as attachment.InspectImage, so both agree.
	configuration, _, err := image.DecodeConfig(io.LimitReader(bytes.NewReader(data), maxImageHeaderBytes))
	if err != nil {
		return protocol.HostImageFile{}, damaged
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || int64(configuration.Width)*int64(configuration.Height) > protocol.MaxHostFileImagePixels {
		return protocol.HostImageFile{}, &Error{Code: LimitExceeded, Message: "image has more pixels than Kit shows", Details: map[string]string{"limit": "image_pixels"}}
	}
	inspected, err := attachment.InspectImage(filename, bytes.NewReader(data), attachment.ImageLimits{MaxBytes: protocol.MaxHostFileImageBytes, MaxPixels: protocol.MaxHostFileImagePixels})
	if err != nil || inspected.MediaType != mediaType || inspected.Validate == nil || inspected.Validate(bytes.NewReader(data)) != nil {
		return protocol.HostImageFile{}, damaged
	}
	return protocol.HostImageFile{Size: int64(len(data)), MediaType: mediaType, Width: inspected.Width, Height: inspected.Height, Data: data}, nil
}

func supportedImage(mediaType string) bool {
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// expandHome returns the absolute, cleaned path a request names.
func (s *Service) expandHome(requested string) (string, error) {
	if requested != "~" && !strings.HasPrefix(requested, "~/") {
		return filepath.Clean(requested), nil
	}
	home, err := s.home()
	if err != nil || !filepath.IsAbs(home) {
		return "", &Error{Code: Unavailable, Message: "the server's home directory is unknown"}
	}
	name := filepath.Clean(filepath.Join(home, strings.TrimPrefix(requested, "~")))
	if protocol.ValidateHostFilePath(name, false) != nil {
		return "", hostInputError(errors.New("host file path is invalid"), name)
	}
	return name, nil
}

func hostInputError(err error, value string) *Error {
	if len(value) > protocol.MaxWorkspacePathBytes {
		return &Error{Code: LimitExceeded, Message: err.Error(), Details: map[string]string{"limit": "path_bytes"}}
	}
	return &Error{Code: InvalidPath, Message: err.Error()}
}

func classifyHost(err error) *Error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &Error{Code: NotFound, Message: "file does not exist"}
	case errors.Is(err, os.ErrPermission):
		return &Error{Code: PermissionDenied, Message: "permission denied"}
	case errors.Is(err, syscall.ENAMETOOLONG):
		return &Error{Code: LimitExceeded, Message: "path is too long", Details: map[string]string{"limit": "path_bytes"}}
	case errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.EISDIR), errors.Is(err, syscall.ELOOP),
		errors.Is(err, syscall.ENXIO), errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.ENODEV):
		return &Error{Code: NotFile, Message: "path is not a regular file"}
	default:
		return &Error{Code: Unavailable, Message: "the file is temporarily unavailable"}
	}
}
