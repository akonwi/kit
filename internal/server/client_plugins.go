package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/akonwi/kit/internal/plugin"
	"github.com/akonwi/kit/internal/session"
)

// The session plugin host also hosts client plugin instances (ADR 0051).
var _ session.PluginClientHost = (*sessionPluginHost)(nil)

func (h *sessionPluginHost) SetClientPluginCallObserver(requested func(context.Context, session.ClientPluginCall) error, resolved func(session.ClientPluginCall)) {
	h.clientCallMu.Lock()
	defer h.clientCallMu.Unlock()
	h.clientRequested = requested
	h.clientResolved = resolved
}

func (h *sessionPluginHost) requestClientCall(ctx context.Context, request plugin.ClientCallRequest) error {
	h.clientCallMu.RLock()
	observer := h.clientRequested
	h.clientCallMu.RUnlock()
	if observer == nil {
		return plugin.ErrToolUnavailable
	}
	return observer(ctx, clientPluginCall(request))
}

func (h *sessionPluginHost) resolveClientCall(resolution plugin.ClientCallResolution) {
	h.clientCallMu.RLock()
	observer := h.clientResolved
	h.clientCallMu.RUnlock()
	if observer != nil {
		observer(session.ClientPluginCall{ID: resolution.CallID, InstanceID: resolution.InstanceID, Outcome: string(resolution.Outcome)})
	}
}

func (h *sessionPluginHost) AttachClientPlugin(attachment session.ClientPluginAttachment) error {
	return clientPluginError(h.Host.AttachClient(plugin.ClientAttachment{InstanceID: attachment.InstanceID, PluginID: attachment.PluginID, Tools: attachment.Tools}))
}

func (h *sessionPluginHost) DetachClientPlugin(instanceID string) error {
	return clientPluginError(h.Host.DetachClient(instanceID))
}

func (h *sessionPluginHost) BindClientPluginStream(instanceID string) (session.ClientPluginBinding, error) {
	binding, err := h.Host.BindClientStream(instanceID)
	if err != nil {
		return session.ClientPluginBinding{}, clientPluginError(err)
	}
	return session.ClientPluginBinding{InstanceID: binding.InstanceID, Token: binding.Token}, nil
}

func (h *sessionPluginHost) PendingClientPluginCalls(instanceID string) []session.ClientPluginCall {
	pending := h.Host.PendingClientCalls(instanceID)
	result := make([]session.ClientPluginCall, 0, len(pending))
	for _, request := range pending {
		result = append(result, clientPluginCall(request))
	}
	return result
}

func clientPluginCall(request plugin.ClientCallRequest) session.ClientPluginCall {
	return session.ClientPluginCall{
		ID: request.CallID, InstanceID: request.InstanceID, PluginID: request.PluginID, ToolID: request.ToolID,
		ToolCallID: request.Call.ID, TurnID: request.Call.TurnID, SubagentConversationID: request.Call.SubagentConversationID,
		Input: request.Input,
	}
}

func (h *sessionPluginHost) ReleaseClientPluginStream(binding session.ClientPluginBinding) {
	h.Host.ReleaseClientStream(plugin.ClientStreamBinding{InstanceID: binding.InstanceID, Token: binding.Token})
}

func (h *sessionPluginHost) ClientPluginCallDeliverable(binding session.ClientPluginBinding, callID string) bool {
	return h.Host.ClientCallDeliverable(plugin.ClientStreamBinding{InstanceID: binding.InstanceID, Token: binding.Token}, callID)
}

func (h *sessionPluginHost) CompleteClientPluginCall(instanceID, callID string, result session.ClientPluginCallResult) error {
	return clientPluginError(h.Host.CompleteClientCall(instanceID, callID, result.Result, result.Error))
}

// clientPluginError projects plugin host failures onto session errors.
func clientPluginError(err error) error {
	var invalid *plugin.InvalidClientError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &invalid):
		return fmt.Errorf("%w: %s", session.ErrInvalidInput, invalid.Message)
	case errors.Is(err, plugin.ErrClientInstanceNotFound):
		return session.ErrClientPluginNotFound
	case errors.Is(err, plugin.ErrClientCallConflict):
		return session.ErrClientPluginCallConflict
	case errors.Is(err, plugin.ErrClientCapacity):
		return session.ErrClientPluginCapacity
	case errors.Is(err, plugin.ErrClientUnavailable):
		return session.ErrClientPluginsUnavailable
	}
	return err
}
