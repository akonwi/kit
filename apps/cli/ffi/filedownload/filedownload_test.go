package filedownload

import (
	"bytes"
	"context"
	bigendian "encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"reflect"
	"strings"
	"testing"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

// version is a file's content at a revision.
type version struct {
	revision string
	content  string
}

// fakeSession serves downloads of one file from a list of versions: each
// download takes the next, and each check reports the one after it, so a
// test can change the file while it downloads.
type fakeSession struct {
	versions  []version
	downloads int
	checks    int
	held      []string
	err       error
	checkErr  error
	// retryErr fails every download after the first.
	retryErr error
	// cancel, when set, is called as the file is checked.
	cancel context.CancelFunc
	// served counts the bytes the downloads' bodies gave out.
	served int
}

func (s *fakeSession) current(index int) version {
	return s.versions[min(index, len(s.versions)-1)]
}

func (s *fakeSession) DownloadFile(_ context.Context, input protocol.DownloadFileInput, conditions protocol.DownloadConditions) (protocol.FileDownloadResponse, io.ReadCloser, error) {
	if s.err != nil {
		return protocol.FileDownloadResponse{}, nil, s.err
	}
	if s.retryErr != nil && s.downloads > 0 {
		return protocol.FileDownloadResponse{}, nil, s.retryErr
	}
	s.held = append(s.held, conditions.CachedRevision)
	file := s.current(s.downloads)
	s.downloads++
	info := protocol.DownloadedFileInfo{SessionID: "s", WorkspaceID: input.WorkspaceID, Path: input.Path, Revision: file.revision, Size: int64(len(file.content)), ModifiedAt: "2026-01-02T03:04:05Z"}
	if file.revision == "" {
		info.Size = -1
	}
	if conditions.CachedRevision != "" && conditions.CachedRevision == file.revision {
		return protocol.FileDownloadResponse{Info: info, NotModified: true}, nil, nil
	}
	return protocol.FileDownloadResponse{Info: info}, io.NopCloser(&counted{reader: strings.NewReader(file.content), count: &s.served}), nil
}

func (s *fakeSession) CheckFile(ctx context.Context, _ protocol.DownloadFileInput) (protocol.DownloadedFileInfo, error) {
	s.checks++
	if s.cancel != nil {
		s.cancel()
		return protocol.DownloadedFileInfo{}, ctx.Err()
	}
	if s.checkErr != nil {
		return protocol.DownloadedFileInfo{}, s.checkErr
	}
	return protocol.DownloadedFileInfo{Revision: s.current(s.downloads).revision}, nil
}

type counted struct {
	reader io.Reader
	count  *int
}

func (c *counted) Read(buffer []byte) (int, error) {
	n, err := c.reader.Read(buffer)
	*c.count += n
	return n, err
}

var input = protocol.DownloadFileInput{HasWorkspace: true, WorkspaceID: "w", Path: "main.go"}

// workspace downloads the workspace file at input as a workspace File tab
// does, refusing images as binary.
func workspace(ctx context.Context, session Downloader, input protocol.DownloadFileInput, held string) (Read, error) {
	return Download(ctx, session, input, held, nil)
}

// binary reports whether err refuses a file as binary.
func binary(err error) bool {
	var failure *protocol.WorkspaceError
	return errors.As(err, &failure) && failure.Code == protocol.WorkspaceErrorBinary
}

func TestAFileWithinTheLimitsDownloadsWhole(t *testing.T) {
	session := &fakeSession{versions: []version{{"file_1", "package main\n\nfunc main() {}\n"}}}
	read, err := workspace(t.Context(), session, input, "")
	want := protocol.WorkspaceFileRead{
		SessionID: "s", Workspace: protocol.WorkspaceRef{SessionID: "s", WorkspaceID: "w"}, Path: "main.go", Revision: "file_1", Size: 29, ModifiedAt: "2026-01-02T03:04:05Z",
		Encoding: "utf-8", Content: "package main\n\nfunc main() {}\n", ReturnedBytes: 29, ReturnedLines: 3,
	}
	if err != nil || read.File != want || read.Image != nil || read.Changed || session.downloads != 1 || session.checks != 1 {
		t.Fatalf("read = %+v, %v; %d downloads, %d checks", read, err, session.downloads, session.checks)
	}
}

func TestAFilePastTheByteLimitStopsAtTheLastLineThatFits(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n"
	content := strings.Repeat(line, MaxBytes/len(line)+10)
	session := &fakeSession{versions: []version{{"file_1", content}}}
	read, err := workspace(t.Context(), session, input, "")
	lines := MaxBytes / len(line)
	if err != nil || read.File.Content != content[:lines*len(line)] || read.File.ReturnedBytes != lines*len(line) ||
		read.File.ReturnedLines != lines || !read.File.Truncated || read.File.TruncationReason != "byte_limit" || read.File.Size != int64(len(content)) {
		t.Fatalf("read = %d bytes, %d lines, %q, size %d, %v", read.File.ReturnedBytes, read.File.ReturnedLines, read.File.TruncationReason, read.File.Size, err)
	}
	if session.served > MaxBytes+1+4096 {
		t.Fatalf("the download served %d bytes", session.served)
	}
}

func TestAFilePastTheLineLimitStopsAtTheLimit(t *testing.T) {
	session := &fakeSession{versions: []version{{"file_1", strings.Repeat("\n", MaxLines+5)}}}
	read, err := workspace(t.Context(), session, input, "")
	if err != nil || read.File.Content != strings.Repeat("\n", MaxLines) || read.File.ReturnedLines != MaxLines ||
		read.File.TruncationReason != "line_limit" || read.File.Size != MaxLines+5 {
		t.Fatalf("read = %d lines, %q, %v", read.File.ReturnedLines, read.File.TruncationReason, err)
	}
}

func TestAnOlderServerIsUnsupported(t *testing.T) {
	session := &fakeSession{err: &kit.UnsupportedError{Capability: "downloadFile"}}
	if _, err := workspace(t.Context(), session, input, "file_1"); !Unsupported(err) {
		t.Fatalf("err = %v, want unsupported", err)
	}
	if Unsupported(errBinary()) {
		t.Fatal("a binary file counted as an older server")
	}
}

func TestFilesThatArentTextAreBinary(t *testing.T) {
	for name, content := range map[string]string{
		"binary":             "\x00\x01\x02" + strings.Repeat("\x00", MaxBytes),
		"not UTF-8":          "caf\xe9\n",
		"invalid past head":  strings.Repeat("a", 600) + "\xff",
		"ends mid-character": strings.Repeat("a", 600) + "\xc3",
		"an image":           "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR",
	} {
		session := &fakeSession{versions: []version{{"file_1", content}}}
		if read, err := workspace(t.Context(), session, input, ""); !binary(err) {
			t.Errorf("%s: read = %+v, %v", name, read.File, err)
		}
		if name == "binary" && session.served > 4096 {
			t.Errorf("binary: the download served %d bytes", session.served)
		}
	}
}

func TestAFirstLinePastTheByteLimitIsCutAtACharacter(t *testing.T) {
	content := strings.Repeat("x", MaxBytes-1) + "é" + strings.Repeat("y", 10)
	session := &fakeSession{versions: []version{{"file_1", content}}}
	read, err := workspace(t.Context(), session, input, "")
	if err != nil || read.File.Content != strings.Repeat("x", MaxBytes-1) || read.File.ReturnedLines != 1 ||
		!read.File.Truncated || read.File.TruncationReason != "byte_limit" {
		t.Fatalf("read = %d bytes, %q, %v", read.File.ReturnedBytes, read.File.TruncationReason, err)
	}
}

func TestImagesDownloadWholeWhenAskedFor(t *testing.T) {
	// A PNG past the text limit: an image with trailing bytes.
	large := pngOf(t, 2, 1) + strings.Repeat("\x00", MaxBytes+10)
	session := &fakeSession{versions: []version{{"hostfile_1", large}}}
	read, err := Download(t.Context(), session, protocol.DownloadFileInput{Path: "/tmp/shot.png"}, "", bounds)
	if err != nil || string(read.Image) != large || read.MediaType != "image/png" || read.File.Path != "/tmp/shot.png" ||
		read.File.Size != int64(len(large)) || read.File.Revision != "hostfile_1" {
		t.Fatalf("read = %d bytes, %q, %+v, %v", len(read.Image), read.MediaType, read.File, err)
	}
	huge := &fakeSession{versions: []version{{"hostfile_1", "GIF89a" + strings.Repeat("\x00", MaxImageBytes)}}}
	var failure *protocol.WorkspaceError
	if _, err := Download(t.Context(), huge, protocol.DownloadFileInput{Path: "/tmp/big.gif"}, "", bounds); !errors.As(err, &failure) ||
		failure.Code != protocol.WorkspaceErrorLimit {
		t.Fatalf("a huge image: err = %v", err)
	}
}

func TestImageFormatsAreKnownByTheirBytes(t *testing.T) {
	for want, data := range map[string]string{
		"image/png":  "\x89PNG\r\n\x1a\nrest",
		"image/jpeg": "\xff\xd8\xff\xe0",
		"image/gif":  "GIF87a",
		"image/webp": "RIFF\x00\x00\x00\x00WEBPVP8 ",
		"":           "RIFF\x00\x00\x00\x00WAVE",
	} {
		if got := imageType([]byte(data)); got != want {
			t.Errorf("imageType(%q) = %q, want %q", data, got, want)
		}
	}
}

func TestAHeldRevisionThatIsCurrentIsUnchanged(t *testing.T) {
	session := &fakeSession{versions: []version{{"file_1", "a\n"}}}
	read, err := workspace(t.Context(), session, input, "file_1")
	if err != nil || !reflect.DeepEqual(read, Read{Unchanged: true}) || session.held[0] != "file_1" || session.checks != 0 {
		t.Fatalf("read = %+v, %v; held %v", read, err, session.held)
	}
}

func TestAFileThatChangesWhileItDownloadsDownloadsAgain(t *testing.T) {
	session := &fakeSession{versions: []version{{"file_1", "one\n"}, {"file_2", "two\n"}}}
	read, err := workspace(t.Context(), session, input, "file_0")
	if err != nil || read.File.Content != "two\n" || read.File.Revision != "file_2" || read.Changed || session.downloads != 2 {
		t.Fatalf("read = %+v, %v; %d downloads", read, err, session.downloads)
	}
	if want := []string{"file_0", ""}; session.held[0] != want[0] || session.held[1] != want[1] {
		t.Fatalf("held = %q", session.held)
	}
}

func TestAFileThatKeepsChangingKeepsItsLastCopyMarkedChanged(t *testing.T) {
	session := &fakeSession{versions: []version{{"file_1", "one\n"}, {"file_2", "two\n"}, {"file_3", "three\n"}}}
	read, err := workspace(t.Context(), session, input, "")
	if err != nil || read.File.Content != "two\n" || read.File.Revision != "" || !read.Changed || session.downloads != 2 {
		t.Fatalf("read = %+v, %v; %d downloads", read, err, session.downloads)
	}
}

func TestAFileGoneAfterItsDownloadIsChanged(t *testing.T) {
	session := &fakeSession{
		versions: []version{{"file_1", "one\n"}},
		checkErr: &protocol.WorkspaceError{Code: protocol.WorkspaceErrorNotFound, Message: "gone"},
	}
	read, err := workspace(t.Context(), session, input, "")
	if err != nil || read.File.Content != "one\n" || read.File.Revision != "" || !read.Changed || session.downloads != 2 {
		t.Fatalf("read = %+v, %v", read, err)
	}
	gone := &protocol.WorkspaceError{Code: protocol.WorkspaceErrorNotFound, Message: "gone"}
	session = &fakeSession{versions: []version{{"file_1", "one\n"}}, checkErr: gone, retryErr: gone}
	read, err = workspace(t.Context(), session, input, "")
	if err != nil || read.File.Content != "one\n" || read.File.Revision != "" || !read.Changed {
		t.Fatalf("failed retry: read = %+v, %v", read, err)
	}
	session = &fakeSession{
		versions: []version{{"file_1", "one\n"}},
		checkErr: &protocol.WorkspaceError{Code: protocol.WorkspaceErrorCapacity, Message: "busy"},
	}
	read, err = workspace(t.Context(), session, input, "")
	if err != nil || read.File.Content != "one\n" || read.Changed || session.downloads != 1 {
		t.Fatalf("busy check: read = %+v, %v", read, err)
	}
}

func TestAFileWithoutARevisionIsntChecked(t *testing.T) {
	session := &fakeSession{versions: []version{{"", "cpu 1\n"}}}
	read, err := workspace(t.Context(), session, input, "")
	if err != nil || read.File.Content != "cpu 1\n" || read.File.Size != 6 || read.File.Revision != "" || session.checks != 0 {
		t.Fatalf("read = %+v, %v", read, err)
	}
}

func TestOtherFailuresAreReported(t *testing.T) {
	failure := &protocol.WorkspaceError{Code: protocol.WorkspaceErrorStaleWorkspace, Message: "stale"}
	session := &fakeSession{err: failure}
	if _, err := workspace(t.Context(), session, input, ""); err != failure {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := readBody(io.MultiReader(strings.NewReader("ab"), &failing{}), false); err == nil {
		t.Fatal("a failed body read was not reported")
	}
}

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestACheckCutShortByCancellationFails(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	session := &fakeSession{versions: []version{{"file_1", "one\n"}}, cancel: cancel}
	if read, err := workspace(ctx, session, input, ""); err != context.Canceled {
		t.Fatalf("read = %+v, %v", read, err)
	}
}

func TestAnUnknownLengthCutShortReportsOnlyWhatShows(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n"
	session := &fakeSession{versions: []version{{"", strings.Repeat(line, MaxBytes/len(line)+10)}}}
	read, err := workspace(t.Context(), session, input, "")
	shown := MaxBytes / len(line) * len(line)
	if err != nil || read.File.ReturnedBytes != shown || read.File.Size != int64(shown) || read.File.TruncationReason != "byte_limit" {
		t.Fatalf("read = %d of %d bytes, %q, %v", read.File.ReturnedBytes, read.File.Size, read.File.TruncationReason, err)
	}
}

var bounds = &ImageBounds{Width: 100, Height: 100}

// pngOf is a PNG of width by height pixels as its header says; only a 2 by
// 1 image has pixels to match.
func pngOf(t *testing.T, width, height uint32) string {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	// The IHDR chunk's data starts at 16; its CRC covers its type and data.
	bigendian.BigEndian.PutUint32(data[16:], width)
	bigendian.BigEndian.PutUint32(data[20:], height)
	bigendian.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	return string(data)
}

func TestImagesAreCheckedBeforeTheyAreShown(t *testing.T) {
	host := protocol.DownloadFileInput{Path: "/tmp/shot.png"}
	read, err := Download(t.Context(), &fakeSession{versions: []version{{"hostfile_1", pngOf(t, 2, 1)}}}, host, "", bounds)
	if err != nil || read.Width != 2 || read.Height != 1 || read.MediaType != "image/png" {
		t.Fatalf("read = %dx%d %q, %v", read.Width, read.Height, read.MediaType, err)
	}
	var failure *protocol.WorkspaceError
	_, err = Download(t.Context(), &fakeSession{versions: []version{{"hostfile_1", pngOf(t, 10_000, 10_000)}}}, host, "", bounds)
	if !errors.As(err, &failure) || failure.Code != protocol.WorkspaceErrorLimit {
		t.Fatalf("a huge image: %v", err)
	}
	if _, err := Download(t.Context(), &fakeSession{versions: []version{{"hostfile_1", "\x89PNG\r\n\x1a\nbroken"}}}, host, "", bounds); !binary(err) {
		t.Fatalf("a broken image: %v", err)
	}
}

func TestARetryWithAnImageThatCantShowKeepsTheChangedCopy(t *testing.T) {
	host := protocol.DownloadFileInput{Path: "/tmp/shot.png"}
	first := pngOf(t, 2, 1)
	for name, retry := range map[string]string{
		"broken":         "\x89PNG\r\n\x1a\nbroken",
		"too large":      pngOf(t, 10_000, 10_000),
		"corrupt pixels": corruptPixels(t),
	} {
		session := &fakeSession{versions: []version{{"hostfile_1", first}, {"hostfile_2", retry}}}
		read, err := Download(t.Context(), session, host, "", bounds)
		if err != nil || string(read.Image) != first || !read.Changed || read.File.Revision != "" || session.downloads != 2 {
			t.Errorf("%s: read = %d bytes, changed %t, %q, %v", name, len(read.Image), read.Changed, read.File.Revision, err)
		}
	}
}

// corruptPixels is a PNG whose header is sound but whose pixel data isn't.
func corruptPixels(t *testing.T) string {
	data := []byte(pngOf(t, 2, 1))
	start := bytes.Index(data, []byte("IDAT")) + 4
	for index := start; index < start+8; index++ {
		data[index] ^= 0xFF
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		t.Fatalf("the header doesn't decode: %v", err)
	}
	return string(data)
}
