package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/apphome"
)

func testStatus(input int, cost float64) SessionStatus {
	name := "Parent"
	return SessionStatus{
		SessionID: "session-one", Model: "anthropic/claude-sonnet-4-5", ThinkingLevel: "medium",
		Parent:  &ParentSession{ID: "session-parent", Name: &name},
		Context: &ContextStatus{Tokens: input, Window: 200000},
		Usage:   contract.SessionUsage{Input: input, TotalTokens: input, Cost: contract.SessionUsageCost{Input: cost, Total: cost}},
	}
}

func encodedStatus(t *testing.T, status SessionStatus) string {
	t.Helper()
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// receivedStatuses returns the params of every status notification the
// helper plugin in root received, in order.
func receivedStatuses(root string) []string {
	data, _ := os.ReadFile(filepath.Join(root, "received.jsonl"))
	var statuses []string
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var message struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &message) == nil && message.Method == statusChangedMethod {
			statuses = append(statuses, string(message.Params))
		}
	}
	return statuses
}

func TestStatusReachesPluginsAtStartupAndWhenItChanges(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	root := hostInstallationVersion(t, home.Plugins, "status", "status", "normal", 2)
	host := testHost(t, home, t.TempDir())
	first, second := testStatus(100, 0.01), testStatus(5000, 0.25)
	host.SetStatus(first)
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })

	raw, err := os.ReadFile(filepath.Join(root, "initial.json"))
	if err != nil {
		t.Fatal(err)
	}
	var initial struct {
		Status json.RawMessage `json:"status"`
	}
	if err := json.Unmarshal(raw, &initial); err != nil || string(initial.Status) != encodedStatus(t, first) {
		t.Fatalf("initialize status = %s, want %s", initial.Status, encodedStatus(t, first))
	}

	host.SetStatus(second)
	want := []string{encodedStatus(t, second)}
	eventuallyHost(t, func() bool { return slices.Equal(receivedStatuses(root), want) })
	// The same status again is not sent again.
	host.SetStatus(second)
	host.Rename("Renamed")
	eventuallyHost(t, func() bool {
		data, _ := os.ReadFile(filepath.Join(root, "received.jsonl"))
		return bytes.Contains(data, []byte(`"kit/events/session.changed"`))
	})
	if got := receivedStatuses(root); !slices.Equal(got, want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
}

func TestVersion1PluginsReceiveNoStatus(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	root := hostInstallation(t, home.Plugins, "status", "status", "normal")
	host := testHost(t, home, t.TempDir())
	host.SetStatus(testStatus(100, 0.01))
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	host.SetStatus(testStatus(200, 0.02))
	host.CompactionChanged(Compaction{ID: "compaction_1"})
	host.Rename("Renamed")
	eventuallyHost(t, func() bool {
		data, _ := os.ReadFile(filepath.Join(root, "received.jsonl"))
		return bytes.Contains(data, []byte(`"kit/events/session.changed"`))
	})
	initial, _ := os.ReadFile(filepath.Join(root, "initial.json"))
	want := `{"protocolVersion":1,"context":{"project":{"cwd":`
	if !bytes.HasPrefix(initial, []byte(want)) || bytes.Contains(initial, []byte(`"status"`)) {
		t.Fatalf("version 1 initialize = %s", initial)
	}
	data, _ := os.ReadFile(filepath.Join(root, "received.jsonl"))
	if bytes.Contains(data, []byte(`kit/events/session.status.changed`)) || bytes.Contains(data, []byte(`kit/events/compaction.`)) {
		t.Fatalf("version 1 plugin received status events:\n%s", data)
	}
}

