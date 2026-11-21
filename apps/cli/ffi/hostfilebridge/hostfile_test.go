package hostfilebridge

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

func TestResolveOpensWorkspaceFilesInsideTheDirectoryAndHostFilesOutside(t *testing.T) {
	for _, test := range []struct {
		cwd, path string
		want      Target
	}{
		{"/repo", "/repo/cmd/main.go", Target{Workspace: "cmd/main.go"}},
		{"/repo", "cmd/main.go", Target{Workspace: "cmd/main.go"}},
		{"/repo", "/var/log/system.log", Target{Host: "/var/log/system.log"}},
		{"/repo", "../other/b.go", Target{Host: "/other/b.go"}},
		{"/repo", "/repo/../etc//hosts", Target{Host: "/etc/hosts"}},
		{"/repo", "~/notes.md", Target{Host: "~/notes.md"}},
		{"", "/etc/hosts", Target{Host: "/etc/hosts"}},
		{"", "notes.md", Target{}},
		{"/repo", "  ", Target{}},
		{"/repo", "/a\x00b", Target{}},
	} {
		if got := Resolve(test.cwd, test.path); got != test.want {
			t.Errorf("Resolve(%q, %q) = %+v, want %+v", test.cwd, test.path, got, test.want)
		}
	}
}

func TestTypedPathsLoseQuotesAndShellEscapes(t *testing.T) {
	for text, want := range map[string]string{
		"  /var/log/app.log\n":          "/var/log/app.log",
		"'/Users/me/My File.txt'":       "/Users/me/My File.txt",
		`/Users/me/My\ File\ \(1\).txt`: "/Users/me/My File (1).txt",
		`"~/notes.md"`:                  "~/notes.md",
		"cmd/main":                      "cmd/main",
		"/a\nb":                         "",
	} {
		if got := Typed(text); got != want {
			t.Errorf("Typed(%q) = %q, want %q", text, got, want)
		}
	}
	for text, want := range map[string]bool{"/etc/hosts": true, "~/a": true, "~": true, " '/tmp/x' ": true, "cmd/main.go": false, "main": false} {
		if IsPath(text) != want {
			t.Errorf("IsPath(%q) = %t", text, !want)
		}
	}
}

func TestDescribeExplainsHostReadFailures(t *testing.T) {
	for err, want := range map[error]Problem{
		&kit.UnsupportedError{Capability: "readHostFile"}:                                                                               {"Server needs an update", "This server's Kit can't download files."},
		&protocol.WorkspaceError{Code: protocol.WorkspaceErrorBinary, Message: "x"}:                                                     {"Can't show this file", "Kit shows text files and PNG, JPEG, GIF, or WebP images."},
		&protocol.WorkspaceError{Code: protocol.WorkspaceErrorNotFile, Message: "x"}:                                                    {"Not a file", "The path names a directory or another kind of file."},
		&protocol.WorkspaceError{Code: protocol.WorkspaceErrorPermissionDenied, Message: "x"}:                                           {"File is unreadable", "Permission was denied by the server."},
		&protocol.WorkspaceError{Code: protocol.WorkspaceErrorLimit, Message: "x", Details: map[string]string{"limit": "image_pixels"}}: {"Image too large", "Kit shows images up to 16 MiB and 24 megapixels."},
		&protocol.WorkspaceError{Code: protocol.WorkspaceErrorNotFound, Message: "x"}:                                                   {},
		errors.New("context canceled"): {},
	} {
		if got := Describe(err); got != want {
			t.Errorf("Describe(%v) = %+v, want %+v", err, got, want)
		}
	}
}

// hostSession downloads one file's content.
type hostSession struct {
	content string
}

func (s *hostSession) DownloadFile(_ context.Context, input protocol.DownloadFileInput, conditions protocol.DownloadConditions) (protocol.FileDownloadResponse, io.ReadCloser, error) {
	info := protocol.DownloadedFileInfo{SessionID: "s", Path: "/home/me/notes.txt", Revision: "hostfile_1", Size: int64(len(s.content))}
	if input.HasWorkspace || input.Path != "~/notes.txt" {
		return protocol.FileDownloadResponse{}, nil, errors.New("unexpected input")
	}
	if conditions.CachedRevision == info.Revision {
		return protocol.FileDownloadResponse{Info: info, NotModified: true}, nil, nil
	}
	return protocol.FileDownloadResponse{Info: info}, io.NopCloser(strings.NewReader(s.content)), nil
}

func (s *hostSession) CheckFile(context.Context, protocol.DownloadFileInput) (protocol.DownloadedFileInfo, error) {
	return protocol.DownloadedFileInfo{Revision: "hostfile_1"}, nil
}

func TestReadDownloadsTextWithARevisionItCantBeAnnotatedAt(t *testing.T) {
	session := &hostSession{content: "a\nb\n"}
	file, err := read(t.Context(), session, "~/notes.txt", 100, 100, "")
	if err != nil || file.Text.Content != "a\nb\n" || file.Text.Path != "/home/me/notes.txt" || file.Text.Revision != "" ||
		file.Revision != "hostfile_1" || file.Text.ReturnedLines != 2 || file.Image {
		t.Fatalf("file = %+v, %v", file, err)
	}
	file, err = read(t.Context(), session, "~/notes.txt", 100, 100, "hostfile_1")
	if err != nil || !file.Unchanged || file.Text.Content != "" {
		t.Fatalf("revalidated = %+v, %v", file, err)
	}
}

func TestReadDecodesAnImageToFitItsBounds(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 400, 100))
	for x := range 400 {
		for y := range 100 {
			picture.Set(x, y, color.NRGBA{B: 255, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	file, err := read(t.Context(), &hostSession{content: encoded.String()}, "~/notes.txt", 100, 100, "")
	if err != nil || !file.Image || file.Width != 400 || file.Height != 100 || file.Pixels.Width != 100 || file.Pixels.Height != 25 ||
		file.Text.Path != "/home/me/notes.txt" || file.Text.Size != int64(encoded.Len()) || file.MediaType != "image/png" || file.Revision != "hostfile_1" {
		t.Fatalf("image = %+v, %v", file.Text, err)
	}
}

func TestReadRefusesWhatIsntTextOrAnImage(t *testing.T) {
	for name, content := range map[string]string{
		"binary":             "\x7fELF\x02\x01\x01\x00",
		"an undecodable PNG": "\x89PNG\r\n\x1a\n\x00\x00",
	} {
		_, err := read(t.Context(), &hostSession{content: content}, "~/notes.txt", 100, 100, "")
		if got := Describe(err); got != (Problem{"Can't show this file", "Kit shows text files and PNG, JPEG, GIF, or WebP images."}) {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
}
