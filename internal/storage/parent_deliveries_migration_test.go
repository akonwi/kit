package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/subagent"
)

func TestParentDeliveriesMigrationPreservesBoundariesAndAcknowledgements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kit.db")
	legacyDB, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	prefix := fstest.MapFS{}
	for i := 1; i <= 14; i++ {
		name := fmt.Sprintf("%04d_", i)
		matches, err := fs.Glob(migrationFiles, "migrations/"+name+"*.sql")
		if err != nil || len(matches) != 1 {
			t.Fatalf("migration %s = %#v, %v", name, matches, err)
		}
		body, err := fs.ReadFile(migrationFiles, matches[0])
		if err != nil {
			t.Fatal(err)
		}
		prefix[matches[0]] = &fstest.MapFile{Data: body}
	}
	if err := migrateFS(t.Context(), legacyDB, prefix); err != nil {
		t.Fatal(err)
	}
	legacy := &Store{db: legacyDB}
	owner := "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := legacy.CreateSession(t.Context(), session.NewSession{
		ID: owner, ScratchpadOwnerID: owner, CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model", ThinkingLevel: "medium",
	}); err != nil {
		t.Fatal(err)
	}
	conversation, pendingTask, err := legacy.Admit(t.Context(), testAdmission(owner, "pending task"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	_, deliveredTask, err := legacy.Admit(t.Context(), testAdmission(owner, "delivered task"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	definition := testAdmission(owner, "request recipient").Definition
	definition.Name = "reviewer"
	pendingRequest, err := legacy.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_pending",
		DeliveryMode: "send", Message: "pending reply",
	})
	if err != nil {
		t.Fatal(err)
	}
	deliveredRequest, err := legacy.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name,
		CallIdentity: "turn_parent/call_delivered", DeliveryMode: "send", Message: "delivered reply",
	})
	if err != nil {
		t.Fatal(err)
	}
	ask, err := legacy.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name,
		CallIdentity: "turn_parent/call_ask", DeliveryMode: "ask", Message: "ask reply",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-time.Minute)
	for _, task := range []subagent.Task{pendingTask, deliveredTask} {
		if _, err := legacyDB.ExecContext(t.Context(), `UPDATE subagent_tasks SET state = 'completed', finished_at = ? WHERE id = ?`, formatTimestamp(base), task.ID); err != nil {
			t.Fatal(err)
		}
	}
	for i, task := range []subagent.Task{pendingTask, deliveredTask} {
		id := fmt.Sprintf("mail_%032x", i+1)
		var delivered any
		if i == 1 {
			delivered = formatTimestamp(base.Add(5 * time.Second))
		}
		generation := 1
		if i == 1 {
			generation = 4
		}
		if _, err := legacyDB.ExecContext(t.Context(), `INSERT INTO parent_mailbox
			(id, owner_session_id, conversation_id, task_id, agent_name, task_state, summary,
			 generation, created_at, delivered_at) VALUES (?, ?, ?, ?, 'scout', 'completed', ?, ?, ?, ?)`,
			id, owner, conversation.ID, task.ID, task.Message, generation, formatTimestamp(base.Add(time.Duration(i)*time.Second)), delivered); err != nil {
			t.Fatal(err)
		}
	}
	for i, request := range []subagent.Request{pendingRequest, deliveredRequest, ask} {
		if _, err := legacyDB.ExecContext(t.Context(), `UPDATE subagent_requests
			SET state = 'replied', reply = ?, resolved_at = ? WHERE id = ?`,
			request.Message+" answer", formatTimestamp(base.Add(time.Duration(i)*time.Second)), request.ID); err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			continue // ask has no parent-directed result
		}
		var delivered any
		if i == 1 {
			delivered = formatTimestamp(base.Add(6 * time.Second))
		}
		if _, err := legacyDB.ExecContext(t.Context(), `INSERT INTO subagent_parent_reply_mailbox(request_id, created_at, delivered_at)
			VALUES (?, ?, ?)`, request.ID, formatTimestamp(base.Add(time.Duration(i+2)*time.Second)), delivered); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if version, err := store.CurrentMigration(t.Context()); err != nil || version != 15 {
		t.Fatalf("migration version = %d, %v", version, err)
	}
	pending, err := store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(pending) != 2 || pending[0].ID != fmt.Sprintf("mail_%032x", 1) || pending[0].TaskID != pendingTask.ID || pending[0].Kind != subagent.ParentDeliveryTask || pending[0].Generation != 1 || !pending[0].CreatedAt.Equal(base) ||
		pending[1].ID != parentRequestDeliveryID(pendingRequest.ID) || pending[1].RequestID != pendingRequest.ID || pending[1].Summary != pendingRequest.Message+" answer" || pending[1].Kind != subagent.ParentDeliveryRequest || pending[1].Generation != 1 || !pending[1].CreatedAt.Equal(base.Add(2*time.Second)) {
		t.Fatalf("migrated pending deliveries = %#v, %v", pending, err)
	}
	owners, err := store.PendingMailboxOwners(t.Context(), "", 8)
	if err != nil || len(owners) != 1 || owners[0] != owner {
		t.Fatalf("pending delivery owners = %#v, %v", owners, err)
	}
	for _, item := range []struct {
		id         string
		kind       subagent.ParentDeliveryKind
		summary    string
		generation int
		created    time.Time
		delivered  time.Time
	}{
		{fmt.Sprintf("mail_%032x", 2), subagent.ParentDeliveryTask, deliveredTask.Message, 4, base.Add(time.Second), base.Add(5 * time.Second)},
		{parentRequestDeliveryID(deliveredRequest.ID), subagent.ParentDeliveryRequest, deliveredRequest.Message + " answer", 1, base.Add(3 * time.Second), base.Add(6 * time.Second)},
	} {
		var kind, summary, created string
		var delivered sql.NullString
		var generation int
		if err := store.db.QueryRowContext(t.Context(), `SELECT kind, summary, generation, created_at, delivered_at FROM subagent_parent_deliveries WHERE id = ?`, item.id).Scan(&kind, &summary, &generation, &created, &delivered); err != nil || kind != string(item.kind) || summary != item.summary || generation != item.generation || created != formatTimestamp(item.created) || !delivered.Valid || delivered.String != formatTimestamp(item.delivered) {
			t.Fatalf("migrated acknowledged delivery %s = %q, %q, %d, %q, %#v, %v", item.id, kind, summary, generation, created, delivered, err)
		}
	}
	before, err := store.InspectSubagentRequest(t.Context(), owner, "", pendingRequest.ID)
	if err != nil || !before.DeliveryPending || before.Reply != "" {
		t.Fatalf("pending request inspection = %#v, %v", before, err)
	}
	ids := []string{pending[0].ID, pending[1].ID}
	if err := store.MarkMailboxDelivered(t.Context(), ids, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkMailboxDelivered(t.Context(), ids, 1, time.Now()); err != nil {
		t.Fatalf("retry migrated acknowledgement: %v", err)
	}
	if err := store.MarkMailboxDelivered(t.Context(), ids, 2, time.Now()); !errors.Is(err, subagent.ErrConflict) {
		t.Fatalf("stale generation acknowledgement = %v", err)
	}
	if remaining, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(remaining) != 0 {
		t.Fatalf("acknowledged migrated deliveries = %#v, %v", remaining, err)
	}
	after, err := store.InspectSubagentRequest(t.Context(), owner, "", pendingRequest.ID)
	if err != nil || after.Reply != pendingRequest.Message+" answer" || after.DeliveryPending {
		t.Fatalf("acknowledged request inspection = %#v, %v", after, err)
	}
	var phantom int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM subagent_parent_deliveries WHERE request_id = ?`, ask.ID).Scan(&phantom); err != nil || phantom != 0 {
		t.Fatalf("ask gained parent delivery = %d, %v", phantom, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if remaining, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(remaining) != 0 {
		t.Fatalf("reopened acknowledged deliveries = %#v, %v", remaining, err)
	}
	var total int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM subagent_parent_deliveries WHERE owner_session_id = ?`, owner).Scan(&total); err != nil || total != 4 {
		t.Fatalf("reopened delivery history = %d, %v", total, err)
	}
}
