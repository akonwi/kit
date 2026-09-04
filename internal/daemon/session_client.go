package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/version"
)

const maxSessionResponseBytes = 8 << 20

// APIError is a non-success response from the local session protocol.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("daemon returned HTTP %d: %s", e.StatusCode, e.Message)
}

// CreateSession creates a persisted session through the local daemon.
func (c *Client) CreateSession(ctx context.Context, input protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	var output protocol.SessionInfo
	if err := c.sessionJSON(ctx, http.MethodPost, "/v1/sessions", input, http.StatusCreated, &output); err != nil {
		return protocol.SessionInfo{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionInfo{}, fmt.Errorf("validate daemon session response: %w", err)
	}
	return output, nil
}

// ListSessions lists daemon sessions, optionally filtered to one cwd.
func (c *Client) ListSessions(ctx context.Context, cwd string) ([]protocol.SessionInfo, error) {
	path := "/v1/sessions"
	if cwd != "" {
		values := url.Values{}
		values.Set("cwd", cwd)
		path += "?" + values.Encode()
	}
	var output struct {
		Sessions []protocol.SessionInfo `json:"sessions"`
	}
	if err := c.sessionJSON(ctx, http.MethodGet, path, nil, http.StatusOK, &output); err != nil {
		return nil, err
	}
	for index, session := range output.Sessions {
		if err := session.Validate(); err != nil {
			return nil, fmt.Errorf("validate daemon session %d: %w", index, err)
		}
	}
	return output.Sessions, nil
}

// GetSessionSnapshot returns an authoritative transcript and active-run snapshot.
func (c *Client) GetSessionSnapshot(ctx context.Context, sessionID string) (protocol.SessionSnapshot, error) {
	path := "/v1/sessions/" + url.PathEscape(sessionID)
	var output protocol.SessionSnapshot
	if err := c.sessionJSON(ctx, http.MethodGet, path, nil, http.StatusOK, &output); err != nil {
		return protocol.SessionSnapshot{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionSnapshot{}, fmt.Errorf("validate daemon session snapshot: %w", err)
	}
	if output.Session.ID != sessionID {
		return protocol.SessionSnapshot{}, fmt.Errorf("daemon session snapshot identity mismatch")
	}
	return output, nil
}

// GetSessionEvents returns the next ordered page after a session stream sequence.
func (c *Client) GetSessionEvents(ctx context.Context, sessionID string, after int64) (protocol.SessionEventBatch, error) {
	values := url.Values{}
	values.Set("after", strconv.FormatInt(after, 10))
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/events?" + values.Encode()
	var output protocol.SessionEventBatch
	if err := c.sessionJSON(ctx, http.MethodGet, path, nil, http.StatusOK, &output); err != nil {
		return protocol.SessionEventBatch{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionEventBatch{}, fmt.Errorf("validate daemon session events: %w", err)
	}
	if !output.ResyncRequired && after > 0 && len(output.Events) > 0 && output.Events[0].Sequence != after+1 {
		return protocol.SessionEventBatch{}, fmt.Errorf("daemon session event sequence gap after %d", after)
	}
	for _, event := range output.Events {
		if event.SessionID != sessionID {
			return protocol.SessionEventBatch{}, fmt.Errorf("daemon session event identity mismatch")
		}
	}
	return output, nil
}

// ReserveRun durably reserves a generation before prompt execution starts.
func (c *Client) ReserveRun(
	ctx context.Context,
	sessionID, runID string,
) (protocol.RunReservation, error) {
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/runs"
	var output protocol.RunReservation
	if err := c.sessionJSON(
		ctx,
		http.MethodPost,
		path,
		protocol.ReserveRunInput{RunID: runID},
		http.StatusCreated,
		&output,
	); err != nil {
		return protocol.RunReservation{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.RunReservation{}, fmt.Errorf("validate daemon run reservation: %w", err)
	}
	if output.SessionID != sessionID || output.RunID != runID {
		return protocol.RunReservation{}, fmt.Errorf("daemon run reservation identity mismatch")
	}
	return output, nil
}

// StartPrompt atomically admits a daemon-owned parent run and returns after it
// is running or has already reached a durable terminal state.
func (c *Client) StartPrompt(ctx context.Context, sessionID, runID, text string) (protocol.RunReservation, error) {
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/prompts"
	var output protocol.RunReservation
	if err := c.sessionJSON(ctx, http.MethodPost, path, protocol.PromptInput{RunID: runID, Text: text}, http.StatusAccepted, &output); err != nil {
		return protocol.RunReservation{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.RunReservation{}, fmt.Errorf("validate daemon prompt reservation: %w", err)
	}
	if output.SessionID != sessionID || output.RunID != runID {
		return protocol.RunReservation{}, fmt.Errorf("daemon prompt reservation identity mismatch")
	}
	return output, nil
}

// GetRun returns one durable parent-run generation.
func (c *Client) GetRun(ctx context.Context, sessionID, runID string) (protocol.RunInfo, error) {
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/runs/" + url.PathEscape(runID)
	var output protocol.RunInfo
	if err := c.sessionJSON(ctx, http.MethodGet, path, nil, http.StatusOK, &output); err != nil {
		return protocol.RunInfo{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.RunInfo{}, fmt.Errorf("validate daemon run response: %w", err)
	}
	if output.SessionID != sessionID || output.RunID != runID {
		return protocol.RunInfo{}, fmt.Errorf("daemon run response identity mismatch")
	}
	return output, nil
}

// RunPrompt executes an already reserved parent run. PromptOutcome carries
// durable model failures and aborts; returned Go errors represent protocol failures.
func (c *Client) RunPrompt(ctx context.Context, sessionID, runID, text string) (protocol.PromptOutcome, error) {
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/prompt"
	var output protocol.PromptOutcome
	if err := c.sessionJSON(ctx, http.MethodPost, path, protocol.PromptInput{RunID: runID, Text: text}, http.StatusOK, &output); err != nil {
		return protocol.PromptOutcome{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.PromptOutcome{}, fmt.Errorf("validate daemon prompt response: %w", err)
	}
	if output.SessionID != sessionID || output.RunID != runID {
		return protocol.PromptOutcome{}, fmt.Errorf("daemon prompt response identity mismatch")
	}
	return output, nil
}

// AbortSession requests cancellation of a loaded session's active run.
func (c *Client) AbortSession(ctx context.Context, sessionID, runID string) error {
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/runs/" + url.PathEscape(runID) + "/abort"
	return c.sessionJSON(ctx, http.MethodPost, path, nil, http.StatusAccepted, nil)
}

func (c *Client) sessionJSON(
	ctx context.Context,
	method string,
	path string,
	input any,
	expectedStatus int,
	output any,
) error {
	registry, err := LoadRegistry(c.paths)
	if err != nil {
		return err
	}
	if err := compatible(registry); err != nil {
		return err
	}
	token, err := loadToken(c.paths)
	if err != nil {
		return err
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode daemon request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, registry.URL+path, body)
	if err != nil {
		return fmt.Errorf("create daemon session request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(instanceHeader, registry.InstanceID)
	request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.sessionHTTP.Do(request)
	if err != nil {
		return fmt.Errorf("contact daemon session API: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxSessionResponseBytes)
	if response.StatusCode != expectedStatus {
		body, _ := io.ReadAll(limited)
		var envelope struct {
			Error string `json:"error"`
		}
		message := strings.TrimSpace(string(body))
		if json.Unmarshal(body, &envelope) == nil && envelope.Error != "" {
			message = envelope.Error
		}
		return &APIError{StatusCode: response.StatusCode, Message: message}
	}
	if output == nil {
		_, _ = io.Copy(io.Discard, limited)
		return nil
	}
	if err := json.NewDecoder(limited).Decode(output); err != nil {
		return fmt.Errorf("decode daemon session response: %w", err)
	}
	return nil
}
