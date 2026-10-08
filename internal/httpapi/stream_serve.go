package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StreamSource yields the records of one open stream. Next blocks until a
// record is due or ctx ends; any other error ends the stream.
type StreamSource[Payload any] interface {
	Next(context.Context) (StreamRecord[Payload], error)
	Close()
}

// HandleStream registers a stream operation on mux. open runs before the
// response starts, so its errors use options.WriteError and the operation's
// declared error body. Once the stream opens, failures end the response and
// are never reported inside it (ADR 0035).
func HandleStream[Params, Payload any](mux *http.ServeMux, options ServeOptions, op StreamOperation[Params, Payload], open func(context.Context, Params) (StreamSource[Payload], error)) {
	registerOperation(op.ID)
	mux.HandleFunc(http.MethodGet+" "+op.Path, func(w http.ResponseWriter, r *http.Request) {
		var params Params
		if err := bindPathParams(r, &params); err != nil {
			options.WriteError(w, NewAPIError(http.StatusBadRequest, ErrorInvalidRequest, "invalid request", nil))
			return
		}
		source, err := open(r.Context(), params)
		if err != nil {
			options.WriteError(w, err)
			return
		}
		defer source.Close()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		if options.StreamShutdown != nil {
			stop := context.AfterFunc(options.StreamShutdown, cancel)
			defer stop()
		}
		serveStream(ctx, w, op, source, options.StreamHeartbeat)
	})
}

// serveStream writes SSE framing, heartbeats, and bounds until ctx ends, the
// client disconnects, the source fails, or a record would violate the operation.
func serveStream[Params, Payload any](streamContext context.Context, w http.ResponseWriter, op StreamOperation[Params, Payload], source StreamSource[Payload], heartbeat time.Duration) {
	if heartbeat <= 0 || heartbeat > StreamHeartbeatInterval {
		heartbeat = StreamHeartbeatInterval
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	defer controller.SetWriteDeadline(time.Time{})
	_ = controller.SetWriteDeadline(time.Now().Add(streamWriteTimeout))
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, ": connected\n\n"); err != nil || controller.Flush() != nil {
		return
	}
	for streamContext.Err() == nil {
		ctx, cancel := context.WithTimeout(streamContext, heartbeat)
		record, err := source.Next(ctx)
		cancel()
		_ = controller.SetWriteDeadline(time.Now().Add(streamWriteTimeout))
		var frame []byte
		switch {
		case errors.Is(err, context.DeadlineExceeded) && streamContext.Err() == nil:
			frame = []byte(": heartbeat\n\n")
		case err != nil:
			return
		default:
			if frame, err = encodeStreamRecord(op, record); err != nil {
				return
			}
		}
		if _, err := w.Write(frame); err != nil || controller.Flush() != nil {
			return
		}
	}
}

// encodeStreamRecord frames one validated record, or fails when the record
// violates the operation's names, resumability, payload rules, or size bound.
func encodeStreamRecord[Params, Payload any](op StreamOperation[Params, Payload], record StreamRecord[Payload]) ([]byte, error) {
	if !validRecordName(record.Name) || !op.permits(record.Name) {
		return nil, fmt.Errorf("undeclared record %q", record.Name)
	}
	if record.ID != "" && (!op.Resumable || strings.ContainsAny(record.ID, "\r\n\x00")) {
		return nil, fmt.Errorf("invalid record id")
	}
	if op.Validate != nil {
		if err := op.Validate(record.Payload); err != nil {
			return nil, err
		}
	}
	// SSE is not HTML. Avoid HTML escaping multiplying valid payload bounds.
	var payload bytes.Buffer
	encoder := json.NewEncoder(&payload)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(record.Payload); err != nil {
		return nil, err
	}
	data := bytes.TrimSuffix(payload.Bytes(), []byte("\n"))
	if len(data) == 0 || data[0] != '{' {
		return nil, fmt.Errorf("record payload is not a JSON object")
	}
	var frame bytes.Buffer
	frame.WriteString("event: " + record.Name + "\n")
	if record.ID != "" {
		frame.WriteString("id: " + record.ID + "\n")
	}
	frame.WriteString("data: ")
	frame.Write(data)
	frame.WriteByte('\n')
	if frame.Len() > op.MaxRecordBytes {
		return nil, fmt.Errorf("record exceeds %d bytes", op.MaxRecordBytes)
	}
	frame.WriteByte('\n')
	return frame.Bytes(), nil
}
