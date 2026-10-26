package hostfilebridge

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
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

func TestProjectMakesTextUnannotatableAndScalesImages(t *testing.T) {
	text, err := project(protocol.HostFileRead{Payload: protocol.HostTextFile{
		SessionID: "s", Path: "/etc/hosts", Size: 10, Encoding: "utf-8", Content: "127.0.0.1\n", ReturnedBytes: 10, ReturnedLines: 1,
	}}, 100, 100)
	if err != nil || text.Image || text.Text.Path != "/etc/hosts" || text.Text.Content != "127.0.0.1\n" || text.Text.Revision != "" || text.Text.Size != 10 {
		t.Fatalf("text = %+v, %v", text, err)
	}
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
	read := protocol.HostFileRead{Payload: protocol.HostImageFile{
		SessionID: "s", Path: "/tmp/wide.png", Size: int64(encoded.Len()), MediaType: "image/png", Width: 400, Height: 100, Data: encoded.Bytes(),
	}}
	file, err := project(read, 100, 100)
	if err != nil || !file.Image || file.Width != 400 || file.Height != 100 || file.Pixels.Width != 100 || file.Pixels.Height != 25 ||
		file.Text.Path != "/tmp/wide.png" || file.MediaType != "image/png" {
		t.Fatalf("image = %+v, %v", file.Text, err)
	}
	if _, err := project(protocol.HostFileRead{}, 100, 100); err == nil {
		t.Fatal("a read without a kind was projected")
	}
}

func TestDescribeExplainsHostReadFailures(t *testing.T) {
	for err, want := range map[error]Problem{
		&kit.UnsupportedError{Capability: "readHostFile"}:                                                                               {"Server needs an update", "This server's Kit can't open files outside the working directory."},
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
