package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"strconv"
	"sync"
)

// JSON-RPC and ACP error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
	codeCancelled      = -32800
	codeAuthRequired   = -32000
	codeNotFound       = -32002
)

const (
	// maxLineBytes bounds one inbound message. A longer line is discarded up
	// to its newline and answered with a parse error.
	maxLineBytes = 16 << 20
	// maxInFlight bounds the inbound requests handled at once.
	maxInFlight = 64
)

// rpcError is a JSON-RPC error object. It is also the Go error a handler
// returns to choose the code a request is answered with.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

func newError(code int, format string, arguments ...any) *rpcError {
	return &rpcError{Code: code, Message: fmt.Sprintf(format, arguments...)}
}

func invalidParams(format string, arguments ...any) *rpcError {
	return newError(codeInvalidParams, format, arguments...)
}

// asRPCError projects any handler error onto the wire, as an internal error
// unless the handler chose a code.
func asRPCError(err error) *rpcError {
	var target *rpcError
	if errors.As(err, &target) {
		return target
	}
	return &rpcError{Code: codeInternalError, Message: err.Error()}
}

// wireMessage is any inbound JSON-RPC message. A field the sender omitted is
// nil; one it sent as null holds the bytes "null".
type wireMessage struct {
	JSONRPC json.RawMessage `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  json.RawMessage `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type outboundRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outboundNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outboundResult struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
}

type outboundError struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   *rpcError       `json:"error"`
}

var nullID = json.RawMessage("null")

// acceptance is a request a handler accepted. Accepting happens in the order
// requests arrive; run then executes concurrently with later messages, and
// its result or error answers the request.
type acceptance struct {
	run func(context.Context) (any, error)
	// cancel, when set, is how $/cancel_request cancels the request; run still
	// answers it. Otherwise run's context is cancelled and the request is
	// answered with -32800.
	cancel func()
	// after, when set, runs once run's successful result has been sent.
	after func()
}

// handler serves the methods of one side of the protocol.
type handler interface {
	// accept validates and registers a request without blocking on I/O.
	accept(method string, params json.RawMessage) (acceptance, error)
	// notify handles a notification in arrival order.
	notify(method string, params json.RawMessage)
}

// conn is one JSON-RPC 2.0 connection over newline-delimited streams. Only
// conn writes to out, one complete message per line.
type conn struct {
	in      io.Reader
	out     io.Writer
	logger  *slog.Logger
	handler handler

	writeMu  sync.Mutex
	writeErr error

	mu       sync.Mutex
	nextID   int64
	pending  map[string]chan wireMessage
	inFlight map[string]*inFlightRequest

	handlers sync.WaitGroup
}

type inFlightRequest struct {
	cancelled bool
	cancel    func()
}

func newConn(in io.Reader, out io.Writer, logger *slog.Logger) *conn {
	return &conn{
		in: in, out: out, logger: logger,
		pending: map[string]chan wireMessage{}, inFlight: map[string]*inFlightRequest{},
	}
}

// serve reads messages until in ends or ctx is cancelled, then cancels the
// requests still running and waits for them to finish.
func (c *conn) serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		c.handlers.Wait()
	}()
	lines := make(chan lineResult)
	go c.readLines(ctx, lines)
	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-lines:
			if !ok {
				return nil
			}
			if line.err != nil {
				return line.err
			}
			if line.overflow {
				c.writeError(nullID, newError(codeParseError, "message exceeds %d bytes", maxLineBytes))
				continue
			}
			c.dispatch(ctx, line.data)
		}
	}
}

type lineResult struct {
	data     []byte
	overflow bool
	err      error
}

