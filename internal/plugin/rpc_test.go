package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func rpcTestPeer(t *testing.T, handlers RPCHandlers, limits rpcLimits) (*RPCEndpoint, net.Conn, *bufio.Reader) {
	t.Helper()
	local, remote := net.Pipe()
	_ = remote.SetDeadline(time.Now().Add(5 * time.Second))
	endpoint := newRPCEndpoint(t.Context(), local, local, handlers, limits)
	t.Cleanup(func() {
		endpoint.Close(nil)
		remote.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		select {
		case <-endpoint.done:
		case <-ctx.Done():
			t.Error("RPC workers did not stop")
		}
	})
	return endpoint, remote, bufio.NewReader(remote)
}

func rpcRead(t *testing.T, reader *bufio.Reader) ([]rpcMessage, bool) {
	t.Helper()
	data, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	messages, batch, err := decodeRPCFrame(data)
	if err != nil {
		t.Fatalf("response %s: %v", data, err)
	}
	return messages, batch
}

func rpcWritePeer(t *testing.T, peer io.Writer, frame string) {
	t.Helper()
	if _, err := io.WriteString(peer, frame+"\n"); err != nil {
		t.Fatal(err)
	}
}

func TestRPCNestedCallsAndIndependentIDNamespaces(t *testing.T) {
	var endpoint *RPCEndpoint
	handlers := RPCHandlers{Request: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		if method != "outer" {
			return nil, rpcError(-32601, "Method not found")
		}
		return endpoint.Call(ctx, "nested", params)
	}}
	endpoint, peer, reader := rpcTestPeer(t, handlers, defaultRPCLimits)
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":"kit-1","method":"outer","params":{"value":7}}`)
	messages, _ := rpcRead(t, reader)
	if len(messages) != 1 || *messages[0].Method != "nested" || string(messages[0].Params) != `{"value":7}` || string(messages[0].ID) != `"kit-1"` {
		t.Fatalf("nested = %#v", messages)
	}
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":"kit-1","result":"nested-result"}`)
	messages, _ = rpcRead(t, reader)
	if string(messages[0].ID) != `"kit-1"` || string(messages[0].Result) != `"nested-result"` {
		t.Fatalf("outer result = %#v", messages)
	}
}

func TestRPCConcurrentCallsCorrelateOutOfOrder(t *testing.T) {
	endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{}, defaultRPCLimits)
	results := make(chan rpcResult, 2)
	for _, method := range []string{"first", "second"} {
		go func() {
			value, err := endpoint.Call(t.Context(), method, nil)
			if err == nil && string(value) != fmt.Sprintf("%q", method) {
				err = fmt.Errorf("%s got %s", method, value)
			}
			results <- rpcResult{value, err}
		}()
	}
	first, _ := rpcRead(t, reader)
	second, _ := rpcRead(t, reader)
	for _, message := range []rpcMessage{second[0], first[0]} {
		rpcWritePeer(t, peer, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%q}`, message.ID, *message.Method))
	}
	for range 2 {
		if result := <-results; result.err != nil {
			t.Fatal(result.err)
		}
	}
}

func TestRPCIncomingCancellationAndLateHandlerResult(t *testing.T) {
	started := make(chan struct{})
	returned := make(chan struct{})
	handlers := RPCHandlers{Request: func(ctx context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
		if method != "wait" {
			return nil, rpcError(-32601, "Method not found")
		}
		close(started)
		<-ctx.Done()
		defer close(returned)
		return json.RawMessage(`"late"`), nil
	}}
	_, peer, reader := rpcTestPeer(t, handlers, defaultRPCLimits)
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":1.0,"method":"wait"}`)
	<-started
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","method":"kit/cancel","params":{"id":1}}`)
	messages, _ := rpcRead(t, reader)
	if messages[0].Error == nil || messages[0].Error.Code != -32001 {
		t.Fatalf("cancel result = %#v", messages)
	}
	<-returned
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":2,"method":"unknown"}`)
	messages, _ = rpcRead(t, reader)
	if string(messages[0].ID) != "2" || messages[0].Error == nil || messages[0].Error.Code != -32601 {
		t.Fatalf("stale handler response: %#v", messages)
	}
}

