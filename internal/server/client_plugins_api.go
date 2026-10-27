package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/session"
)

// maxClientPluginResultRequestBytes bounds a call answer: 4 MiB of decoded
// content is about 5.4 MiB of base64 plus JSON framing.
const maxClientPluginResultRequestBytes = 6 << 20

// clientPluginStream is an event stream's binding to a client instance.
type clientPluginStream interface {
	InstanceID() string
	Deliverable(callID string) bool
	Release()
}

func (s runtimeSessionService) AttachClientPlugin(ctx context.Context, sessionID, instanceID string, input protocol.AttachClientPluginInput) error {
	tools := make([]json.RawMessage, 0, len(input.Tools))
	for _, tool := range input.Tools {
		raw, err := encodeClientPluginTool(tool)
		if err != nil {
			return err
		}
		tools = append(tools, raw)
	}
	return s.manager.AttachClientPlugin(ctx, sessionID, session.ClientPluginAttachment{InstanceID: instanceID, PluginID: input.PluginID, Tools: tools})
}

// encodeClientPluginTool writes a declaration as the plugin tool registration
// object the plugin host parses, omitting absent optional fields.
func encodeClientPluginTool(tool protocol.ClientPluginToolDeclaration) (json.RawMessage, error) {
	fields := map[string]any{"id": tool.ID, "description": tool.Description, "inputSchema": tool.InputSchema}
	if tool.Label != "" {
		fields["label"] = tool.Label
	}
	if tool.ExecutionMode != "" {
		fields["executionMode"] = tool.ExecutionMode
	}
	if tool.PromptSnippet != "" {
		fields["promptSnippet"] = tool.PromptSnippet
	}
	if tool.PromptGuidelines != nil {
		fields["promptGuidelines"] = tool.PromptGuidelines
	}
	return json.Marshal(fields)
}

func (s runtimeSessionService) DetachClientPlugin(ctx context.Context, sessionID, instanceID string) error {
	return s.manager.DetachClientPlugin(ctx, sessionID, instanceID)
}

func (s runtimeSessionService) CompleteClientPluginCall(ctx context.Context, sessionID, instanceID, callID string, input protocol.CompleteClientPluginCallInput) error {
	result := session.ClientPluginCallResult{Error: input.Error}
	if input.Error == "" {
		content := make([]map[string]string, 0, len(input.Content))
		for _, part := range input.Content {
			if part.Type == "text" {
				content = append(content, map[string]string{"type": "text", "text": part.Text})
			} else {
				content = append(content, map[string]string{"type": "image", "data": part.Data, "mimeType": part.MIMEType})
			}
		}
		fields := map[string]any{"content": content}
		if len(input.Details) > 0 {
			fields["details"] = input.Details
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		result.Result = raw
	}
	return s.manager.CompleteClientPluginCall(ctx, sessionID, instanceID, callID, result)
}

func (s runtimeSessionService) BindClientPluginStream(ctx context.Context, sessionID, instanceID string) (clientPluginStream, error) {
	stream, err := s.manager.BindClientPluginStream(ctx, sessionID, instanceID)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func registerClientPluginRoutes(mux *http.ServeMux, options httpapi.ServeOptions, service sessionService) {
	httpapi.Handle(mux, options, httpapi.AttachClientPlugin, func(ctx context.Context, params httpapi.ClientPluginPath, input protocol.AttachClientPluginInput) (httpapi.NoBody, error) {
		if !protocol.ValidClientPluginInstanceID(params.InstanceID) {
			return httpapi.NoBody{}, clientPluginInvalid("client plugin instance id must be clientplugin_ and 32 lowercase hex digits")
		}
		if err := input.Validate(); err != nil {
			return httpapi.NoBody{}, clientPluginInvalid(err.Error())
		}
		return httpapi.NoBody{}, clientPluginAPIError(service.AttachClientPlugin(ctx, params.SessionID, params.InstanceID, input))
	})
	httpapi.Handle(mux, options, httpapi.DetachClientPlugin, func(ctx context.Context, params httpapi.ClientPluginPath, _ httpapi.NoBody) (httpapi.NoBody, error) {
		if !protocol.ValidClientPluginInstanceID(params.InstanceID) {
			return httpapi.NoBody{}, clientPluginInvalid("client plugin instance id must be clientplugin_ and 32 lowercase hex digits")
		}
		return httpapi.NoBody{}, clientPluginAPIError(service.DetachClientPlugin(ctx, params.SessionID, params.InstanceID))
	})
	resultOptions := options
	resultOptions.MaxRequestBytes = maxClientPluginResultRequestBytes
	httpapi.Handle(mux, resultOptions, httpapi.CompleteClientPluginCall, func(ctx context.Context, params httpapi.ClientPluginCallPath, input protocol.CompleteClientPluginCallInput) (httpapi.NoBody, error) {
		if !protocol.ValidClientPluginInstanceID(params.InstanceID) || !protocol.ValidClientPluginCallID(params.CallID) {
			return httpapi.NoBody{}, clientPluginInvalid("invalid client plugin instance or call id")
		}
		if err := input.Validate(); err != nil {
			return httpapi.NoBody{}, clientPluginInvalid(err.Error())
		}
		return httpapi.NoBody{}, clientPluginAPIError(service.CompleteClientPluginCall(ctx, params.SessionID, params.InstanceID, params.CallID, input))
	})
}

func clientPluginInvalid(message string) error {
	if !protocol.ValidErrorMessage(message) {
		message = "invalid request"
	}
	return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, message, nil)
}

// clientPluginAPIError maps client plugin failures to declared ADR 0034
// errors.
func clientPluginAPIError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, session.ErrClientPluginNotFound):
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, session.ErrClientPluginNotFound.Error(), nil)
	case errors.Is(err, session.ErrClientPluginCallConflict):
		return httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorConflict, session.ErrClientPluginCallConflict.Error(), nil)
	case errors.Is(err, session.ErrClientPluginCapacity):
		return httpapi.NewAPIError(http.StatusTooManyRequests, httpapi.ErrorCapacityExceeded, session.ErrClientPluginCapacity.Error(), nil)
	case errors.Is(err, session.ErrClientPluginsUnavailable):
		return httpapi.NewAPIError(http.StatusServiceUnavailable, httpapi.ErrorUnavailable, session.ErrClientPluginsUnavailable.Error(), nil)
	case errors.Is(err, session.ErrInvalidInput):
		return clientPluginInvalid(strings.TrimPrefix(err.Error(), session.ErrInvalidInput.Error()+": "))
	}
	return pluginAPIError(err)
}

// withholdClientPluginCalls removes the call from every
// client_plugin.call.requested event that stream may not deliver: a call
// reaches only the stream bound to its instance, and only while it is
// pending. A nil stream, as for event pages, withholds every call.
func withholdClientPluginCalls(batch *protocol.SessionEventBatch, stream clientPluginStream) {
	for index, event := range batch.Events {
		request, ok := event.Payload.(protocol.ClientPluginCallRequestedEvent)
		if !ok || request.ClientPluginCall == nil {
			continue
		}
		if stream != nil && stream.InstanceID() == request.ClientPluginInstanceID && stream.Deliverable(request.ClientPluginCallID) {
			continue
		}
		request.ClientPluginCall = nil
		batch.Events[index].Payload = request
	}
}