// readLines splits in into lines. A line longer than maxLineBytes is
// reported as overflowing, without its bytes.
func (c *conn) readLines(ctx context.Context, lines chan<- lineResult) {
	defer close(lines)
	reader := bufio.NewReaderSize(c.in, 64<<10)
	var line []byte
	overflow := false
	send := func(result lineResult) bool {
		select {
		case lines <- result:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for {
		chunk, err := reader.ReadSlice('\n')
		if !overflow {
			if len(line)+len(chunk) > maxLineBytes+1 {
				overflow, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(line) > 0 || overflow {
			result := lineResult{data: bytes.TrimRight(line, "\r\n"), overflow: overflow}
			if !send(result) {
				return
			}
		}
		line, overflow = nil, false
		if err != nil {
			if !errors.Is(err, io.EOF) {
				send(lineResult{err: err})
			}
			return
		}
	}
}

// dispatch classifies one line and answers malformed input.
func (c *conn) dispatch(ctx context.Context, line []byte) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return
	}
	if !json.Valid(trimmed) {
		c.writeError(nullID, newError(codeParseError, "parse error: message is not valid JSON"))
		return
	}
	if trimmed[0] != '{' {
		c.writeError(nullID, newError(codeInvalidRequest, "invalid request: expected a JSON-RPC message object"))
		return
	}
	var message wireMessage
	if err := json.Unmarshal(trimmed, &message); err != nil {
		c.writeError(nullID, newError(codeInvalidRequest, "invalid request: %v", err))
		return
	}
	id, idValid := message.ID, validID(message.ID)
	replyID := nullID
	if id != nil && idValid {
		replyID = id
	}
	// A message with a method and no id is a notification, which is never
	// answered, even when it is malformed.
	if message.Method != nil && id == nil {
		var method string
		switch {
		case string(message.JSONRPC) != `"2.0"`:
			c.logger.Warn(`ignoring a notification whose jsonrpc is not "2.0"`)
		case json.Unmarshal(message.Method, &method) != nil || method == "":
			c.logger.Warn("ignoring a notification without a method name")
		case message.Result != nil || message.Error != nil:
			c.logger.Warn("ignoring a notification that carries a result or error", "method", method)
		default:
			c.handleNotification(method, message.Params)
		}
		return
	}
	if string(message.JSONRPC) != `"2.0"` {
		c.writeError(replyID, newError(codeInvalidRequest, `invalid request: jsonrpc must be "2.0"`))
		return
	}
	if id != nil && !idValid {
		c.writeError(nullID, newError(codeInvalidRequest, "invalid request: id must be a string, number, or null"))
		return
	}
	if message.Method != nil {
		var method string
		if err := json.Unmarshal(message.Method, &method); err != nil || method == "" {
			c.writeError(replyID, newError(codeInvalidRequest, "invalid request: method must be a non-empty string"))
			return
		}
		if message.Result != nil || message.Error != nil {
			c.writeError(replyID, newError(codeInvalidRequest, "invalid request: a request cannot carry a result or error"))
			return
		}
		c.handleRequest(ctx, id, method, message.Params)
		return
	}
	if id != nil && (message.Result != nil) != (message.Error != nil) {
		c.handleResponse(message)
		return
	}
	c.writeError(replyID, newError(codeInvalidRequest, "invalid request: expected a request, notification, or response"))
}

// validID reports whether raw is a JSON-RPC id: a string, a number, or null.
func validID(raw json.RawMessage) bool {
	if raw == nil {
		return false
	}
	switch decodeID(raw).(type) {
	case nil, string, json.Number:
		return true
	}
	return false
}

// idKey canonicalizes an id so 7 and "7" are told apart but equal numbers,
// such as 7 and 7.0, match. Numbers are compared exactly, not as float64.
func idKey(raw json.RawMessage) string {
	switch typed := decodeID(raw).(type) {
	case string:
		return "s:" + typed
	case json.Number:
		if exact, ok := new(big.Rat).SetString(typed.String()); ok {
			return "n:" + exact.RatString()
		}
		return "n:" + typed.String()
	case nil:
		return "null"
	}
	return string(raw)
}

// decodeID decodes an id, keeping a number as written. It returns an
// invalidID for anything that is not a single JSON value.
func decodeID(raw json.RawMessage) any {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil || decoder.More() {
		return invalidID{}
	}
	return value
}

type invalidID struct{}

func (c *conn) handleNotification(method string, params json.RawMessage) {
	if method == methodCancelRequest {
		var cancel cancelRequestParams
		if err := json.Unmarshal(params, &cancel); err != nil || !validID(cancel.RequestID) {
			c.logger.Warn("ignoring malformed $/cancel_request")
			return
		}
		c.cancelInbound(idKey(cancel.RequestID))
		return
	}
	c.handler.notify(method, params)
}

func (c *conn) cancelInbound(key string) {
	c.mu.Lock()
	request := c.inFlight[key]
	if request != nil {
		request.cancelled = true
	}
	c.mu.Unlock()
	if request != nil {
		request.cancel()
	}
}

func (c *conn) handleRequest(ctx context.Context, id json.RawMessage, method string, params json.RawMessage) {
	key := idKey(id)
	c.mu.Lock()
	_, duplicate := c.inFlight[key]
	full := len(c.inFlight) >= maxInFlight
	c.mu.Unlock()
	if duplicate {
		c.writeError(id, newError(codeInvalidRequest, "invalid request: id %s is already in use", id))
		return
	}
	if full {
		c.writeError(id, newError(codeInternalError, "too many requests in flight"))
		return
	}
	accepted, err := c.handler.accept(method, params)
	if err != nil {
		c.writeError(id, asRPCError(err))
		return
	}
	runContext, cancel := context.WithCancel(ctx)
	request := &inFlightRequest{cancel: cancel}
	answersCancellation := accepted.cancel == nil
	if !answersCancellation {
		request.cancel = accepted.cancel
	}
	c.mu.Lock()
	c.inFlight[key] = request
	c.mu.Unlock()
	c.handlers.Add(1)
	go func() {
		defer c.handlers.Done()
		defer cancel()
		result, err := accepted.run(runContext)
		c.mu.Lock()
		cancelled := request.cancelled
		delete(c.inFlight, key)
		c.mu.Unlock()
		switch {
		case cancelled && answersCancellation:
			c.writeError(id, newError(codeCancelled, "request cancelled"))
		case err != nil:
			c.writeError(id, asRPCError(err))
		default:
			if result == nil {
				result = struct{}{}
			}
			if c.write(outboundResult{JSONRPC: "2.0", ID: id, Result: result}) == nil && accepted.after != nil {
				accepted.after()
			}
		}
	}()
}

func (c *conn) handleResponse(message wireMessage) {
	key := idKey(message.ID)
	c.mu.Lock()
	reply := c.pending[key]
	delete(c.pending, key)
	c.mu.Unlock()
	if reply == nil {
		c.logger.Debug("ignoring a response to no outstanding request", "id", string(message.ID))
		return
	}
	reply <- message
}

// call sends a request and waits for its response. When ctx ends first, it
// sends $/cancel_request and stops waiting.
func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	key := "n:" + strconv.FormatInt(id, 10)
	reply := make(chan wireMessage, 1)
	c.pending[key] = reply
	c.mu.Unlock()
	if err := c.write(outboundRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		c.forget(key)
		return nil, err
	}
	select {
	case message := <-reply:
		if message.Error != nil {
			var failure rpcError
			if err := json.Unmarshal(message.Error, &failure); err != nil {
				return nil, fmt.Errorf("%s: malformed error response", method)
			}
			return nil, &failure
		}
		return message.Result, nil
	case <-ctx.Done():
		c.forget(key)
		_ = c.notify(methodCancelRequest, map[string]any{"requestId": id})
		return nil, ctx.Err()
	}
}

func (c *conn) forget(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *conn) notify(method string, params any) error {
	return c.write(outboundNotification{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *conn) writeError(id json.RawMessage, failure *rpcError) {
	_ = c.write(outboundError{JSONRPC: "2.0", ID: id, Error: failure})
}

// write sends one message as one line. After a write fails, every later
// write fails with the same error.
func (c *conn) write(message any) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(message); err != nil {
		c.logger.Error("encode protocol message", "err", err)
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.writeErr != nil {
		return c.writeErr
	}
	if _, err := c.out.Write(buffer.Bytes()); err != nil {
		c.writeErr = fmt.Errorf("write protocol message: %w", err)
		c.logger.Error("write protocol message", "err", err)
		return c.writeErr
	}
	return nil
}
