package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// RPCHandlers implements the host side of a plugin connection. Request handlers
// may run concurrently and make nested Call requests. Notifications run serially
// in wire order, independently of the reader. All handlers must honor ctx and
// return after cancellation; Go cannot forcibly terminate an internal callback.
// Method-specific payload validation belongs to these typed host adapters.
type RPCHandlers struct {
	// admit runs on the reader before dispatch; instance lifecycle owns this gate.
	admit        func(method string, notification bool) error
	Request      func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	Notification func(ctx context.Context, method string, params json.RawMessage) error
}

type rpcLimits struct{ frame, queued, incoming, outgoing, notifications int }

var defaultRPCLimits = rpcLimits{MaxFrameBytes, MaxQueuedBytes, MaxIncomingRequests, 128, 128}

type rpcResult struct {
	value json.RawMessage
	err   error
}
type rpcPending struct {
	result   chan rpcResult
	validate func(json.RawMessage) error
}
type rpcIncoming struct {
	cancel  context.CancelFunc
	reply   func(rpcMessage)
	settled bool
	timer   *time.Timer
	size    int
}
type rpcWrite struct {
	data    []byte
	written chan error
}
type rpcNotification struct {
	message rpcMessage
	size    int
}

// RPCEndpoint owns full-duplex newline JSON-RPC framing and connection-local
// request identities. It never retargets to a replacement process or session.
// Closing either transport direction cancels all calls in both directions.
type RPCEndpoint struct {
	input          io.ReadCloser
	output         io.WriteCloser
	handlers       RPCHandlers
	limits         rpcLimits
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	wake           *sync.Cond
	closed         chan struct{}
	done           chan struct{}
	err            error
	next           uint64
	pending        map[string]*rpcPending
	incoming       map[string]*rpcIncoming
	cancelled      map[string]bool
	cancelledOrder []string
	queue          []rpcWrite
	queuedBytes    int
	bufferedBytes  int
	notifications  chan rpcNotification
	workers        sync.WaitGroup
	requests       sync.WaitGroup
}

// NewRPCEndpoint takes exclusive ownership of input/output, which must unblock
// pending reads/writes when closed (as process pipes do). Initialization/version
// negotiation and the owning process's shutdown are the lifecycle host's job.
// Incoming payload/batch retention shares a 32 MiB budget; outgoing calls and
// queued notifications are each capped at 128. Batches allow at most 1024 items.
func NewRPCEndpoint(ctx context.Context, input io.ReadCloser, output io.WriteCloser, handlers RPCHandlers) *RPCEndpoint {
	return newRPCEndpoint(ctx, input, output, handlers, defaultRPCLimits)
}

func newRPCEndpoint(parent context.Context, input io.ReadCloser, output io.WriteCloser, handlers RPCHandlers, limits rpcLimits) *RPCEndpoint {
	ctx, cancel := context.WithCancel(parent)
	endpoint := &RPCEndpoint{input: input, output: output, handlers: handlers, limits: limits, ctx: ctx, cancel: cancel, closed: make(chan struct{}), done: make(chan struct{}), pending: make(map[string]*rpcPending), incoming: make(map[string]*rpcIncoming), cancelled: make(map[string]bool), notifications: make(chan rpcNotification, limits.notifications)}
	endpoint.wake = sync.NewCond(&endpoint.mu)
	endpoint.workers.Add(4)
	go func() { defer endpoint.workers.Done(); endpoint.readLoop() }()
	go func() { defer endpoint.workers.Done(); endpoint.writeLoop() }()
	go func() { defer endpoint.workers.Done(); endpoint.notificationLoop() }()
	go func() {
		defer endpoint.workers.Done()
		select {
		case <-parent.Done():
			endpoint.Close(parent.Err())
		case <-endpoint.closed:
		}
	}()
	go func() {
		endpoint.workers.Wait()
		endpoint.requests.Wait()
		for {
			select {
			case <-endpoint.notifications:
				continue
			default:
				endpoint.mu.Lock()
				endpoint.bufferedBytes = 0
				endpoint.mu.Unlock()
				close(endpoint.done)
				return
			}
		}
	}()
	return endpoint
}

