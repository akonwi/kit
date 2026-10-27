package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

const (
	testClientA = "clientplugin_0123456789abcdef0123456789abcdef"
	testClientB = "clientplugin_fedcba9876543210fedcba9876543210"
)

var testClientTools = []json.RawMessage{
	json.RawMessage(`{"id":"snapshot","description":"Outline one tab","inputSchema":{"type":"object","properties":{"tab":{"type":"string"}},"required":["tab"],"additionalProperties":false}}`),
	json.RawMessage(`{"id":"tabs","description":"List tabs","inputSchema":{"type":"object","additionalProperties":false},"executionMode":"parallel"}`),
}

// clientCalls records the calls and resolutions a host publishes.
type clientCalls struct {
	mu          sync.Mutex
	requests    []ClientCallRequest
	resolutions []ClientCallResolution
	published   chan ClientCallRequest
}

func newClientCalls() *clientCalls {
	return &clientCalls{published: make(chan ClientCallRequest, 16)}
}

func (c *clientCalls) requested(_ context.Context, request ClientCallRequest) error {
	c.mu.Lock()
	c.requests = append(c.requests, request)
	c.mu.Unlock()
	c.published <- request
	return nil
}

func (c *clientCalls) resolved(resolution ClientCallResolution) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resolutions = append(c.resolutions, resolution)
}

func (c *clientCalls) outcomes() []ClientCallOutcome {
	c.mu.Lock()
	defer c.mu.Unlock()
	var result []ClientCallOutcome
	for _, resolution := range c.resolutions {
		result = append(result, resolution.Outcome)
	}
	return result
}

func clientHost(t *testing.T, calls *clientCalls, configure ...func(*HostConfig)) *Host {
	t.Helper()
	config := HostConfig{Session: SessionContext{ID: "session-one"}, ReservedIDs: []string{"model"}, ClientCallRequested: calls.requested, ClientCallResolved: calls.resolved}
	for _, apply := range configure {
		apply(&config)
	}
	host := NewHost(context.Background(), config)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Close(ctx); err != nil {
			t.Errorf("close host: %v", err)
		}
	})
	return host
}

func attachBound(t *testing.T, host *Host, instanceID string) ClientStreamBinding {
	t.Helper()
	if err := host.AttachClient(ClientAttachment{InstanceID: instanceID, PluginID: "browser", Tools: testClientTools}); err != nil {
		t.Fatal(err)
	}
	binding, err := host.BindClientStream(instanceID)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func modelNames(tools []Tool) []string {
	var names []string
	for _, tool := range tools {
		names = append(names, tool.ModelName+"@"+tool.ClientInstance)
	}
	return names
}

func toolNamed(t *testing.T, host *Host, modelName string) Tool {
	t.Helper()
	for _, tool := range host.Tools() {
		if tool.ModelName == modelName {
			return tool
		}
	}
	t.Fatalf("tool %s missing from %v", modelName, modelNames(host.Tools()))
	return Tool{}
}

func TestClientAttachmentRejectsInvalidDeclarations(t *testing.T) {
	host := clientHost(t, newClientCalls())
	tooMany := make([]json.RawMessage, MaxClientTools+1)
	for index := range tooMany {
		tooMany[index] = json.RawMessage(`{"id":"t` + strings.Repeat("a", index+1) + `","description":"d","inputSchema":{"type":"object"}}`)
	}
	tests := []struct {
		name       string
		attachment ClientAttachment
		message    string
	}{
		{"instance id", ClientAttachment{InstanceID: "clientplugin_bad", PluginID: "browser"}, "invalid client plugin instance id"},
		{"plugin id", ClientAttachment{InstanceID: testClientA, PluginID: "Browser"}, "invalid plugin id"},
		{"reserved", ClientAttachment{InstanceID: testClientA, PluginID: "model"}, `plugin id "model" is reserved`},
		{"too many", ClientAttachment{InstanceID: testClientA, PluginID: "browser", Tools: tooMany}, "a client plugin declares at most 32 tools"},
		{"duplicate", ClientAttachment{InstanceID: testClientA, PluginID: "browser", Tools: []json.RawMessage{testClientTools[0], testClientTools[0]}}, `tool "snapshot" is declared twice`},
		{"schema", ClientAttachment{InstanceID: testClientA, PluginID: "browser", Tools: []json.RawMessage{json.RawMessage(`{"id":"x","description":"d","inputSchema":{"type":"string"}}`)}}, "tool 0: Invalid or unsupported tool schema: tool schema root must have type object"},
		{"unknown field", ClientAttachment{InstanceID: testClientA, PluginID: "browser", Tools: []json.RawMessage{json.RawMessage(`{"id":"x","description":"d","inputSchema":{"type":"object"},"extra":1}`)}}, "tool 0: "},
		{"name length", ClientAttachment{InstanceID: testClientA, PluginID: "browser", Tools: []json.RawMessage{json.RawMessage(`{"id":"` + strings.Repeat("a", 60) + `","description":"d","inputSchema":{"type":"object"}}`)}}, "exceeds 64 characters"},
	}
	for _, test := range tests {
		err := host.AttachClient(test.attachment)
		var invalid *InvalidClientError
		if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, test.message) {
			t.Errorf("%s: error = %v, want message containing %q", test.name, err, test.message)
		}
	}
	if tools := host.Tools(); len(tools) != 0 {
		t.Fatalf("tools after rejected attachments = %v", modelNames(tools))
	}
}