func TestCompactionEventsReachVersion2Plugins(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	root := hostInstallationVersion(t, home.Plugins, "status", "status", "normal", 2)
	host := testHost(t, home, t.TempDir())
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	host.CompactionChanged(Compaction{ID: "compaction_1", TurnID: "turn_1"})
	host.CompactionChanged(Compaction{ID: "compaction_1", TurnID: "turn_1", Completed: true})
	host.CompactionChanged(Compaction{ID: "compaction_2", Completed: true, FailureReason: contract.CompactionFailureCanceled})
	want := []string{
		`{"jsonrpc":"2.0","method":"kit/events/compaction.started","params":{"sessionId":"session-one","compactionId":"compaction_1","turnId":"turn_1"}}`,
		`{"jsonrpc":"2.0","method":"kit/events/compaction.completed","params":{"sessionId":"session-one","compactionId":"compaction_1","turnId":"turn_1","failureReason":null}}`,
		`{"jsonrpc":"2.0","method":"kit/events/compaction.completed","params":{"sessionId":"session-one","compactionId":"compaction_2","turnId":null,"failureReason":"canceled"}}`,
	}
	eventuallyHost(t, func() bool {
		data, _ := os.ReadFile(filepath.Join(root, "received.jsonl"))
		var got []string
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			if bytes.Contains(line, []byte(`"kit/events/compaction.`)) {
				got = append(got, string(line))
			}
		}
		return slices.Equal(got, want)
	})
}

func TestTooManyPendingCompactionsAreReported(t *testing.T) {
	host := testHost(t, apphome.FromHome(t.TempDir()), t.TempDir())
	for range maxPendingCompactions + 2 {
		host.CompactionChanged(Compaction{ID: "compaction_1"})
	}
	host.deliverStatus()
	if warnings := host.Warnings(); len(warnings) != 1 || warnings[0] != "Plugins: 2 compaction events were not delivered to plugins" {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestStatusRequestReturnsTheLatestStatus(t *testing.T) {
	host := testHost(t, apphome.FromHome(t.TempDir()), t.TempDir())
	version2 := withProtocolVersion(t.Context(), 2)
	var failure *RPCError
	if _, err := host.handleStatusRequest(version2, nil); !errors.As(err, &failure) || failure.Code != -32603 {
		t.Fatalf("status before the session gave one = %v", err)
	}
	host.SetStatus(testStatus(100, 0.01))
	status := testStatus(300, 0.03)
	host.SetStatus(status)
	result, err := host.handleStatusRequest(version2, nil)
	if err != nil || string(result) != encodedStatus(t, status) {
		t.Fatalf("status = %s, %v", result, err)
	}
	if _, err := host.handleStatusRequest(version2, json.RawMessage(`{}`)); !errors.As(err, &failure) || failure.Code != -32602 {
		t.Fatalf("status with params = %v", err)
	}
	if _, err := host.handleStatusRequest(withProtocolVersion(t.Context(), 1), nil); !errors.As(err, &failure) || failure.Code != -32601 {
		t.Fatalf("version 1 status request = %v", err)
	}
	// The host keeps its own copy.
	*status.Parent.Name = "Changed"
	result, _ = host.handleStatusRequest(version2, nil)
	if bytes.Contains(result, []byte("Changed")) {
		t.Fatalf("status shares memory with the caller: %s", result)
	}
}

// TestQueuedLatestNotificationIsReplaced holds the first notification in the
// pipe, queues a second, a different notification, and a third that replaces
// the second and follows the other notification.
func TestQueuedLatestNotificationIsReplaced(t *testing.T) {
	endpoint, _, reader := rpcTestPeer(t, RPCHandlers{}, defaultRPCLimits)
	notify := func(value string) {
		t.Helper()
		if err := endpoint.tryNotifyLatest("status", "status", json.RawMessage(`{"value":"`+value+`"}`)); err != nil {
			t.Fatal(err)
		}
	}
	notify("first")
	eventuallyHost(t, func() bool {
		endpoint.mu.Lock()
		defer endpoint.mu.Unlock()
		return len(endpoint.queue) == 0
	})
	notify("second")
	if err := endpoint.tryNotify("other", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	notify("third")
	var got []string
	for range 3 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, line)
	}
	want := []string{
		`{"jsonrpc":"2.0","method":"status","params":{"value":"first"}}` + "\n",
		`{"jsonrpc":"2.0","method":"other","params":{}}` + "\n",
		`{"jsonrpc":"2.0","method":"status","params":{"value":"third"}}` + "\n",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("frames = %q, want %q", got, want)
	}
	eventuallyHost(t, func() bool {
		endpoint.mu.Lock()
		defer endpoint.mu.Unlock()
		return endpoint.queuedBytes == 0
	})
}
