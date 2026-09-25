package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// InteractionKind identifies a v1 plugin user request, independent of model tools.
type InteractionKind string

const (
	InteractionConfirm InteractionKind = "confirm"
	InteractionInput   InteractionKind = "input"
	InteractionSelect  InteractionKind = "select"
)

// InteractionOption retains the plugin's opaque JSON value without float coercion.
// The session adapter returns an index; only the host maps it back to this value.
type InteractionOption struct {
	Label, Description string
	Value              json.RawMessage
}

// InteractionRequest is the bounded host-owned request sent to a session adapter.
// Owner and ctx fence its instance lifetime; it has no fabricated model-run identity.
// User-instance requests survive cwd/name changes; project revocation cancels them.
type InteractionRequest struct {
	Owner                     InstanceID
	Kind                      InteractionKind
	Title, Message            string
	ConfirmLabel, CancelLabel string
	DefaultValue              *bool
	Placeholder, InitialValue string
	Filterable                bool
	Options                   []InteractionOption
}

// InteractionResponse contains user intent, never a client-supplied option value.
// Cancelled maps to false for confirm and null for input/select.
type InteractionResponse struct {
	Cancelled   bool
	Confirmed   bool
	Text        string
	OptionIndex int
}

// ErrInteractivityUnavailable distinguishes disabled/unwired interactive requests
// from a user cancellation. A temporarily detached session must not return it.
var ErrInteractivityUnavailable = errors.New("plugin user interactions are unavailable")

const (
	maxInteractionTitleBytes     = 512
	maxInteractionMessageBytes   = 8 * 1024
	maxInteractionTextBytes      = 16 * 1024
	maxInteractionOptions        = 64
	maxPendingPluginInteractions = 8
)