func TestClientToolsJoinTheCatalogOnlyWhileTheirInstanceIsCurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		host := clientHost(t, newClientCalls())
		if err := host.AttachClient(ClientAttachment{InstanceID: testClientA, PluginID: "browser", Tools: testClientTools}); err != nil {
			t.Fatal(err)
		}
		if got := modelNames(host.Tools()); len(got) != 0 {
			t.Fatalf("unbound instance offers %v", got)
		}
		first, err := host.BindClientStream(testClientA)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"browser__snapshot@" + testClientA, "browser__tabs@" + testClientA}
		if got := modelNames(host.Tools()); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("tools = %v, want %v", got, want)
		}
		tabs := toolNamed(t, host, "browser__tabs")
		if tabs.ExecutionMode != "parallel" || tabs.Description != "List tabs" || tabs.Owner.PluginID != "browser" {
			t.Fatalf("tabs tool = %#v", tabs)
		}

		attachBound(t, host, testClientB)
		want = []string{"browser__snapshot@" + testClientB, "browser__tabs@" + testClientB}
		if got := modelNames(host.Tools()); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("after newer attach tools = %v, want %v", got, want)
		}

		if err := host.DetachClient(testClientB); err != nil {
			t.Fatal(err)
		}
		want = []string{"browser__snapshot@" + testClientA, "browser__tabs@" + testClientA}
		if got := modelNames(host.Tools()); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("after detach tools = %v, want %v", got, want)
		}

		host.ReleaseClientStream(first)
		time.Sleep(DefaultClientGrace - time.Second)
		if got := modelNames(host.Tools()); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("within grace tools = %v, want %v", got, want)
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if got := modelNames(host.Tools()); len(got) != 0 {
			t.Fatalf("after grace tools = %v", got)
		}
		if _, err := host.BindClientStream(testClientA); !errors.Is(err, ErrClientInstanceNotFound) {
			t.Fatalf("bind after expiry error = %v", err)
		}
	})
}

