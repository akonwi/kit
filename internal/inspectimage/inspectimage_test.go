package inspectimage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/droids"
)

func TestExecutePersistsImageAndReturnsModelVisibleContent(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	data := testPNG(t, 3, 2)
	if err := os.WriteFile(filepath.Join(cwd, "sample.png"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := execute(t.Context(), Options{SessionID: "session_test", CWD: func() string { return cwd }, Store: store}, arguments{Path: "sample.png"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if text, ok := result.Content[0].(droids.TextContent); !ok || text.Text != "Image loaded for inspection." {
		t.Fatalf("text content = %#v", result.Content[0])
	}
	file, ok := result.Content[1].(droids.FileContent)
	if !ok || file.Filename != "sample.png" || file.MediaType != "image/png" || file.AttachmentID == "" || !strings.HasPrefix(file.URL, "data:image/png;base64,") {
		t.Fatalf("image content = %#v", result.Content[1])
	}
	var details Details
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details.AttachmentID != file.AttachmentID || details.Filename != "sample.png" || details.MediaType != "image/png" || details.Width != 3 || details.Height != 2 {
		t.Fatalf("details = %#v", details)
	}
	_, reader, err := store.Open(t.Context(), "session_test", details.AttachmentID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatalf("stored bytes equal = %v, err = %v", bytes.Equal(stored, data), err)
	}
}

func TestExecuteRejectsMalformedImageAsApplicationError(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "broken.png"), []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := execute(t.Context(), Options{SessionID: "session_test", CWD: func() string { return cwd }, Store: store}, arguments{Path: "broken.png"})
	if err != nil || !result.IsError || len(result.Content) != 1 {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestExecuteRemovesAttachmentWhenPersistedImageCannotBeReopened(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "sample.png"), testPNG(t, 2, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	backing, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	store := &openFailingStore{Store: backing}
	result, err := execute(t.Context(), Options{SessionID: "session_test", CWD: func() string { return cwd }, Store: store}, arguments{Path: "sample.png"})
	if err == nil || result.IsError || store.putID == "" {
		t.Fatalf("result = %#v, err = %v, attachment = %q", result, err, store.putID)
	}
	if _, err := backing.Stat(t.Context(), "session_test", store.putID); !errors.Is(err, attachment.ErrNotFound) {
		t.Fatalf("attachment remains after failure: %v", err)
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var output bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&output, picture); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

type openFailingStore struct {
	attachment.Store
	putID string
}

func (store *openFailingStore) Put(ctx context.Context, input attachment.PutInput) (attachment.Record, error) {
	record, err := store.Store.Put(ctx, input)
	store.putID = record.ID
	return record, err
}

func (store *openFailingStore) Open(context.Context, string, string) (attachment.Record, io.ReadCloser, error) {
	return attachment.Record{}, nil, io.ErrClosedPipe
}

func TestExecuteRemovesAttachmentWhenCanceledDuringPersistedRead(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "sample.png"), testPNG(t, 2, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	backing, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	store := &cancelingReadStore{Store: backing, cancel: cancel}
	result, err := execute(ctx, Options{SessionID: "session_test", CWD: func() string { return cwd }, Store: store}, arguments{Path: "sample.png"})
	if !errors.Is(err, context.Canceled) || result.IsError || store.putID == "" {
		t.Fatalf("result = %#v, err = %v, attachment = %q", result, err, store.putID)
	}
	if _, err := backing.Stat(t.Context(), "session_test", store.putID); !errors.Is(err, attachment.ErrNotFound) {
		t.Fatalf("attachment remains after cancellation: %v", err)
	}
}

type cancelingReadStore struct {
	attachment.Store
	cancel context.CancelFunc
	putID  string
}

func (store *cancelingReadStore) Put(ctx context.Context, input attachment.PutInput) (attachment.Record, error) {
	record, err := store.Store.Put(ctx, input)
	store.putID = record.ID
	return record, err
}

func (store *cancelingReadStore) Open(ctx context.Context, sessionID, attachmentID string) (attachment.Record, io.ReadCloser, error) {
	record, reader, err := store.Store.Open(ctx, sessionID, attachmentID)
	if err != nil {
		return record, reader, err
	}
	return record, &cancelingReader{ReadCloser: reader, cancel: store.cancel}, nil
}

type cancelingReader struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (reader *cancelingReader) Read(data []byte) (int, error) {
	count, err := reader.ReadCloser.Read(data)
	reader.cancel()
	return count, err
}