func TestRPCOutgoingCancellationIgnoresLateResponse(t *testing.T) {
	endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{}, defaultRPCLimits)
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { _, err := endpoint.Call(ctx, "wait", nil); result <- err }()
	request, _ := rpcRead(t, reader)
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	cancellation, _ := rpcRead(t, reader)
	if *cancellation[0].Method != "kit/cancel" || string(cancellation[0].Params) != fmt.Sprintf(`{"id":%s}`, request[0].ID) {
		t.Fatalf("cancel notification = %#v", cancellation)
	}
	rpcWritePeer(t, peer, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":"late"}`, request[0].ID))
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":"peer-next","method":"unknown"}`)
	next, _ := rpcRead(t, reader)
	if string(next[0].ID) != `"peer-next"` || next[0].Error == nil || next[0].Error.Code != -32601 {
		t.Fatalf("next = %#v", next)
	}
	if err := endpoint.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestRPCBatchRequestsNotificationsAndResponses(t *testing.T) {
	notices := make(chan string, 2)
	endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{
		Request: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(fmt.Sprintf("%q", method)), nil
		},
		Notification: func(ctx context.Context, method string, params json.RawMessage) error { notices <- method; return nil },
	}, defaultRPCLimits)
	call := make(chan rpcResult, 1)
	go func() { value, err := endpoint.Call(t.Context(), "outgoing", nil); call <- rpcResult{value, err} }()
	outgoing, _ := rpcRead(t, reader)
	rpcWritePeer(t, peer, fmt.Sprintf(`[{"jsonrpc":"2.0","id":1,"method":"one"},{"jsonrpc":"2.0","method":"notice-one"},{"jsonrpc":"2.0","id":%s,"result":true},{"jsonrpc":"2.0","id":"two","method":"two"},{"jsonrpc":"2.0","method":"notice-two"}]`, outgoing[0].ID))
	replies, batch := rpcRead(t, reader)
	if !batch || len(replies) != 2 || string(replies[0].Result) != `"one"` || string(replies[1].Result) != `"two"` {
		t.Fatalf("batch = %#v (%v)", replies, batch)
	}
	if result := <-call; result.err != nil || string(result.value) != "true" {
		t.Fatalf("outgoing = %#v", result)
	}
	if first, second := <-notices, <-notices; first != "notice-one" || second != "notice-two" {
		t.Fatalf("notifications = %q, %q", first, second)
	}
}

func TestRPCNotificationHandlerCanMakeNestedCall(t *testing.T) {
	var endpoint *RPCEndpoint
	completed := make(chan error, 1)
	handlers := RPCHandlers{Notification: func(ctx context.Context, _ string, _ json.RawMessage) error {
		_, err := endpoint.Call(ctx, "from-notification", nil)
		completed <- err
		return err
	}}
	endpoint, peer, reader := rpcTestPeer(t, handlers, defaultRPCLimits)
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","method":"event"}`)
	nested, _ := rpcRead(t, reader)
	rpcWritePeer(t, peer, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":null}`, nested[0].ID))
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
}

func TestRPCIncomingLimit(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	handlers := RPCHandlers{Request: func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	}}
	limits := defaultRPCLimits
	limits.incoming = 1
	_, peer, reader := rpcTestPeer(t, handlers, limits)
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":1,"method":"wait"}`)
	<-started
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":2,"method":"busy"}`)
	replies, _ := rpcRead(t, reader)
	if string(replies[0].ID) != "2" || replies[0].Error == nil || replies[0].Error.Code != -32006 {
		t.Fatalf("busy = %#v", replies)
	}
	close(release)
	replies, _ = rpcRead(t, reader)
	if string(replies[0].ID) != "1" || string(replies[0].Result) != "null" {
		t.Fatalf("original = %#v", replies)
	}
}