func TestClientReconnectWithinGraceKeepsRegistrations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		host := clientHost(t, newClientCalls())
		first := attachBound(t, host, testClientA)
		before := toolNamed(t, host, "browser__snapshot")
		host.ReleaseClientStream(first)
		time.Sleep(DefaultClientGrace / 2)
		attachBound(t, host, testClientA)
		time.Sleep(DefaultClientGrace)
		synctest.Wait()
		after := toolNamed(t, host, "browser__snapshot")
		if after.Registration != before.Registration || after.Owner != before.Owner {
			t.Fatalf("redeclaration changed registration: before %#v after %#v", before, after)
		}

		changed := append([]json.RawMessage(nil), testClientTools[0])
		if err := host.AttachClient(ClientAttachment{InstanceID: testClientA, PluginID: "browser", Tools: changed}); err != nil {
			t.Fatal(err)
		}
		if got := toolNamed(t, host, "browser__snapshot"); got.Registration == before.Registration {
			t.Fatalf("changed declaration kept registration %d", got.Registration)
		}
		if got := modelNames(host.Tools()); len(got) != 1 {
			t.Fatalf("tools after changed declaration = %v", got)
		}
	})
}

func TestClientToolCallsReachTheHostingInstanceAndSettleOnce(t *testing.T) {
	calls := newClientCalls()
	host := clientHost(t, calls)
	binding := attachBound(t, host, testClientA)
	tool := toolNamed(t, host, "browser__snapshot")

	type outcome struct {
		result ToolResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := host.ExecuteTool(t.Context(), tool, ToolCall{ID: "tool_1", TurnID: "turn_1"}, json.RawMessage(`{"tab":"t1"}`))
		done <- outcome{result, err}
	}()
	request := <-calls.published
	if request.InstanceID != testClientA || request.PluginID != "browser" || request.ToolID != "snapshot" || request.Call != (ToolCall{ID: "tool_1", TurnID: "turn_1"}) || string(request.Input) != `{"tab":"t1"}` || !strings.HasPrefix(request.CallID, "clientcall_") {
		t.Fatalf("request = %#v", request)
	}
	if !host.ClientCallDeliverable(binding, request.CallID) {
		t.Fatal("pending call is not deliverable on its instance's stream")
	}
	if err := host.CompleteClientCall(testClientB, request.CallID, json.RawMessage(`{"content":[]}`), ""); !errors.Is(err, ErrClientCallConflict) {
		t.Fatalf("foreign completion error = %v", err)
	}
	if err := host.CompleteClientCall(testClientA, request.CallID, json.RawMessage(`{"content":[],"terminate":true}`), ""); err == nil {
		t.Fatal("terminate result was accepted")
	}
	if err := host.CompleteClientCall(testClientA, request.CallID, json.RawMessage(`{"content":[{"type":"text","text":"outline"}],"details":{"origin":"https://example.com"}}`), ""); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || len(got.result.Content) != 1 || got.result.Content[0].Text != "outline" || string(got.result.Details) != `{"origin":"https://example.com"}` {
		t.Fatalf("result = %#v, err = %v", got.result, got.err)
	}
	if err := host.CompleteClientCall(testClientA, request.CallID, json.RawMessage(`{"content":[]}`), ""); !errors.Is(err, ErrClientCallConflict) {
		t.Fatalf("second completion error = %v", err)
	}
	if host.ClientCallDeliverable(binding, request.CallID) {
		t.Fatal("settled call is still deliverable")
	}
	if got := calls.outcomes(); len(got) != 1 || got[0] != ClientCallCompleted {
		t.Fatalf("outcomes = %v", got)
	}

	go func() {
		_, err := host.ExecuteTool(t.Context(), tool, ToolCall{ID: "tool_2"}, json.RawMessage(`{"tab":"t1"}`))
		done <- outcome{err: err}
	}()
	request = <-calls.published
	if err := host.CompleteClientCall(testClientA, request.CallID, nil, "no tab t1"); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got.err == nil || got.err.Error() != "browser__snapshot failed: no tab t1" {
		t.Fatalf("failed call error = %v", got.err)
	}
	if _, err := host.ExecuteTool(t.Context(), tool, ToolCall{ID: "tool_3"}, json.RawMessage(`{"tab":1}`)); err == nil || !strings.Contains(err.Error(), "does not match schema") {
		t.Fatalf("invalid input error = %v", err)
	}
}

