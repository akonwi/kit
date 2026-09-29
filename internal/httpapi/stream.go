package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// StreamHeartbeatInterval is the longest a stream stays silent (ADR 0035).
const StreamHeartbeatInterval = 15 * time.Second

// streamWriteTimeout bounds each write and flush to a stalled client.
const streamWriteTimeout = 10 * time.Second

// StreamOperation describes one server-push SSE operation (ADR 0035). Every
// record carries Payload; Records lists the permitted record names.
type StreamOperation[Params, Payload any] struct {
	ID, Tag, Path string
	// Records lists the record names the stream may emit.
	Records []string
	// Resumable streams set the SSE id field; others never do.
	Resumable bool
	// MaxRecordBytes bounds one encoded record: its field lines, including
	// their line terminators, excluding the blank line that dispatches it.
	MaxRecordBytes int
	// Validate applies the payload's semantic rules on both sides.
	Validate func(Payload) error
	// Errors declares failures returned before the stream opens.
	Errors []ErrorResponse
}

// StreamDescriptor is the stream-specific part of a Descriptor.
type StreamDescriptor struct {
	Records        []string
	Resumable      bool
	MaxRecordBytes int
}

// Describe projects a stream operation into its reflection-friendly form.
func (op StreamOperation[Params, Payload]) Describe() Descriptor {
	return Descriptor{ID: op.ID, Tag: op.Tag, Method: http.MethodGet, Path: op.Path, Success: http.StatusOK,
		Params: typeOf[Params](), Input: typeOf[NoBody](), Output: typeOf[Payload](),
		Errors: mergeErrorResponses(op.Errors, CommonErrorResponses),
		Stream: &StreamDescriptor{Records: append([]string(nil), op.Records...), Resumable: op.Resumable, MaxRecordBytes: op.MaxRecordBytes}}
}

func (op StreamOperation[Params, Payload]) permits(name string) bool {
	for _, record := range op.Records {
		if record == name {
			return true
		}
	}
	return false
}

// StreamRecord is one record of a stream. ID is set only on resumable streams.
type StreamRecord[Payload any] struct {
	Name    string
	ID      string
	Payload Payload
}

// StreamError reports a stream that violated its declared framing, bounds,
// record names, or payload schema. Callers treat it as a protocol violation.
type StreamError struct{ Err error }

func (e *StreamError) Error() string { return "session stream: " + e.Err.Error() }
func (e *StreamError) Unwrap() error { return e.Err }

func streamErrorf(format string, args ...any) error {
	return &StreamError{Err: fmt.Errorf(format, args...)}
}

// validRecordName reports whether name is a single-line SSE field value.
func validRecordName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "\r\n")
}