// Close cancels owned work and closes the transport without waiting for handlers.
// The first close reason is retained. Call Wait to join context-aware callbacks.
func (e *RPCEndpoint) Close(reason error) {
	if reason == nil {
		reason = rpcError(-32002, "Endpoint is closed")
	}
	e.mu.Lock()
	if e.err != nil {
		e.mu.Unlock()
		return
	}
	e.err = reason
	e.cancel()
	for _, pending := range e.pending {
		pending.result <- rpcResult{err: reason}
	}
	clear(e.pending)
	for _, active := range e.incoming {
		active.settled = true
		active.cancel()
		if active.timer != nil {
			active.timer.Stop()
		}
	}
	e.queue = nil
	e.queuedBytes = 0
	clear(e.cancelled)
	e.cancelledOrder = nil
	close(e.closed)
	e.wake.Broadcast()
	e.mu.Unlock()
	_ = e.input.Close()
	_ = e.output.Close()
}

// Wait joins transport workers and all request handlers, returning the first
// connection failure. A caller deadline does not abandon connection ownership.
func (e *RPCEndpoint) Wait(ctx context.Context) error {
	select {
	case <-e.done:
		return e.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Err reports the first close reason, or nil while the endpoint is open.
func (e *RPCEndpoint) Err() error { e.mu.Lock(); defer e.mu.Unlock(); return e.err }

// Call sends one request and waits for its result, independently of other calls.
// Cancellation is best-effort and sends kit/cancel; it does not roll back effects.
// No general execution deadline is imposed beyond the caller's context.
func (e *RPCEndpoint) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	return e.callValidated(ctx, method, params, nil)
}

// validate runs synchronously on the reader before subsequent frames dispatch.
// It must not call back into the endpoint or block on external work.
func (e *RPCEndpoint) callValidated(ctx context.Context, method string, params json.RawMessage, validate func(json.RawMessage) error) (json.RawMessage, error) {
	return e.callAdmitted(ctx, method, params, validate, func(enqueue func() error) error { return enqueue() })
}

// callAdmitted separates nonblocking enqueue admission from waiting for a reply.
func (e *RPCEndpoint) callAdmitted(ctx context.Context, method string, params json.RawMessage, validate func(json.RawMessage) error, admit func(func() error) error) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRPCRequest(method, params); err != nil {
		return nil, err
	}
	e.mu.Lock()
	if e.err != nil {
		err := e.err
		e.mu.Unlock()
		return nil, err
	}
	if len(e.pending) >= e.limits.outgoing {
		e.mu.Unlock()
		return nil, rpcError(-32006, "Endpoint is busy")
	}
	e.next++
	id := json.RawMessage(strconv.Quote("kit-" + strconv.FormatUint(e.next, 10)))
	key, _ := requestKey(id)
	pending := &rpcPending{result: make(chan rpcResult, 1), validate: validate}
	e.pending[key] = pending
	e.mu.Unlock()
	if err := admit(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := e.enqueue(requestMessage(id, method, params))
		return err
	}); err != nil {
		e.mu.Lock()
		delete(e.pending, key)
		e.mu.Unlock()
		return nil, err
	}
	select {
	case result := <-pending.result:
		return result.value, result.err
	case <-ctx.Done():
		e.mu.Lock()
		if e.pending[key] != pending {
			e.mu.Unlock()
			result := <-pending.result
			return result.value, result.err
		}
		delete(e.pending, key)
		e.cancelled[key] = true
		e.cancelledOrder = append(e.cancelledOrder, key)
		if len(e.cancelledOrder) > 1024 {
			delete(e.cancelled, e.cancelledOrder[0])
			e.cancelledOrder = e.cancelledOrder[1:]
		}
		e.mu.Unlock()
		cancelParams, _ := json.Marshal(struct {
			ID json.RawMessage `json:"id"`
		}{id})
		_, _ = e.enqueue(requestMessage(nil, "kit/cancel", cancelParams))
		return nil, ctx.Err()
	}
}

// tryNotify validates and queues a notification without waiting for pipe IO.
func (e *RPCEndpoint) tryNotify(method string, params json.RawMessage) error {
	if err := validateRPCRequest(method, params); err != nil {
		return err
	}
	_, err := e.enqueue(requestMessage(nil, method, params))
	return err
}

