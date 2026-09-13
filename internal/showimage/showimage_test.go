package showimage

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

func TestExecutePersistsRelativeImageAndReturnsTypedPresentation(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	path := filepath.Join(cwd, "sample.png")
	data := testPNG(t, 3, 2)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := execute(context.Background(), Options{SessionID: "session_test", CWD: func() string { return cwd }, Store: store}, arguments{
		Path: "sample.png", Caption: "A small sample",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 2 {
		t.Fatalf("result = %#v, want successful text and image content", result)
	}
	file, ok := result.Content[1].(droids.FileContent)
	if !ok || file.Filename != "sample.png" || file.MediaType != "image/png" || file.AttachmentID == "" || !strings.HasPrefix(file.URL, "data:image/png;base64,") {
		t.Fatalf("image content = %#v", result.Content[1])
	}
	var details Details
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details.Presentation != Presentation || details.Filename != "sample.png" || details.MediaType != "image/png" ||
		details.Width != 3 || details.Height != 2 || details.Caption != "A small sample" || details.AttachmentID == "" {
		t.Fatalf("details = %#v", details)
	}
	if parsed, ok := ParseDetails(result.Details); !ok || parsed != details {
		t.Fatalf("ParseDetails() = %#v, %v", parsed, ok)
	}
	record, reader, err := store.Open(context.Background(), "session_test", details.AttachmentID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || record.Width != 3 || record.Height != 2 || !bytes.Equal(stored, data) {
		t.Fatalf("stored record = %#v, bytes equal = %v, err = %v", record, bytes.Equal(stored, data), err)
	}
}

func TestExecuteRejectsUnsupportedMalformedAndNonRegularInputs(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	validWithTrailingPayload := append(testPNG(t, 2, 2), []byte("payload\x00\x00\x00\x00IEND\xaeB\x60\x82")...)
	for name, data := range map[string][]byte{
		"plain.txt":    []byte("not an image"),
		"broken.png":   append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...),
		"trailing.png": validWithTrailingPayload,
	} {
		if err := os.WriteFile(filepath.Join(cwd, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{SessionID: "session_test", CWD: func() string { return cwd }, Store: store}
	for _, path := range []string{"plain.txt", "broken.png", "trailing.png", "."} {
		result, err := execute(context.Background(), options, arguments{Path: path})
		if err != nil {
			t.Fatalf("execute(%q): %v", path, err)
		}
		if !result.IsError || len(result.Content) != 1 {
			t.Fatalf("execute(%q) = %#v, want application error", path, result)
		}
	}
}

func TestExecutePropagatesCancellation(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "sample.png"), testPNG(t, 2, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := execute(ctx, Options{SessionID: "session_test", CWD: func() string { return cwd }, Store: store}, arguments{Path: "sample.png"})
	if !errors.Is(err, context.Canceled) || result.IsError {
		t.Fatalf("result = %#v, err = %v, want context cancellation", result, err)
	}
}

func TestExecuteRejectsCaptionOverTwoHundredCharacters(t *testing.T) {
	t.Parallel()
	result, err := execute(context.Background(), Options{SessionID: "session_test", CWD: func() string { return t.TempDir() }, Store: rejectingStore{}}, arguments{
		Path: "ignored.png", Caption: strings.Repeat("界", 201),
	})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].(droids.TextContent).Text, "200 characters") {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestParseDetailsRejectsUnmarkedOrInvalidPresentation(t *testing.T) {
	t.Parallel()
	valid := Details{
		Presentation: Presentation, AttachmentID: "attachment_0123456789abcdef0123456789abcdef",
		Filename: "image.png", MediaType: "image/png", Width: 2, Height: 3,
	}
	cases := []Details{valid}
	cases[0].Presentation = "other"
	cases = append(cases, Details{Presentation: Presentation, AttachmentID: "bad", Filename: "image.png", MediaType: "image/png", Width: 2, Height: 3})
	for _, details := range cases {
		raw, _ := json.Marshal(details)
		if _, ok := ParseDetails(raw); ok {
			t.Fatalf("accepted details %#v", details)
		}
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

type rejectingStore struct{}

func (rejectingStore) Put(context.Context, attachment.PutInput) (attachment.Record, error) {
	return attachment.Record{}, io.ErrClosedPipe
}
func (rejectingStore) Open(context.Context, string, string) (attachment.Record, io.ReadCloser, error) {
	return attachment.Record{}, nil, io.ErrClosedPipe
}
func (rejectingStore) Remove(context.Context, string, string) error { return nil }
func (rejectingStore) RemoveSession(context.Context, string) error  { return nil }
