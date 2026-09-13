package attachment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFilesystemPutAndOpen(t *testing.T) {
	t.Parallel()

	store, err := NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return createdAt }

	record, err := store.Put(context.Background(), PutInput{
		SessionID: "session_one", Filename: "example.png", MediaType: "image/png",
		Content: strings.NewReader("image bytes"), MaxBytes: 32, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.SessionID != "session_one" || record.Filename != "example.png" || record.Size != 11 || record.CreatedAt != createdAt {
		t.Fatalf("record = %+v", record)
	}
	if record.SHA256 != "de7030234493a8bea844dbe1d8676e68a2c1a4b014c721f0425a22b6df66faec" {
		t.Fatalf("sha256 = %q", record.SHA256)
	}

	opened, content, err := store.Open(context.Background(), "session_one", record.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	got, err := io.ReadAll(content)
	if err != nil {
		t.Fatal(err)
	}
	if opened != record || string(got) != "image bytes" {
		t.Fatalf("opened = %+v, content = %q", opened, got)
	}

	for _, path := range []string{
		filepath.Join(store.root, record.ID),
		filepath.Join(store.root, record.ID, manifestFilename),
		filepath.Join(store.root, record.ID, contentFilename),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s permissions = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}

func TestFilesystemEnforcesSizeAndInputBounds(t *testing.T) {
	t.Parallel()

	store, err := NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Put(context.Background(), PutInput{
		SessionID: "session_one", Filename: "notes.txt", MediaType: "text/plain",
		Content: strings.NewReader("too large"), MaxBytes: 3,
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put() error = %v, want ErrTooLarge", err)
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed put left %d entries", len(entries))
	}

	for _, input := range []PutInput{
		{SessionID: "session_one", Filename: "notes.txt", MediaType: "text/plain", Content: strings.NewReader("x"), MaxBytes: math.MaxInt64},
		{SessionID: "", Filename: "notes.txt", MediaType: "text/plain", Content: strings.NewReader("x"), MaxBytes: 1},
		{SessionID: "session_one", Filename: "../notes.txt", MediaType: "text/plain", Content: strings.NewReader("x"), MaxBytes: 1},
		{SessionID: "session_one", Filename: "notes.txt", MediaType: "text/plain; charset=utf-8", Content: strings.NewReader("x"), MaxBytes: 1},
		{SessionID: "session_one", Filename: "notes.bin", MediaType: "application/octet-stream", Content: strings.NewReader("x"), MaxBytes: 1},
	} {
		if _, err := store.Put(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Put(%+v) error = %v, want ErrInvalidInput", input, err)
		}
	}
}

func TestFilesystemValidatesBeforePublication(t *testing.T) {
	t.Parallel()

	store, err := NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Put(context.Background(), PutInput{
		SessionID: "session_one", Filename: "notes.txt", MediaType: "text/plain",
		Content: strings.NewReader("invalid"), MaxBytes: 16,
		Validate: func(io.ReadSeeker) error { return errors.New("rejected") },
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Put() error = %v, want ErrInvalidInput", err)
	}
	entries, err := os.ReadDir(store.root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed validation left entries: %v, %v", entries, err)
	}
}

func TestFilesystemHidesCrossSessionAttachments(t *testing.T) {
	t.Parallel()

	store, err := NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Put(context.Background(), PutInput{
		SessionID: "session_one", Filename: "notes.txt", MediaType: "text/plain",
		Content: strings.NewReader("private"), MaxBytes: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, content, err := store.Open(context.Background(), "session_two", record.ID); !errors.Is(err, ErrNotFound) || content != nil {
		t.Fatalf("cross-session Open() = %v, %v, want ErrNotFound", content, err)
	}
	if _, content, err := store.Open(context.Background(), "session_one", "../"+record.ID); !errors.Is(err, ErrNotFound) || content != nil {
		t.Fatalf("invalid-id Open() = %v, %v, want ErrNotFound", content, err)
	}
}

func TestFilesystemRejectsMalformedManifestAndSymlinkContent(t *testing.T) {
	t.Parallel()

	store, err := NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Put(context.Background(), PutInput{
		SessionID: "session_one", Filename: "notes.txt", MediaType: "text/plain",
		Content: strings.NewReader("private"), MaxBytes: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(store.root, record.ID, manifestFilename)
	record.Filename = "../unsafe.txt"
	manifest, _ := json.Marshal(record)
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, content, err := store.Open(context.Background(), "session_one", record.ID); err == nil || content != nil {
		t.Fatalf("malformed manifest Open() = %v, %v", content, err)
	}

	record.Filename = "notes.txt"
	manifest, _ = json.Marshal(record)
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	contentPath := filepath.Join(store.root, record.ID, contentFilename)
	if err := os.Remove(contentPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(manifestPath, contentPath); err != nil {
		t.Fatal(err)
	}
	if _, content, err := store.Open(context.Background(), "session_one", record.ID); err == nil || content != nil {
		t.Fatalf("symlink content Open() = %v, %v", content, err)
	}
}

func TestFilesystemRemovesSessionAttachmentsOnly(t *testing.T) {
	t.Parallel()

	store, err := NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	var records []Record
	for _, sessionID := range []string{"session_one", "session_two"} {
		record, err := store.Put(context.Background(), PutInput{
			SessionID: sessionID, Filename: "notes.txt", MediaType: "text/plain",
			Content: strings.NewReader("private"), MaxBytes: 16,
		})
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := store.RemoveSession(context.Background(), "session_one"); err != nil {
		t.Fatal(err)
	}
	if _, content, err := store.Open(context.Background(), "session_one", records[0].ID); !errors.Is(err, ErrNotFound) || content != nil {
		t.Fatalf("removed attachment remains: %v, %v", content, err)
	}
	if _, content, err := store.Open(context.Background(), "session_two", records[1].ID); err != nil {
		t.Fatalf("other session attachment removed: %v", err)
	} else {
		_ = content.Close()
	}
}

func TestFilesystemDetectsContentCorruption(t *testing.T) {
	t.Parallel()

	store, err := NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Put(context.Background(), PutInput{
		SessionID: "session_one", Filename: "notes.txt", MediaType: "text/plain",
		Content: strings.NewReader("original"), MaxBytes: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.root, record.ID, contentFilename), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, content, err := store.Open(context.Background(), "session_one", record.ID); err == nil || content != nil {
		t.Fatalf("corrupt Open() = %v, %v, want validation error", content, err)
	}
}