func TestRPCFrameLimitsAndMalformedOutput(t *testing.T) {
	for name, frame := range map[string]string{
		"invalid JSON":     "not protocol",
		"invalid UTF-8":    string([]byte{0xff}),
		"oversize":         strings.Repeat("x", 257),
		"null params":      `{"jsonrpc":"2.0","id":1,"method":"x","params":null}`,
		"fractional ID":    `{"jsonrpc":"2.0","id":1.5,"method":"x"}`,
		"unknown response": `{"jsonrpc":"2.0","id":"never-sent","result":null}`,
		"wrong casing":     `{"JSONRPC":"2.0","id":1,"method":"x"}`,
		"mixed shape":      `{"jsonrpc":"2.0","id":1,"method":"x","result":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			limits := defaultRPCLimits
			limits.frame = 256
			endpoint, peer, _ := rpcTestPeer(t, RPCHandlers{}, limits)
			rpcWritePeer(t, peer, frame)
			select {
			case <-endpoint.closed:
			case <-time.After(5 * time.Second):
				t.Fatal("invalid frame did not fail connection")
			}
			if endpoint.Err() == nil {
				t.Fatal("missing failure")
			}
		})
	}
}

func TestRPCEmptyCRLFAndSplitFrames(t *testing.T) {
	_, peer, reader := rpcTestPeer(t, RPCHandlers{}, defaultRPCLimits)
	for _, part := range []string{"\n\r\n", `{"jsonrpc":"2.0",`, `"id":1,"method":"unknown"}`, "\r\n"} {
		if _, err := io.WriteString(peer, part); err != nil {
			t.Fatal(err)
		}
	}
	replies, _ := rpcRead(t, reader)
	if replies[0].Error == nil || replies[0].Error.Code != -32601 {
		t.Fatalf("response = %#v", replies)
	}
}

func TestRPCOversizedResultBecomesFailedResponse(t *testing.T) {
	limits := defaultRPCLimits
	limits.frame = 256
	endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{Request: func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(fmt.Sprintf("%q", strings.Repeat("x", 512))), nil
	}}, limits)
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":1,"method":"large"}`)
	replies, _ := rpcRead(t, reader)
	if replies[0].Error == nil || replies[0].Error.Code != -32005 {
		t.Fatalf("response = %#v", replies)
	}
	if err := endpoint.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestRPCQueueOverflowIncludesBlockedWrite(t *testing.T) {
	limits := defaultRPCLimits
	limits.queued = 160
	endpoint, _, _ := rpcTestPeer(t, RPCHandlers{}, limits)
	frame := requestMessage(nil, "blocked", json.RawMessage(`{"data":"abcdefghijklmnop"}`))
	for range 10 {
		if _, err := endpoint.enqueue(frame); err != nil {
			break
		}
	}
	select {
	case <-endpoint.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("queue remained unbounded")
	}
	var failure *RPCError
	if !errors.As(endpoint.Err(), &failure) || failure.Code != -32005 {
		t.Fatalf("failure = %v", endpoint.Err())
	}
}

func TestRPCCloseCancelsBothDirections(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{Request: func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}}, defaultRPCLimits)
	outgoing := make(chan error, 1)
	go func() { _, err := endpoint.Call(t.Context(), "outgoing", nil); outgoing <- err }()
	rpcRead(t, reader)
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":7,"method":"incoming"}`)
	<-started
	failure := errors.New("plugin crashed")
	endpoint.Close(failure)
	if err := <-outgoing; !errors.Is(err, failure) {
		t.Fatalf("outgoing = %v", err)
	}
	<-stopped
	if err := endpoint.Wait(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("wait = %v", err)
	}
}