// Notify sends a notification and waits for its frame to be written. Once queued,
// cancellation may stop waiting but cannot retract the notification from the wire.
func (e *RPCEndpoint) Notify(ctx context.Context, method string, params json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRPCRequest(method, params); err != nil {
		return err
	}
	written, err := e.enqueue(requestMessage(nil, method, params))
	if err != nil {
		return err
	}
	select {
	case err := <-written:
		return err
	case <-e.closed:
		return e.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validateRPCRequest(method string, params json.RawMessage) error {
	if !utf8.ValidString(method) || strings.HasPrefix(method, "rpc.") {
		return rpcError(-32600, "Invalid method")
	}
	if len(params) > 0 {
		trimmed := bytes.TrimSpace(params)
		if !utf8.Valid(params) || !json.Valid(params) || len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
			return rpcError(-32602, "Params must be an object or array, or omitted")
		}
	}
	return nil
}

func (e *RPCEndpoint) enqueue(value any) (<-chan error, error) {
	data, err := json.Marshal(value)
	if err != nil || !utf8.Valid(data) {
		return nil, rpcError(-32603, "Invalid JSON-RPC output")
	}
	if len(data) > e.limits.frame {
		return nil, rpcError(-32005, "Response exceeds protocol limits")
	}
	data = append(data, '\n')
	written := make(chan error, 1)
	e.mu.Lock()
	if e.err != nil {
		err := e.err
		e.mu.Unlock()
		return nil, err
	}
	if len(data) > e.limits.queued-e.queuedBytes {
		e.mu.Unlock()
		err := rpcError(-32005, "JSON-RPC outbound queue exceeds limit")
		e.Close(err)
		return nil, err
	}
	e.queue = append(e.queue, rpcWrite{data, written})
	e.queuedBytes += len(data)
	e.wake.Signal()
	e.mu.Unlock()
	return written, nil
}

func (e *RPCEndpoint) writeLoop() {
	for {
		e.mu.Lock()
		for e.err == nil && len(e.queue) == 0 {
			e.wake.Wait()
		}
		if e.err != nil {
			e.mu.Unlock()
			return
		}
		frame := e.queue[0]
		e.queue[0] = rpcWrite{}
		e.queue = e.queue[1:]
		e.mu.Unlock()
		data := frame.data
		var err error
		for len(data) > 0 {
			var n int
			n, err = e.output.Write(data)
			if err != nil {
				break
			}
			if n <= 0 || n > len(data) {
				err = io.ErrShortWrite
				break
			}
			data = data[n:]
		}
		e.mu.Lock()
		if e.err == nil {
			e.queuedBytes -= len(frame.data)
		}
		e.mu.Unlock()
		frame.written <- err
		if err != nil {
			e.Close(fmt.Errorf("plugin RPC output: %w", err))
			return
		}
	}
}

func (e *RPCEndpoint) readLoop() {
	buffer := make([]byte, 4096)
	var frame []byte
	emptyReads := 0
	for {
		n, err := e.input.Read(buffer)
		if n == 0 && err == nil {
			emptyReads++
			if emptyReads >= 100 {
				e.Close(io.ErrNoProgress)
				return
			}
			continue
		}
		emptyReads = 0
		chunk := buffer[:n]
		for len(chunk) > 0 {
			delimiter := bytes.IndexByte(chunk, '\n')
			length := len(chunk)
			if delimiter >= 0 {
				length = delimiter
			}
			if len(frame)+length > e.limits.frame {
				e.Close(errors.New("plugin RPC frame exceeds limit"))
				return
			}
			frame = append(frame, chunk[:length]...)
			chunk = chunk[length:]
			if delimiter < 0 {
				break
			}
			chunk = chunk[1:]
			line := bytes.TrimSuffix(frame, []byte{'\r'})
			if len(line) > 0 {
				messages, batch, decodeErr := decodeRPCFrame(line)
				if decodeErr != nil {
					e.rejectProtocol(line, decodeErr)
					return
				}
				e.dispatch(messages, batch)
			}
			frame = frame[:0]
			if e.Err() != nil {
				return
			}
		}
		if err != nil {
			if len(frame) > 0 {
				err = errors.New("plugin RPC stream ended inside a frame")
			}
			e.Close(fmt.Errorf("plugin RPC input: %w", err))
			return
		}
	}
}

// Attempt the standard JSON-RPC error before terminating strict-profile invalid
// stdout. A peer that does not read cannot postpone teardown indefinitely.
func (e *RPCEndpoint) rejectProtocol(frame []byte, reason error) {
	response := invalidRPCMessage(frame)
	var failure *RPCError
	if errors.As(reason, &failure) {
		response.Error = failure
	}
	written, err := e.enqueue(response)
	if err != nil {
		e.Close(reason)
		return
	}
	e.workers.Add(1)
	go func() {
		defer e.workers.Done()
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-written:
		case <-timer.C:
		case <-e.closed:
		}
		e.Close(reason)
	}()
}