func interactionText(value string, limit int) bool {
	if len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func interactionString(fields map[string]json.RawMessage, name string, limit int) (string, error) {
	raw, present := fields[name]
	if !present || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || !interactionText(value, limit) {
		return "", rpcError(-32602, "Invalid parameter: "+name)
	}
	return value, nil
}

func interactionBool(fields map[string]json.RawMessage, name string) (bool, error) {
	raw, present := fields[name]
	if !present {
		return false, nil
	}
	var value bool
	if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil {
		return false, rpcError(-32602, "Invalid parameter: "+name)
	}
	return value, nil
}

func parseInteraction(method string, params json.RawMessage) (InteractionRequest, error) {
	request := InteractionRequest{Kind: InteractionKind(strings.TrimPrefix(method, "kit/ui/"))}
	allowed := []string{"title", "message"}
	switch request.Kind {
	case InteractionConfirm:
		allowed = append(allowed, "confirmLabel", "cancelLabel", "defaultValue")
	case InteractionInput:
		allowed = append(allowed, "placeholder", "initialValue")
	case InteractionSelect:
		allowed = append(allowed, "options", "filterable", "placeholder")
	default:
		return request, rpcError(-32601, "Unknown interaction method")
	}
	fields, err := contributionObject(params, allowed...)
	if err != nil {
		return request, err
	}
	if request.Title, err = contributionString(fields, "title", maxInteractionTitleBytes, true); err != nil {
		return request, err
	}
	if request.Message, err = interactionString(fields, "message", maxInteractionMessageBytes); err != nil {
		return request, err
	}
	switch request.Kind {
	case InteractionConfirm:
		if request.ConfirmLabel, err = contributionString(fields, "confirmLabel", 128, false); err != nil {
			return request, err
		}
		if request.CancelLabel, err = contributionString(fields, "cancelLabel", 128, false); err != nil {
			return request, err
		}
		var value bool
		value, err = interactionBool(fields, "defaultValue")
		if _, present := fields["defaultValue"]; present && err == nil {
			request.DefaultValue = &value
		}
	case InteractionInput:
		if request.Placeholder, err = contributionString(fields, "placeholder", maxInteractionTitleBytes, false); err != nil {
			return request, err
		}
		request.InitialValue, err = interactionString(fields, "initialValue", maxInteractionTextBytes)
	case InteractionSelect:
		if request.Placeholder, err = contributionString(fields, "placeholder", maxInteractionTitleBytes, false); err != nil {
			return request, err
		}
		if request.Filterable, err = interactionBool(fields, "filterable"); err != nil {
			return request, err
		}
		request.Options, err = parseInteractionOptions(fields["options"])
	}
	return request, err
}

func parseInteractionOptions(raw json.RawMessage) ([]InteractionOption, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, rpcError(-32602, "Options must be an array")
	}
	var options []InteractionOption
	for decoder.More() {
		if len(options) >= maxInteractionOptions {
			return nil, rpcError(-32005, "Too many interaction options")
		}
		var encoded json.RawMessage
		if err := decoder.Decode(&encoded); err != nil {
			return nil, rpcError(-32602, "Invalid interaction option")
		}
		fields, err := contributionObject(encoded, "label", "value", "description")
		if err != nil {
			return nil, err
		}
		var option InteractionOption
		if option.Label, err = contributionString(fields, "label", maxInteractionTitleBytes, true); err != nil {
			return nil, err
		}
		if option.Description, err = interactionString(fields, "description", maxInteractionMessageBytes); err != nil {
			return nil, err
		}
		value, present := fields["value"]
		if !present || !json.Valid(value) {
			return nil, rpcError(-32602, "Missing or invalid option value")
		}
		if len(value) > maxInteractionTextBytes {
			return nil, rpcError(-32005, "Option value exceeds protocol limits")
		}
		option.Value = append(json.RawMessage(nil), value...)
		options = append(options, option)
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim(']') || len(options) == 0 {
		return nil, rpcError(-32602, "Options must contain at least one choice")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, rpcError(-32602, "Invalid options array")
	}
	return options, nil
}

func cloneInteraction(request InteractionRequest) InteractionRequest {
	if request.DefaultValue != nil {
		value := *request.DefaultValue
		request.DefaultValue = &value
	}
	request.Options = append([]InteractionOption(nil), request.Options...)
	for index := range request.Options {
		request.Options[index].Value = append(json.RawMessage(nil), request.Options[index].Value...)
	}
	return request
}

func interactionResult(request InteractionRequest, response InteractionResponse) (json.RawMessage, error) {
	if response.Cancelled {
		if request.Kind == InteractionConfirm {
			return json.RawMessage("false"), nil
		}
		return json.RawMessage("null"), nil
	}
	switch request.Kind {
	case InteractionConfirm:
		return json.Marshal(response.Confirmed)
	case InteractionInput:
		if !interactionText(response.Text, maxInteractionTextBytes) {
			return nil, rpcError(-32603, "Invalid interaction text result")
		}
		return json.Marshal(response.Text)
	case InteractionSelect:
		if response.OptionIndex < 0 || response.OptionIndex >= len(request.Options) {
			return nil, rpcError(-32603, "Invalid interaction selection")
		}
		return json.Marshal(struct {
			Value json.RawMessage `json:"value"`
		}{request.Options[response.OptionIndex].Value})
	default:
		return nil, rpcError(-32603, "Invalid interaction kind")
	}
}

func (h *Host) handleInteraction(ctx context.Context, owner InstanceID, method string, params json.RawMessage) (json.RawMessage, error) {
	request, err := parseInteraction(method, params)
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
	observer := h.config.Interaction
	if observer == nil {
		h.mu.Unlock()
		return nil, interactivityUnavailableError()
	}
	if h.interactionsInFlight >= maxPendingPluginInteractions {
		h.mu.Unlock()
		return nil, rpcError(-32005, "Too many pending plugin interactions")
	}
	h.interactionsInFlight++
	h.mu.Unlock()
	defer func() { h.mu.Lock(); h.interactionsInFlight--; h.mu.Unlock() }()
	request.Owner = owner
	// Never hold host authority while a user or the session broker is awaited.
	// Supply a detached copy so the adapter cannot replace retained option values.
	response, err := observer(ctx, cloneInteraction(request))
	h.mu.Lock()
	active := h.activeEntryLocked(owner) != nil
	h.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !active {
		return nil, rpcError(-32002, "Plugin generation is unavailable")
	}
	if errors.Is(err, ErrInteractivityUnavailable) {
		return nil, interactivityUnavailableError()
	}
	if err != nil {
		return nil, err
	}
	return interactionResult(request, response)
}

func interactivityUnavailableError() *RPCError {
	return &RPCError{Code: -32000, Message: ErrInteractivityUnavailable.Error(), Data: json.RawMessage(`{"reason":"interactivity_unavailable"}`)}
}

// InteractionOwnerActive lets the session broker fence admission and answers
// under its own authority. It does not wait on process IO or invoke callbacks.
func (h *Host) InteractionOwnerActive(owner InstanceID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activeEntryLocked(owner) != nil
}
