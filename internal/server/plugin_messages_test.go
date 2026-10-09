package server

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/plugin"
	"github.com/akonwi/kit/internal/session"
)

const sessionMessageFixture = "session-message-demo"

type pluginMessageRow struct {
	ID, TurnID, Source, Text string
	Details                  string
}

// pluginMessageRows returns the session's plugin message transcript rows.
func pluginMessageRowsFromSnapshot(snapshot protocol.SessionSnapshot) []pluginMessageRow {
	var rows []pluginMessageRow
	for _, message := range snapshot.Messages {
		if message.Role == "context" && message.BoundaryKind == protocol.PluginMessageBoundaryKind {
			rows = append(rows, pluginMessageRow{
				ID: message.BoundaryID, TurnID: message.TurnID, Source: message.BoundarySource,
				Text: message.TextContent(), Details: string(message.Details),
			})
		}
	}
	return rows
}

func TestPluginMessageBoundaryKindMatchesContract(t *testing.T) {
	if session.PluginMessageBoundaryKind != protocol.PluginMessageBoundaryKind {
		t.Fatalf("session kind %q != contract kind %q", session.PluginMessageBoundaryKind, protocol.PluginMessageBoundaryKind)
	}
}

func TestPluginSessionMessageFixtureThroughDaemon(t *testing.T) {
	providers := &daemonEchoProviders{}
	client, sessionID, _ := pluginFixtureClientWithProviders(t, sessionMessageFixture, 3, providers)
	snapshot, err := client.transport.GetSessionSnapshot(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	commands := map[string]protocol.PluginCommand{}
	for _, command := range snapshot.PluginCommands {
		commands[command.LocalID] = command
	}

	toastContext, cancelToasts := context.WithCancel(t.Context())
	defer cancelToasts()
	toastBody, err := client.transport.StreamPluginToasts(toastContext, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer toastBody.Close()
	toasts := make(chan protocol.PluginToast, 32)
	go func() {
		_ = ReadPluginToasts(toastBody, func(toast protocol.PluginToast) error { toasts <- toast; return nil })
	}()
	execute := func(local, args string) {
		t.Helper()
		command := commands[local]
		if err := client.transport.ExecutePluginCommand(t.Context(), sessionID, protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance, Args: args}); err != nil {
			t.Fatalf("execute %s: %v", local, err)
		}
	}
	nextToast := func() (string, string) {
		t.Helper()
		select {
		case toast := <-toasts:
			return toast.Title, toast.Subtitle
		case <-time.After(5 * time.Second):
			t.Fatal("missing plugin submission toast")
			return "", ""
		}
	}
	settledRows := func(id string, count int) []pluginMessageRow {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			snapshot, err := client.transport.GetSessionSnapshot(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			rows := pluginMessageRowsFromSnapshot(snapshot)
			if snapshot.ActiveTurnID == "" && len(rows) == count {
				return rows
			}
			if time.Now().After(deadline) {
				t.Fatalf("session %s did not settle with %d plugin rows: active %q, rows %+v", id, count, snapshot.ActiveTurnID, rows)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	details := `{"version":1,"pluginId":"` + sessionMessageFixture + `"}`

	// Kickoff from a user-invoked command.
	execute("send", "Kickoff from a command.")
	rows := settledRows(sessionID, 1)
	if !strings.HasPrefix(rows[0].ID, "pluginmsg_") || rows[0].TurnID == "" {
		t.Fatalf("kickoff row identity = %+v", rows[0])
	}
	if want := []pluginMessageRow{{ID: rows[0].ID, TurnID: rows[0].TurnID, Source: sessionMessageFixture, Text: "Kickoff from a command.", Details: details}}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("kickoff rows = %+v, want %+v", rows, want)
	}

	// Continuation after each completed turn the plugin started.
	execute("loop", "3")
	rows = settledRows(sessionID, 4)
	var texts []string
	for _, row := range rows {
		texts = append(texts, row.Text)
	}
	if want := []string{"Kickoff from a command.", "Loop turn 1 of 3.", "Loop turn 2 of 3.", "Loop turn 3 of 3."}; !reflect.DeepEqual(texts, want) {
		t.Fatalf("continuation texts = %q, want %q", texts, want)
	}

	// A keyed retry replays the original admission without a rejection or a
	// second row; reuse with other text conflicts. The plugin submits in order,
	// so a rejected retry would surface as a toast before the conflict.
	execute("send-keyed", "Keyed message.")
	settledRows(sessionID, 5)
	execute("send-keyed", "Keyed message.")
	execute("send-keyed", "Different text.")
	if title, subtitle := nextToast(); title != "Message rejected" || subtitle != "-32003 Idempotency key was used with different text" {
		t.Fatalf("conflict toast = %q %q", title, subtitle)
	}

	// A running turn rejects plugin messages without recording them.
	block, started := make(chan struct{}), make(chan struct{})
	providers.mu.Lock()
	providers.block, providers.requestStarted = block, started
	providers.mu.Unlock()
	if _, err := client.transport.StartPrompt(t.Context(), sessionID, "user turn"); err != nil {
		t.Fatal(err)
	}
	<-started
	execute("send", "While busy.")
	if title, subtitle := nextToast(); title != "Message rejected" || subtitle != "-32006 Session is busy" {
		t.Fatalf("busy toast = %q %q", title, subtitle)
	}
	providers.mu.Lock()
	providers.block, providers.requestStarted = nil, nil
	providers.mu.Unlock()
	close(block)
	rows = settledRows(sessionID, 5)
	texts = texts[:0]
	for _, row := range rows {
		texts = append(texts, row.Text)
	}
	if want := []string{"Kickoff from a command.", "Loop turn 1 of 3.", "Loop turn 2 of 3.", "Loop turn 3 of 3.", "Keyed message."}; !reflect.DeepEqual(texts, want) {
		t.Fatalf("final texts = %q, want %q", texts, want)
	}

	// Another session's plugin instance and transcript are unaffected.
	other, err := client.transport.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if rows := settledRows(other.ID, 0); len(rows) != 0 {
		t.Fatalf("other session plugin rows = %+v", rows)
	}
}

func TestPluginMessageSessionErrorsMapToStableRPCCodes(t *testing.T) {
	owner := plugin.InstanceID{SessionID: "session_one", PluginID: "demo", HostID: "host", Generation: 1}
	internal := errors.New("store detail: /private/path")
	for _, test := range []struct {
		err     error
		code    int64
		message string
	}{
		{session.ErrBusy, -32006, "Session is busy"},
		{fmt.Errorf("wrapped: %w", session.ErrPluginMessageConflict), -32003, "Idempotency key was used with different text"},
		{session.ErrClosed, -32002, "Plugin generation is unavailable"},
		{fmt.Errorf("%w: bad text", session.ErrInvalidInput), -32602, "Invalid session message"},
		{internal, -32603, "Session message submission failed"},
	} {
		host := &sessionPluginHost{}
		host.SetMessageObserver(func(context.Context, session.PluginMessageInput, func() bool) (session.PluginMessageResult, error) {
			return session.PluginMessageResult{}, test.err
		})
		_, err := host.submitMessage(t.Context(), plugin.MessageRequest{Owner: owner, Text: "Continue."})
		var failure *plugin.RPCError
		if !errors.As(err, &failure) || failure.Code != test.code || failure.Message != test.message {
			t.Errorf("%v mapped to %v; want %d %q", test.err, err, test.code, test.message)
		}
	}
	host := &sessionPluginHost{}
	host.SetMessageObserver(func(context.Context, session.PluginMessageInput, func() bool) (session.PluginMessageResult, error) {
		return session.PluginMessageResult{}, context.Canceled
	})
	if _, err := host.submitMessage(t.Context(), plugin.MessageRequest{Owner: owner, Text: "Continue."}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation mapped to %v", err)
	}
	var observed session.PluginMessageInput
	host.SetMessageObserver(func(_ context.Context, input session.PluginMessageInput, _ func() bool) (session.PluginMessageResult, error) {
		observed = input
		return session.PluginMessageResult{MessageID: "pluginmsg_1", TurnID: "turn_1"}, nil
	})
	result, err := host.submitMessage(t.Context(), plugin.MessageRequest{Owner: owner, Text: "Continue.", IdempotencyKey: "k"})
	if err != nil || result != (plugin.MessageResult{MessageID: "pluginmsg_1", TurnID: "turn_1"}) ||
		observed != (session.PluginMessageInput{PluginID: "demo", Text: "Continue.", IdempotencyKey: "k"}) {
		t.Fatalf("submission = %+v, %v; observed %+v", result, err, observed)
	}
}