func (e *RPCEndpoint) dispatch(messages []rpcMessage, batch bool) {
	count := 0
	for _, message := range messages {
		if message.invalid || (message.Method != nil && message.ID != nil) {
			count++
		}
	}
	var replies *rpcBatch
	if batch && count > 0 {
		base := count * 128
		for _, message := range messages {
			if message.invalid || (message.Method != nil && message.ID != nil) {
				base += len(message.ID)
			}
		}
		e.mu.Lock()
		if base > e.limits.queued-e.bufferedBytes {
			e.mu.Unlock()
			e.Close(errors.New("plugin batch buffer exceeds limit"))
			return
		}
		e.bufferedBytes += base
		e.mu.Unlock()
		replies = &rpcBatch{endpoint: e, remaining: count, bytes: 1, reserved: base, responses: make([]rpcMessage, count)}
	}
	index := 0
	for _, message := range messages {
		if message.Method == nil && !message.invalid {
			e.acceptResponse(message)
			continue
		}
		if message.ID == nil && !message.invalid {
			e.acceptNotification(message)
			continue
		}
		reply := e.sendResponse
		if replies != nil {
			slot := index
			reply = func(response rpcMessage) { replies.add(slot, response) }
			index++
		}
		if message.invalid {
			if e.handlers.admit != nil {
				if err := e.handlers.admit("", false); err != nil {
					reply(responseMessage(message.ID, nil, err))
					continue
				}
			}
			reply(message)
		} else {
			e.acceptRequest(message, reply)
		}
	}
}

func (e *RPCEndpoint) acceptResponse(message rpcMessage) {
	key, _ := requestKey(message.ID)
	e.mu.Lock()
	pending := e.pending[key]
	if pending != nil {
		delete(e.pending, key)
		result := rpcResult{value: message.Result}
		if message.Error != nil {
			result.err = message.Error
		} else if pending.validate != nil {
			result.err = pending.validate(message.Result)
		}
		if message.Error == nil && result.err != nil {
			e.mu.Unlock()
			// Revoke the endpoint before exposing a malformed response to its
			// caller, which may immediately initiate instance shutdown.
			e.Close(result.err)
			pending.result <- result
			return
		}
		pending.result <- result
		e.mu.Unlock()
		return
	}
	if e.cancelled[key] {
		delete(e.cancelled, key)
		e.mu.Unlock()
		return
	}
	closed := e.err != nil
	e.mu.Unlock()
	if !closed {
		e.Close(errors.New("plugin sent response for unknown request"))
	}
}

func (e *RPCEndpoint) acceptRequest(message rpcMessage, reply func(rpcMessage)) {
	if e.handlers.admit != nil {
		if err := e.handlers.admit(*message.Method, false); err != nil {
			reply(responseMessage(message.ID, nil, err))
			return
		}
	}
	key, _ := requestKey(message.ID)
	size := len(message.ID) + len(message.Params) + len(*message.Method) + 128
	e.mu.Lock()
	if e.err != nil {
		e.mu.Unlock()
		return
	}
	if _, exists := e.incoming[key]; exists {
		e.mu.Unlock()
		reply(responseMessage(message.ID, nil, rpcError(-32600, "Duplicate request id")))
		return
	}
	if len(e.incoming) >= e.limits.incoming || size > e.limits.queued-e.bufferedBytes {
		e.mu.Unlock()
		reply(responseMessage(message.ID, nil, rpcError(-32006, "Endpoint is busy")))
		return
	}
	ctx, cancel := context.WithCancel(e.ctx)
	active := &rpcIncoming{cancel: cancel, reply: reply, size: size}
	e.incoming[key] = active
	e.bufferedBytes += size
	e.requests.Add(1)
	e.mu.Unlock()
	go func() {
		defer e.requests.Done()
		result, err := e.runRequest(ctx, *message.Method, message.Params)
		e.mu.Lock()
		delete(e.incoming, key)
		e.bufferedBytes -= active.size
		settled := active.settled
		active.settled = true
		if active.timer != nil {
			active.timer.Stop()
		}
		cancel()
		e.mu.Unlock()
		if !settled {
			reply(responseMessage(message.ID, result, err))
		}
	}()
}

func (e *RPCEndpoint) runRequest(ctx context.Context, method string, params json.RawMessage) (result json.RawMessage, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("plugin RPC handler panic: %v", failure)
		}
	}()
	if e.handlers.Request == nil {
		return nil, rpcError(-32601, "Method not found")
	}
	return e.handlers.Request(ctx, method, params)
}

