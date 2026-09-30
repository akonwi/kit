package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"
)

// OpenStream starts a stream operation. Pre-stream failures that match the
// operation's declared errors return *APIError; any other non-success
// response, like a success response that is not an event stream, is a
// contract violation and returns *StreamError.
// The caller owns the returned body and reads it with ReadStream.
func OpenStream[Params, Payload any](ctx context.Context, transport Transport, op StreamOperation[Params, Payload], params Params) (io.ReadCloser, error) {
	path, err := operationPath(op.Path, params)
	if err != nil {
		return nil, err
	}
	response, err := transport.DoSessionRequest(ctx, http.MethodGet, path, nil, false)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		encoded, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read daemon session response: %w", err)
		}
		if len(encoded) > maxResponseBytes {
			return nil, streamErrorf("pre-stream error body exceeds %d bytes", maxResponseBytes)
		}
		err = DecodeStreamError(op, response.StatusCode, encoded)
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			// An undeclared status or code, or a body that is not the error
			// envelope, violates the contract like a malformed record does.
			return nil, &StreamError{Err: err}
		}
		return nil, err
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/event-stream" {
		response.Body.Close()
		return nil, streamErrorf("invalid content type")
	}
	return response.Body, nil
}

// ReadStream parses op's records from body in wire order. It ignores
// comments, enforces the record bound, rejects undeclared record names,
// fields, and multi-line data, strictly decodes each payload (unknown fields,
// duplicate keys, missing required fields, trailing data), applies
// op.Validate, and then invokes receive. Violations return *StreamError;
// receive errors are returned unchanged. A clean end of stream returns nil
// and a truncated record is a violation.
func ReadStream[Params, Payload any](body io.Reader, op StreamOperation[Params, Payload], receive func(StreamRecord[Payload]) error) error {
	if op.MaxRecordBytes <= 0 {
		return streamErrorf("operation %s declares no record bound", op.ID)
	}
	reader := bufio.NewReaderSize(body, op.MaxRecordBytes)
	var (
		recordBytes             int
		name, id                string
		data                    []byte
		hasName, hasID, hasData bool
	)
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			return streamErrorf("record exceeds %d bytes", op.MaxRecordBytes)
		}
		if errors.Is(err, io.EOF) {
			if len(line) != 0 || recordBytes != 0 {
				return streamErrorf("unterminated record")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if !utf8.Valid(line) {
			return streamErrorf("invalid UTF-8")
		}
		content := bytes.TrimSuffix(line[:len(line)-1], []byte("\r"))
		if len(content) == 0 {
			if hasName || hasID || hasData {
				record, err := decodeStreamRecord(op, name, id, data, hasName, hasData)
				if err != nil {
					return err
				}
				if err := receive(record); err != nil {
					return err
				}
			}
			recordBytes, name, id, data, hasName, hasID, hasData = 0, "", "", nil, false, false, false
			continue
		}
		recordBytes += len(line)
		if recordBytes > op.MaxRecordBytes {
			return streamErrorf("record exceeds %d bytes", op.MaxRecordBytes)
		}
		if content[0] == ':' {
			continue
		}
		field, value, found := bytes.Cut(content, []byte(":"))
		if !found {
			return streamErrorf("malformed field line")
		}
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "event":
			if hasName {
				return streamErrorf("record repeats its event field")
			}
			name, hasName = string(value), true
		case "id":
			if !op.Resumable {
				return streamErrorf("non-resumable stream sent an id")
			}
			if hasID {
				return streamErrorf("record repeats its id field")
			}
			id, hasID = string(value), true
		case "data":
			if hasData {
				return streamErrorf("record spans multiple data lines")
			}
			data, hasData = append([]byte(nil), value...), true
		default:
			return streamErrorf("undeclared field %q", field)
		}
	}
}

func decodeStreamRecord[Params, Payload any](op StreamOperation[Params, Payload], name, id string, data []byte, hasName, hasData bool) (StreamRecord[Payload], error) {
	var record StreamRecord[Payload]
	if !hasName || !op.permits(name) {
		return record, streamErrorf("undeclared record %q", name)
	}
	if !hasData {
		return record, streamErrorf("record %q has no data", name)
	}
	if err := decodeStrictPayload(data, &record.Payload); err != nil {
		return record, &StreamError{Err: fmt.Errorf("record %q: %w", name, err)}
	}
	if op.Validate != nil {
		if err := op.Validate(record.Payload); err != nil {
			return record, &StreamError{Err: fmt.Errorf("record %q: %w", name, err)}
		}
	}
	record.Name, record.ID = name, id
	return record, nil
}