func TestClientToolCallsEndWhenWithdrawnTimedOutOrStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := newClientCalls()
		host := clientHost(t, calls, func(config *HostConfig) { config.ClientCallTimeout = time.Minute })
		binding := attachBound(t, host, testClientA)
		tool := toolNamed(t, host, "browser__snapshot")
		input := json.RawMessage(`{"tab":"t1"}`)

		ctx, cancel := context.WithCancel(t.Context())
		errs := make(chan error, 1)
		go func() {
			_, err := host.ExecuteTool(ctx, tool, ToolCall{ID: "tool_1"}, input)
			errs <- err
		}()
		withdrawn := <-calls.published
		cancel()
		if err := <-errs; !errors.Is(err, context.Canceled) {
			t.Fatalf("withdrawn error = %v", err)
		}
		if err := host.CompleteClientCall(testClientA, withdrawn.CallID, json.RawMessage(`{"content":[]}`), ""); !errors.Is(err, ErrClientCallConflict) {
			t.Fatalf("late completion error = %v", err)
		}

		go func() {
			_, err := host.ExecuteTool(t.Context(), tool, ToolCall{ID: "tool_2"}, input)
			errs <- err
		}()
		<-calls.published
		time.Sleep(time.Minute)
		if err := <-errs; err == nil || err.Error() != "browser__snapshot timed out after 1m0s; effects may have partially completed" {
			t.Fatalf("timed out error = %v", err)
		}

		go func() {
			_, err := host.ExecuteTool(t.Context(), tool, ToolCall{ID: "tool_3"}, input)
			errs <- err
		}()
		<-calls.published
		host.ReleaseClientStream(binding)
		time.Sleep(DefaultClientGrace)
		if err := <-errs; err == nil || !strings.Contains(err.Error(), "is no longer attached") {
			t.Fatalf("stopped error = %v", err)
		}
		want := []ClientCallOutcome{ClientCallWithdrawn, ClientCallTimedOut, ClientCallUnavailable}
		if got := calls.outcomes(); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Fatalf("outcomes = %v, want %v", got, want)
		}
	})
}

func TestClientCallsStayWithTheirInstanceWhenSuperseded(t *testing.T) {
	calls := newClientCalls()
	host := clientHost(t, calls)
	older := attachBound(t, host, testClientA)
	tool := toolNamed(t, host, "browser__snapshot")
	errs := make(chan error, 1)
	go func() {
		_, err := host.ExecuteTool(t.Context(), tool, ToolCall{ID: "tool_1"}, json.RawMessage(`{"tab":"t1"}`))
		errs <- err
	}()
	request := <-calls.published
	newer := attachBound(t, host, testClientB)
	if host.ClientCallDeliverable(newer, request.CallID) || !host.ClientCallDeliverable(older, request.CallID) {
		t.Fatal("call moved to the superseding instance")
	}
	if _, err := host.ExecuteTool(t.Context(), tool, ToolCall{ID: "tool_2"}, json.RawMessage(`{"tab":"t1"}`)); !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("new call to a superseded instance error = %v", err)
	}
	if err := host.CompleteClientCall(testClientA, request.CallID, json.RawMessage(`{"content":[]}`), ""); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatalf("superseded instance call error = %v", err)
	}

	// A second stream for the same instance replaces the first.
	replacement, err := host.BindClientStream(testClientB)
	if err != nil {
		t.Fatal(err)
	}
	current := toolNamed(t, host, "browser__snapshot")
	go func() {
		_, err := host.ExecuteTool(t.Context(), current, ToolCall{ID: "tool_3"}, json.RawMessage(`{"tab":"t1"}`))
		errs <- err
	}()
	request = <-calls.published
	if host.ClientCallDeliverable(newer, request.CallID) || !host.ClientCallDeliverable(replacement, request.CallID) {
		t.Fatal("call is deliverable on a replaced stream")
	}
	host.ReleaseClientStream(newer)
	if !host.ClientCallDeliverable(replacement, request.CallID) {
		t.Fatal("releasing a replaced stream unbound the instance")
	}
	if err := host.CompleteClientCall(testClientB, request.CallID, json.RawMessage(`{"content":[]}`), ""); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