func (e *RPCEndpoint) acceptNotification(message rpcMessage) {
	if e.handlers.admit != nil {
		if err := e.handlers.admit(*message.Method, true); err != nil {
			return
		}
	}
	if *message.Method == "kit/cancel" {
		var params map[string]json.RawMessage
		if json.Unmarshal(message.Params, &params) != nil || len(params) != 1 {
			return
		}
		key, err := requestKey(params["id"])
		if err != nil {
			return
		}
		e.mu.Lock()
		active := e.incoming[key]
		if active == nil || active.settled {
			e.mu.Unlock()
			return
		}
		active.settled = true
		active.cancel()
		active.timer = time.AfterFunc(time.Second, func() {
			e.mu.Lock()
			stuck := e.incoming[key] == active
			e.mu.Unlock()
			if stuck {
				e.Close(errors.New("cancelled plugin RPC handler did not stop"))
			}
		})
		e.mu.Unlock()
		active.reply(responseMessage(params["id"], nil, rpcError(-32001, "Request cancelled")))
		return
	}
	size := len(message.Params) + len(*message.Method) + 128
	e.mu.Lock()
	if e.err != nil {
		e.mu.Unlock()
		return
	}
	if size > e.limits.queued-e.bufferedBytes {
		e.mu.Unlock()
		e.Close(errors.New("plugin notification buffer exceeds limit"))
		return
	}
	e.bufferedBytes += size
	e.mu.Unlock()
	select {
	case e.notifications <- rpcNotification{message, size}:
	default:
		e.Close(errors.New("plugin notification queue exceeds limit"))
	}
}

func (e *RPCEndpoint) notificationLoop() {
	for {
		select {
		case <-e.closed:
			return
		case notification := <-e.notifications:
			if e.ctx.Err() != nil {
				return
			}
			err := e.runNotification(*notification.message.Method, notification.message.Params)
			e.mu.Lock()
			e.bufferedBytes -= notification.size
			e.mu.Unlock()
			if err != nil {
				e.Close(err)
				return
			}
		}
	}
}

func (e *RPCEndpoint) runNotification(method string, params json.RawMessage) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("plugin notification handler panic: %v", failure)
		}
	}()
	if e.handlers.Notification == nil {
		return nil
	}
	return e.handlers.Notification(e.ctx, method, params)
}

func (e *RPCEndpoint) sendResponse(response rpcMessage) {
	if _, err := e.enqueue(response); err != nil && e.Err() == nil {
		code := int64(-32603)
		var failure *RPCError
		if errors.As(err, &failure) {
			code = failure.Code
		}
		if _, fallback := e.enqueue(responseMessage(response.ID, nil, rpcError(code, "Response exceeds protocol limits or is invalid"))); fallback != nil {
			e.Close(fallback)
		}
	}
}

type rpcBatch struct {
	mu        sync.Mutex
	endpoint  *RPCEndpoint
	remaining int
	bytes     int
	oversized bool
	reserved  int
	retained  int
	responses []rpcMessage
}

func (b *rpcBatch) add(index int, response rpcMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	encoded, err := json.Marshal(response)
	if err != nil || !utf8.Valid(encoded) {
		response = responseMessage(response.ID, nil, rpcError(-32603, "Invalid handler result"))
		encoded, _ = json.Marshal(response)
	}
	b.bytes += len(encoded) + 1
	if !b.oversized {
		b.endpoint.mu.Lock()
		tooLarge := b.bytes > b.endpoint.limits.frame || len(encoded) > b.endpoint.limits.queued-b.endpoint.bufferedBytes
		if tooLarge {
			b.endpoint.bufferedBytes -= b.retained
			b.retained = 0
		} else {
			b.endpoint.bufferedBytes += len(encoded)
			b.retained += len(encoded)
		}
		b.endpoint.mu.Unlock()
		if tooLarge {
			b.oversized = true
			for i, previous := range b.responses {
				if previous.ID != nil {
					b.responses[i] = responseMessage(previous.ID, nil, rpcError(-32005, "Batch exceeds protocol limits"))
				}
			}
		}
	}
	if b.oversized {
		response = responseMessage(response.ID, nil, rpcError(-32005, "Batch exceeds protocol limits"))
	}
	b.responses[index] = response
	b.remaining--
	if b.remaining == 0 {
		if _, err := b.endpoint.enqueue(b.responses); err != nil {
			b.endpoint.Close(err)
		}
		b.endpoint.mu.Lock()
		b.endpoint.bufferedBytes -= b.reserved + b.retained
		b.endpoint.mu.Unlock()
	}
}
