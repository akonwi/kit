package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	submitMessageMethod = "kit/session/submit-message"
	// MaxMessageTextBytes bounds submitted message text, matching user prompts.
	MaxMessageTextBytes = 128 << 10
	// maxMessageParamsBytes bounds the encoded params, allowing for JSON escaping.
	maxMessageParamsBytes = 1 << 20
)

var messageIdempotencyKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// MessageRequest is one plugin-authored message for the plugin's owning session.
type MessageRequest struct {
	Owner          InstanceID
	Text           string
	IdempotencyKey string
}

// MessageResult identifies an admitted message and the turn it started.
type MessageResult struct {
	MessageID string
	TurnID    string
}

func parseMessage(raw json.RawMessage) (MessageRequest, error) {
	fields, err := boundedParamsObject(raw, maxMessageParamsBytes, "text", "idempotencyKey")
	if err != nil {
		return MessageRequest{}, err
	}
	var request MessageRequest
	value, present := fields["text"]
	if !present || bytes.Equal(value, []byte("null")) {
		return MessageRequest{}, rpcError(-32602, "Missing required parameter: text")
	}
	if json.Unmarshal(value, &request.Text) != nil || strings.TrimSpace(request.Text) == "" ||
		len(request.Text) > MaxMessageTextBytes || !utf8.ValidString(request.Text) || strings.IndexByte(request.Text, 0) >= 0 {
		return MessageRequest{}, rpcError(-32602, "Invalid parameter: text")
	}
	if value, present := fields["idempotencyKey"]; present && !bytes.Equal(value, []byte("null")) {
		if json.Unmarshal(value, &request.IdempotencyKey) != nil || !messageIdempotencyKey.MatchString(request.IdempotencyKey) {
			return MessageRequest{}, rpcError(-32602, "Invalid parameter: idempotencyKey")
		}
	}
	return request, nil
}

// handleSubmitMessage forwards a current generation's message to the session
// owner without holding host authority while the session admits it.
func (h *Host) handleSubmitMessage(ctx context.Context, owner InstanceID, params json.RawMessage) (json.RawMessage, error) {
	request, err := parseMessage(params)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	if ctx.Err() != nil {
		h.mu.Unlock()
		return nil, ctx.Err()
	}
	if h.activeEntryLocked(owner) == nil {
		h.mu.Unlock()
		return nil, rpcError(-32002, "Plugin generation is unavailable")
	}
	submit := h.config.SubmitMessage
	h.mu.Unlock()
	if submit == nil {
		return nil, rpcError(-32601, "Session message submission is unavailable")
	}
	request.Owner = owner
	result, err := submit(ctx, request)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		MessageID string `json:"messageId"`
		TurnID    string `json:"turnId"`
	}{result.MessageID, result.TurnID})
}