func TestProcessPluginHidesClientToolsWithTheSameID(t *testing.T) {
	host := clientHost(t, newClientCalls())
	attachBound(t, host, testClientA)
	host.mu.Lock()
	host.entries = append(host.entries, &hostEntry{owner: InstanceID{HostID: host.id, PluginID: "browser", Generation: 99}, epoch: host.view.epoch})
	host.mu.Unlock()
	if got := modelNames(host.Tools()); len(got) != 0 {
		t.Fatalf("conflicting client tools = %v", got)
	}
	want := []string{"Plugins: browser: an attached client's tools are hidden by the server plugin with the same ID"}
	if got := host.Warnings(); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

func TestClosingTheHostEndsClientCalls(t *testing.T) {
	calls := newClientCalls()
	host := NewHost(context.Background(), HostConfig{Session: SessionContext{ID: "session-one"}, ClientCallRequested: calls.requested, ClientCallResolved: calls.resolved})
	attachBound(t, host, testClientA)
	tool := toolNamed(t, host, "browser__snapshot")
	errs := make(chan error, 1)
	go func() {
		_, err := host.ExecuteTool(context.Background(), tool, ToolCall{ID: "tool_1"}, json.RawMessage(`{"tab":"t1"}`))
		errs <- err
	}()
	<-calls.published
	if err := host.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err == nil || !strings.Contains(err.Error(), "is no longer attached") {
		t.Fatalf("error after close = %v", err)
	}
	if err := host.AttachClient(ClientAttachment{InstanceID: testClientB, PluginID: "browser"}); !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("attach after close error = %v", err)
	}
}

func TestPendingClientCallsListUnsettledCalls(t *testing.T) {
	calls := newClientCalls()
	host := clientHost(t, calls)
	attachBound(t, host, testClientA)
	tool := toolNamed(t, host, "browser__snapshot")
	errs := make(chan error, 1)
	go func() {
		_, err := host.ExecuteTool(t.Context(), tool, ToolCall{ID: "tool_1", SubagentConversationID: "subagent_1"}, json.RawMessage(`{"tab":"t1"}`))
		errs <- err
	}()
	request := <-calls.published
	pending := host.PendingClientCalls(testClientA)
	if len(pending) != 1 || pending[0].CallID != request.CallID || pending[0].Call.SubagentConversationID != "subagent_1" || string(pending[0].Input) != `{"tab":"t1"}` {
		t.Fatalf("pending = %#v", pending)
	}
	if got := host.PendingClientCalls(testClientB); len(got) != 0 {
		t.Fatalf("other instance pending = %#v", got)
	}
	if err := host.CompleteClientCall(testClientA, request.CallID, json.RawMessage(`{"content":[]}`), ""); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if got := host.PendingClientCalls(testClientA); len(got) != 0 {
		t.Fatalf("pending after completion = %#v", got)
	}
}

func TestClientToolChangesRefreshTheCatalogWithoutInvalidatingClients(t *testing.T) {
	var mu sync.Mutex
	var changed, clientChanged int
	host := clientHost(t, newClientCalls(), func(config *HostConfig) {
		config.Home = apphome.FromHome(t.TempDir())
		config.CWD = t.TempDir()
		config.Changed = func() { mu.Lock(); changed++; mu.Unlock() }
		config.ClientToolsChanged = func() { mu.Lock(); clientChanged++; mu.Unlock() }
	})
	host.Start()
	counts := func() (int, int) { mu.Lock(); defer mu.Unlock(); return changed, clientChanged }
	attachBound(t, host, testClientA)
	eventuallyHost(t, func() bool { _, client := counts(); return client == 1 })
	if err := host.DetachClient(testClientA); err != nil {
		t.Fatal(err)
	}
	eventuallyHost(t, func() bool { _, client := counts(); return client == 2 })
	if got, _ := counts(); got != 0 {
		t.Fatalf("Changed called %d times for client tool changes", got)
	}
}
