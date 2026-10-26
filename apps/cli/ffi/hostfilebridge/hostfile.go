// Package hostfilebridge adapts host file reads (ADR 0050) to the terminal
// client: Ard cannot switch over the HostFileRead union or decode images.
package hostfilebridge

import (
	"context"
	"errors"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/apps/cli/ffi/attachmentbridge"
)

// File is a file on the session's server as a File tab shows it: text as a
// workspace-style read that names no workspace or revision, so its lines
// cannot be annotated, or an image decoded for the terminal.
type File struct {
	// Text is the preview of a text file, with the absolute path that was
	// read.
	Text protocol.WorkspaceFileRead
	// Image reports that the file is an image; Text then carries only the
	// path and size.
	Image bool
	// MediaType, Width, and Height describe the image as stored.
	MediaType string
	Width     int
	Height    int
	// Pixels is the image scaled to fit the requested bounds.
	Pixels attachmentbridge.Pixels
}

// Read reads path, absolute or "~/…", on the session's server. An image is
// scaled to fit within maxWidth by maxHeight pixels.
func Read(session *kit.Session, ctx context.Context, path string, maxWidth, maxHeight int) (File, error) {
	if session == nil {
		return File{}, errors.New("session is unavailable")
	}
	read, err := session.ReadHostFile(ctx, protocol.ReadHostFileInput{Path: path})
	if err != nil {
		return File{}, err
	}
	return project(read, maxWidth, maxHeight)
}

func project(read protocol.HostFileRead, maxWidth, maxHeight int) (File, error) {
	if text := read.Text(); text != nil {
		return File{Text: protocol.WorkspaceFileRead{
			SessionID: text.SessionID, Path: text.Path, Size: text.Size, ModifiedAt: text.ModifiedAt,
			Encoding: text.Encoding, Content: text.Content, ReturnedBytes: text.ReturnedBytes,
			ReturnedLines: text.ReturnedLines, Truncated: text.Truncated, TruncationReason: text.TruncationReason,
		}}, nil
	}
	image := read.Image()
	if image == nil {
		return File{}, errors.New("host file read has no kind")
	}
	pixels, err := attachmentbridge.DecodeBytes(image.Data, maxWidth, maxHeight)
	if err != nil {
		return File{}, err
	}
	return File{
		Text:  protocol.WorkspaceFileRead{SessionID: image.SessionID, Path: image.Path, Size: image.Size, ModifiedAt: image.ModifiedAt},
		Image: true, MediaType: image.MediaType, Width: image.Width, Height: image.Height, Pixels: pixels,
	}, nil
}

// Target is where a path the terminal client shows opens: a workspace file
// relative to the working directory, or a host file by absolute or "~/" path.
type Target struct {
	// Workspace is the workspace-relative path, or "".
	Workspace string
	// Host is the host path, or "".
	Host string
}

// Problem is what a host File tab shows when its file can't be read.
type Problem struct {
	Title  string
	Detail string
}

// Describe returns what a failed host file read shows, or the zero Problem
// for a failure that a workspace read would describe the same way, such as
// a missing file or a canceled read.
func Describe(err error) Problem {
	if errors.Is(err, kit.ErrUnsupported) {
		return Problem{"Server needs an update", "This server's Kit can't open files outside the working directory."}
	}
	var failure *protocol.WorkspaceError
	if !errors.As(err, &failure) {
		return Problem{}
	}
	switch failure.Code {
	case protocol.WorkspaceErrorBinary:
		return Problem{"Can't show this file", "Kit shows text files and PNG, JPEG, GIF, or WebP images."}
	case protocol.WorkspaceErrorLimit:
		if failure.Details["limit"] == "path_bytes" {
			return Problem{"Not a path Kit can open", "The path is too long."}
		}
		return Problem{"Image too large", "Kit shows images up to 16 MiB and 24 megapixels."}
	case protocol.WorkspaceErrorNotFile:
		return Problem{"Not a file", "The path names a directory or another kind of file."}
	case protocol.WorkspaceErrorInvalidPath:
		return Problem{"Not a path Kit can open", "Open an absolute or ~/ path."}
	case protocol.WorkspaceErrorPermissionDenied:
		return Problem{"File is unreadable", "Permission was denied by the server."}
	case protocol.WorkspaceErrorUnavailable:
		return Problem{"File unavailable", "It loads again when this tab is next shown."}
	case protocol.WorkspaceErrorCapacity:
		return Problem{"Server is busy", "It loads again when this tab is next shown."}
	}
	return Problem{}
}
