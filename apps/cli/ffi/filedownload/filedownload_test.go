package filedownload

import (
	"context"
	"io"
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
	read      int
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

func (s *fakeSession) ReadWorkspaceFile(_ context.Context, input protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	s.read++
	return protocol.WorkspaceFileRead{Path: input.Path, Revision: "file_preview", Content: "preview\n", ReturnedBytes: 8, ReturnedLines: 1}, nil
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

var input = protocol.ReadWorkspaceFileInput{WorkspaceID: "w", Path: "main.go"}

func TestAFileWithinTheLimitsDownloadsWhole(t *testing.T) {
	session := &fakeSession{versions: []version{{"file_1", "package main\n\nfunc main() {}\n"}}}
	read, err := workspace(t.Context(), session, input, "")
	want := protocol.WorkspaceFileRead{
		SessionID: "s", Workspace: protocol.WorkspaceRef{SessionID: "s", WorkspaceID: "w"}, Path: "main.go", Revision: "file_1", Size: 29, ModifiedAt: "2026-01-02T03:04:05Z",
		Encoding: "utf-8", Content: "package main\n\nfunc main() {}\n", ReturnedBytes: 29, ReturnedLines: 3,
	}
	if err != nil || read != (Read{File: want}) || session.downloads != 1 || session.checks != 1 || session.read != 0 {
		t.Fatalf("read = %+v, %v; %d downloads, %d checks, %d preview reads", read, err, session.downloads, session.checks, session.read)
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

func TestAnOlderServerGetsTheBoundedRead(t *testing.T) {
	session := &fakeSession{err: &kit.UnsupportedError{Capability: "downloadFile"}}
	read, err := workspace(t.Context(), session, input, "file_1")
	if err != nil || read.File.Content != "preview\n" || read.File.Revision != "file_preview" || read.Unchanged || session.read != 1 {
		t.Fatalf("read = %+v, %v", read, err)
	}
}

func TestFilesTheDownloadCantShowGetTheBoundedRead(t *testing.T) {
	for name, content := range map[string]string{
		"binary":            "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR" + strings.Repeat("\x00", MaxBytes),
		"not UTF-8":         "caf\xe9\n",
		"one long line":     strings.Repeat("x", MaxBytes+1),
		"invalid past head": strings.Repeat("a", 600) + "\xff",
	} {
		session := &fakeSession{versions: []version{{"file_1", content}}}
		read, err := workspace(t.Context(), session, input, "")
		if err != nil || read.File.Content != "preview\n" || session.read != 1 {
			t.Errorf("%s: read = %+v, %v", name, read.File, err)
		}
		if name == "binary" && session.served > 4096 {
			t.Errorf("binary: the download served %d bytes", session.served)
		}
	}
}

func TestACharacterSplitByTheSniffedHeadIsText(t *testing.T) {
	content := strings.Repeat("a", sniffBytes-1) + "é\n"
	session := &fakeSession{versions: []version{{"file_1", content}}}
	read, err := workspace(t.Context(), session, input, "")
	if err != nil || read.File.Content != content || session.read != 0 {
		t.Fatalf("read = %+v, %v", read.File, err)
	}
}

func TestAHeldRevisionThatIsCurrentIsUnchanged(t *testing.T) {
	session := &fakeSession{versions: []version{{"file_1", "a\n"}}}
	read, err := workspace(t.Context(), session, input, "file_1")
	if err != nil || read != (Read{Unchanged: true}) || session.held[0] != "file_1" || session.checks != 0 {
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
	if _, err := workspace(t.Context(), session, input, ""); err != failure || session.read != 0 {
		t.Fatalf("err = %v", err)
	}
	if Fallback(failure) || !Fallback(errNotText) || !Fallback(&kit.UnsupportedError{}) {
		t.Fatal("Fallback misclassified")
	}
	if _, err := readText(io.MultiReader(strings.NewReader("ab"), &failing{})); err == nil {
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
