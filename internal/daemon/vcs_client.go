package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/version"
)

// StreamSessionVCS opens the authenticated live repository-status stream. The
// server sends the latest snapshot first, then deduplicated latest-only updates
// as newline-terminated JSON with blank-line heartbeats.
func (c *Client) StreamSessionVCS(ctx context.Context, sessionID string) (io.ReadCloser, error) {
	registry, err := LoadRegistry(c.paths)
	if err != nil {
		return nil, err
	}
	if err := compatible(registry); err != nil {
		return nil, err
	}
	token, err := loadToken(c.paths)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, registry.URL+"/v1/sessions/"+url.PathEscape(sessionID)+"/vcs/events", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(instanceHeader, registry.InstanceID)
	request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	response, err := c.sessionHTTP.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxSessionResponseBytes))
		return nil, decodeAPIError(response.StatusCode, body)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-ndjson" {
		response.Body.Close()
		return nil, &VCSFrameError{Err: errors.New("invalid session VCS stream content type")}
	}
	return response.Body, nil
}

// maxVCSFrameBytes bounds one complete repository-status stream frame,
// including its terminating newline.
const maxVCSFrameBytes = 64 * 1024

// VCSFrameError marks a terminal repository-stream protocol violation.
type VCSFrameError struct{ Err error }

func (e *VCSFrameError) Error() string { return "session VCS stream frame: " + e.Err.Error() }
func (e *VCSFrameError) Unwrap() error { return e.Err }

// ReadSessionVCS validates bounded, newline-terminated live frames and invokes
// receive in wire order. Blank lines are heartbeats. A final partial frame is a
// protocol violation rather than an update.
func ReadSessionVCS(body io.Reader, receive func(protocol.SessionVCSStatus) error) error {
	reader := bufio.NewReaderSize(body, maxVCSFrameBytes)
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxVCSFrameBytes {
			return &VCSFrameError{Err: errors.New("frame exceeds 64 KiB")}
		}
		if errors.Is(err, io.EOF) {
			if len(line) == 0 {
				return nil
			}
			return &VCSFrameError{Err: errors.New("unterminated frame")}
		}
		if err != nil {
			return err
		}
		frame := line[:len(line)-1]
		if !utf8.Valid(frame) {
			return &VCSFrameError{Err: errors.New("invalid UTF-8")}
		}
		if strings.TrimSpace(string(frame)) == "" {
			continue
		}
		if err := rejectDuplicateJSONKeys(frame); err != nil {
			return &VCSFrameError{Err: err}
		}
		var status protocol.SessionVCSStatus
		if err := decodeStrictJSONObject(frame, &status); err != nil {
			return &VCSFrameError{Err: err}
		}
		// A bool's zero value cannot distinguish false from missing/null.
		var required struct {
			Status *struct {
				Dirty *bool `json:"dirty"`
			} `json:"status"`
		}
		if err := json.Unmarshal(frame, &required); err != nil {
			return &VCSFrameError{Err: err}
		}
		if status.Status != nil && (required.Status == nil || required.Status.Dirty == nil) {
			return &VCSFrameError{Err: errors.New("missing boolean dirty state")}
		}
		if err := status.Validate(); err != nil {
			return &VCSFrameError{Err: err}
		}
		if err := receive(status); err != nil {
			return err
		}
	}
}

// rejectDuplicateJSONKeys rejects ambiguous keys in every nested object.
func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var readValue func(int) error
	readValue = func(depth int) error {
		if depth > 8 {
			return errors.New("VCS frame nesting exceeds limit")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("invalid object key")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate object key %q", key)
				}
				seen[key] = struct{}{}
				if err := readValue(depth + 1); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := readValue(depth + 1); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return errors.New("unexpected JSON delimiter")
		}
	}
	if err := readValue(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
