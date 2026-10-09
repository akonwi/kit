package sqlitestore

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestShowImageBlobMigrationRemovesOnlyPresentedImageContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "droid.sqlite")
	store, err := Open(t.Context(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	showImage := `{"id":"message_show","conversation_id":"conversation_test","turn_id":"turn_test","created_at":"2025-01-01T00:00:00Z","message":{"role":"toolResult","content":[{"type":"text","text":"Image displayed successfully."},{"type":"file","filename":"sample.png","media_type":"image/png","url":"data:image/png;base64,expanded","attachment_id":"attachment_0123456789abcdef0123456789abcdef"}],"tool_call_id":"call_show","tool_name":"show_image","details":{"presentation":"transcript-image","attachmentId":"attachment_0123456789abcdef0123456789abcdef","filename":"sample.png","mediaType":"image/png","width":3,"height":2}}}`
	otherTool := strings.ReplaceAll(showImage, `"id":"message_show"`, `"id":"message_other"`)
	otherTool = strings.ReplaceAll(otherTool, `"tool_name":"show_image"`, `"tool_name":"inspect_image"`)
	var envelope map[string]any
	if err := json.Unmarshal([]byte(showImage), &envelope); err != nil {
		t.Fatal(err)
	}
	message, err := json.Marshal(envelope["message"])
	if err != nil {
		t.Fatal(err)
	}
	runtime := fmt.Sprintf(`{"model_cycles":9007199254740993,"context":[%s,{"message":{"role":"context","details":{"nested":%s}}}],"tools":{"call_show":{"raw_result":%s,"final_result":%s}}}`, showImage, message, message, message)
	tool := fmt.Sprintf(`{"raw_result":%s,"final_result":%s}`, message, message)
	checkpoint := fmt.Sprintf(`{"messages":[%s]}`, showImage)
	invalidDetails := strings.Replace(showImage, `"height":2`, `"height":2,"caption":"`+strings.Repeat("a", 201)+`"`, 1)
	if _, err := store.db.ExecContext(t.Context(), `
		INSERT INTO records(record_kind, record_id, scope, sequence, version, payload, created_revision, updated_revision)
		VALUES ('message', 'message_show', 'history', 1, 1, ?, 1, 1),
		       ('message', 'message_other', 'history', 2, 1, ?, 1, 1),
		       ('runtime', 'current', 'runtime', NULL, 1, ?, 1, 1),
		       ('tool', 'call_show', 'history', 3, 1, ?, 1, 1),
		       ('checkpoint', 'checkpoint_show', 'history', 4, 1, ?, 1, 1),
		       ('message', 'message_invalid', 'history', 5, 1, ?, 1, 1);
		DELETE FROM schema_migrations WHERE version = 2;
	`, []byte(showImage), []byte(otherTool), []byte(runtime), []byte(tool), []byte(checkpoint), []byte(invalidDetails)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(t.Context(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var migrated, untouched []byte
	if err := store.db.QueryRowContext(t.Context(), `SELECT payload FROM records WHERE record_id = 'message_show'`).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT payload FROM records WHERE record_id = 'message_other'`).Scan(&untouched); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(migrated), "base64,expanded") || !json.Valid(migrated) {
		t.Fatalf("migrated payload = %s", migrated)
	}
	var payload struct {
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Details struct {
				AttachmentID string `json:"attachmentId"`
			} `json:"details"`
		} `json:"message"`
	}
	if err := json.Unmarshal(migrated, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Message.Content) != 1 || payload.Message.Content[0].Type != "text" || payload.Message.Content[0].Text != "Image displayed successfully." || payload.Message.Details.AttachmentID == "" {
		t.Fatalf("migrated payload shape = %#v", payload)
	}
	if !strings.Contains(string(untouched), "base64,expanded") {
		t.Fatalf("unrelated tool payload changed = %s", untouched)
	}
	var remaining int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM records WHERE CAST(payload AS TEXT) LIKE '%base64,expanded%'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 3 {
		t.Fatalf("records retaining expanded image = %d, want unrelated tool, arbitrary details, and invalid presentation", remaining)
	}
	var migratedRuntime []byte
	if err := store.db.QueryRowContext(t.Context(), `SELECT payload FROM records WHERE record_kind = 'runtime' AND record_id = 'current'`).Scan(&migratedRuntime); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migratedRuntime), `"model_cycles":9007199254740993`) || strings.Count(string(migratedRuntime), "base64,expanded") != 1 {
		t.Fatalf("migrated runtime lost precise or arbitrary data = %s", migratedRuntime)
	}
	if _, err := store.db.ExecContext(t.Context(), `UPDATE records SET payload = payload WHERE record_id = 'message_show'`); err == nil || !strings.Contains(err.Error(), "historical record is immutable") {
		t.Fatalf("history update error = %v", err)
	}
}