func TestRPCRequestIDCanonicalization(t *testing.T) {
	for _, group := range [][]string{{"1", "1.0", "10e-1", "0.01e2"}, {"0", "-0", "0e999999"}, {"1000", "1e3", "10e2"}, {`"a"`, `"\u0061"`}} {
		expected, err := requestKey(json.RawMessage(group[0]))
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range group {
			got, err := requestKey(json.RawMessage(raw))
			if err != nil || got != expected {
				t.Errorf("%s = %q, %v; want %q", raw, got, err, expected)
			}
		}
	}
	var keys []string
	for _, raw := range []string{`"1"`, "1", "9007199254740992", "9007199254740993", "1e999999999999999999999"} {
		key, err := requestKey(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	for i, key := range keys {
		for j := i + 1; j < len(keys); j++ {
			if key == keys[j] {
				t.Fatalf("IDs collided: %v", keys)
			}
		}
	}
	for _, raw := range []string{"null", "true", "1.00000000000000001", "1e-3", "[]"} {
		if _, err := requestKey(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted ID %s", raw)
		}
	}
}

func TestRPCMixedInvalidBatchAndEmptyBatch(t *testing.T) {
	for _, test := range []struct {
		frame string
		batch bool
		codes []int64
	}{
		{`[{"jsonrpc":"2.0","id":1,"method":"unknown"},17,{"jsonrpc":"2.0","id":2,"method":"unknown"}]`, true, []int64{-32601, -32600, -32601}},
		{`[]`, false, []int64{-32600}},
	} {
		t.Run(test.frame, func(t *testing.T) {
			endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{}, defaultRPCLimits)
			rpcWritePeer(t, peer, test.frame)
			messages, batch := rpcRead(t, reader)
			if batch != test.batch || len(messages) != len(test.codes) {
				t.Fatalf("messages = %#v, batch %v", messages, batch)
			}
			for i, code := range test.codes {
				if messages[i].Error == nil || messages[i].Error.Code != code {
					t.Fatalf("message %d = %#v", i, messages[i])
				}
			}
			if endpoint.Err() != nil {
				t.Fatal(endpoint.Err())
			}
		})
	}
}

func TestRPCInvalidRequestReportsErrorBeforeClosing(t *testing.T) {
	endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{}, defaultRPCLimits)
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":9,"method":"bad","params":null}`)
	messages, _ := rpcRead(t, reader)
	if string(messages[0].ID) != "9" || messages[0].Error == nil || messages[0].Error.Code != -32600 {
		t.Fatalf("invalid request = %#v", messages)
	}
	select {
	case <-endpoint.closed:
	case <-time.After(time.Second):
		t.Fatal("strict protocol failure did not close")
	}
}

func TestRPCUnterminatedFrameLimit(t *testing.T) {
	limits := defaultRPCLimits
	limits.frame = 256
	endpoint, peer, _ := rpcTestPeer(t, RPCHandlers{}, limits)
	if _, err := io.WriteString(peer, strings.Repeat(" ", 257)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-endpoint.closed:
	case <-time.After(time.Second):
		t.Fatal("reader waited for delimiter after frame exceeded limit")
	}
}

func TestRPCBatchOversizeReplacesPriorErrorData(t *testing.T) {
	limits := defaultRPCLimits
	limits.frame = 512
	large := strings.Repeat("x", 330)
	endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{Request: func(ctx context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
		if method == "error" {
			return nil, &RPCError{Code: -32000, Message: "large", Data: json.RawMessage(fmt.Sprintf("%q", large))}
		}
		return json.RawMessage(fmt.Sprintf("%q", large)), nil
	}}, limits)
	rpcWritePeer(t, peer, `[{"jsonrpc":"2.0","id":1,"method":"error"},{"jsonrpc":"2.0","id":2,"method":"result"}]`)
	messages, batch := rpcRead(t, reader)
	if !batch || len(messages) != 2 {
		t.Fatalf("batch = %#v (%v)", messages, batch)
	}
	for _, message := range messages {
		if message.Error == nil || message.Error.Code != -32005 || len(message.Error.Data) != 0 {
			t.Fatalf("limit response = %#v", message)
		}
	}
	if endpoint.Err() != nil {
		t.Fatal(endpoint.Err())
	}
}

func TestRPCBatchExactEncodedBoundary(t *testing.T) {
	result := json.RawMessage(fmt.Sprintf("%q", strings.Repeat("x", 200)))
	expected := []rpcMessage{responseMessage(json.RawMessage("1"), result, nil), responseMessage(json.RawMessage("2"), result, nil)}
	encoded, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []int{0, -1} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			limits := defaultRPCLimits
			limits.frame = len(encoded) + extra
			_, peer, reader := rpcTestPeer(t, RPCHandlers{Request: func(context.Context, string, json.RawMessage) (json.RawMessage, error) { return result, nil }}, limits)
			rpcWritePeer(t, peer, `[{"jsonrpc":"2.0","id":1,"method":"result"},{"jsonrpc":"2.0","id":2,"method":"result"}]`)
			messages, batch := rpcRead(t, reader)
			if !batch || len(messages) != 2 {
				t.Fatalf("batch = %#v", messages)
			}
			for _, message := range messages {
				if extra == 0 {
					if string(message.Result) != string(result) {
						t.Fatalf("exact-limit result = %#v", message)
					}
				} else if message.Error == nil || message.Error.Code != -32005 {
					t.Fatalf("over-limit result = %#v", message)
				}
			}
		})
	}
}

func TestRPCCodecNumericCodesEscapesAndLargeExponents(t *testing.T) {
	messages, _, err := decodeRPCFrame([]byte(`{"jsonrpc":"2\u002e0","id":1,"error":{"code":-32001.0,"message":"cancelled"}}`))
	if err != nil || messages[0].Error.Code != -32001 {
		t.Fatalf("numeric code = %#v, %v", messages, err)
	}
	exponent := strings.Repeat("9", 1024*1024)
	key, err := requestKey(json.RawMessage("10e" + exponent))
	if err != nil || key != "n:1e1"+strings.Repeat("0", len(exponent)) {
		t.Fatalf("large exponent normalization failed: %v (length %d)", err, len(key))
	}
}

func TestRPCParentCancellationAndNotificationFlood(t *testing.T) {
	t.Run("parent", func(t *testing.T) {
		parent, cancel := context.WithCancel(t.Context())
		local, remote := net.Pipe()
		defer remote.Close()
		endpoint := NewRPCEndpoint(parent, local, local, RPCHandlers{})
		cancel()
		ctx, stop := context.WithTimeout(t.Context(), time.Second)
		defer stop()
		if err := endpoint.Wait(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("parent cancellation = %v", err)
		}
	})
	t.Run("notifications", func(t *testing.T) {
		started := make(chan struct{})
		limits := defaultRPCLimits
		limits.notifications = 1
		endpoint, peer, _ := rpcTestPeer(t, RPCHandlers{Notification: func(ctx context.Context, _ string, _ json.RawMessage) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}}, limits)
		rpcWritePeer(t, peer, `{"jsonrpc":"2.0","method":"first"}`)
		<-started
		rpcWritePeer(t, peer, `{"jsonrpc":"2.0","method":"second"}`)
		rpcWritePeer(t, peer, `{"jsonrpc":"2.0","method":"overflow"}`)
		select {
		case <-endpoint.closed:
		case <-time.After(time.Second):
			t.Fatal("notification queue was not bounded")
		}
	})
}

func TestRPCBatchMetadataBound(t *testing.T) {
	frame := "[" + strings.Repeat("0,", MaxBatchMessages) + "0]"
	_, _, err := decodeRPCFrame([]byte(frame))
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32005 {
		t.Fatalf("batch metadata bound = %v", err)
	}
}

func TestRPCAdmissionRejectsBeforeEnqueueAndDoesNotHoldResponseWait(t *testing.T) {
	endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{}, defaultRPCLimits)
	rejected := errors.New("admission rejected")
	if _, err := endpoint.callAdmitted(t.Context(), "rejected", nil, nil, func(func() error) error { return rejected }); !errors.Is(err, rejected) {
		t.Fatalf("rejection = %v", err)
	}
	endpoint.mu.Lock()
	pendingCount := len(endpoint.pending)
	endpoint.mu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("pending after rejection = %d", pendingCount)
	}
	admitted := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := endpoint.callAdmitted(t.Context(), "accepted", nil, nil, func(enqueue func() error) error { defer close(admitted); return enqueue() })
		result <- err
	}()
	select {
	case <-admitted:
	case <-time.After(time.Second):
		t.Fatal("admission waited for response")
	}
	messages, _ := rpcRead(t, reader)
	if len(messages) != 1 || *messages[0].Method != "accepted" {
		t.Fatalf("first dispatched request = %#v", messages)
	}
	rpcWritePeer(t, peer, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":null}`, messages[0].ID))
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
