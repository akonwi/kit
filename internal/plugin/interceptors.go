package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ErrInterceptorUnavailable blocks calls whose admitted policy was replaced.
var ErrInterceptorUnavailable = errors.New("plugin interceptor policy is unavailable; tool execution blocked")

type interceptor struct{ owner InstanceID }

// InterceptionDecision permits execution or returns a model-visible rejection.
type InterceptionDecision struct {
	Reject  bool
	Message string
}

func (h *Host) replaceInterceptorPolicyLocked() {
	h.interceptorRevision++
	if h.cancelInterceptors != nil {
		h.cancelInterceptors()
	}
	h.interceptorContext, h.cancelInterceptors = context.WithCancel(h.ctx)
	h.signalStateLocked()
}

func (h *Host) interceptorIdentityLocked() string {
	if h.closed || h.ctx.Err() != nil {
		return h.id + ":closed"
	}
	if h.interceptorContext == nil {
		h.interceptorContext, h.cancelInterceptors = context.WithCancel(h.ctx)
	}
	retained := h.interceptors[:0]
	for _, item := range h.interceptors {
		if h.activeEntryLocked(item.owner) == nil {
			h.replaceInterceptorPolicyLocked()
			continue
		}
		retained = append(retained, item)
	}
	h.interceptors = retained
	return h.id + ":" + strconv.FormatUint(h.interceptorRevision, 10)
}

// InterceptorIdentity identifies the exact live ordered policy, even if empty.
// A new host lifetime never recovers approvals from a previous host.
func (h *Host) InterceptorIdentity() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.interceptorIdentityLocked()
}

func (h *Host) handleInterceptorRequest(ctx context.Context, owner InstanceID, method string, params json.RawMessage) (json.RawMessage, error) {
	if len(params) != 0 {
		return nil, rpcError(-32602, "Interceptor registration takes no params")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h.activeEntryLocked(owner) == nil {
		return nil, rpcError(-32002, "Plugin generation is unavailable")
	}
	h.interceptorIdentityLocked()
	index := -1
	for i, item := range h.interceptors {
		if item.owner == owner {
			index = i
			break
		}
	}
	if method == "kit/tool-calls/register-interceptor" {
		if index < 0 {
			if len(h.interceptors) >= 128 {
				return nil, rpcError(-32005, "Session interceptor limit exceeded")
			}
			h.interceptors = append(h.interceptors, interceptor{owner: owner})
			h.replaceInterceptorPolicyLocked()
		}
	} else if index >= 0 {
		h.interceptors = append(h.interceptors[:index], h.interceptors[index+1:]...)
		h.replaceInterceptorPolicyLocked()
	}
	return json.RawMessage("null"), nil
}

func parseInterceptionDecision(raw json.RawMessage) (InterceptionDecision, error) {
	fields, err := contributionObject(raw, "action", "message")
	if err != nil {
		return InterceptionDecision{}, err
	}
	action, err := contributionString(fields, "action", 32, true)
	if err != nil {
		return InterceptionDecision{}, err
	}
	switch action {
	case "allow":
		if _, exists := fields["message"]; exists {
			return InterceptionDecision{}, errors.New("allow response cannot contain a message")
		}
		return InterceptionDecision{}, nil
	case "reject-and-continue":
		message, err := interactionString(fields, "message", 8*1024)
		if err != nil || message == "" {
			return InterceptionDecision{}, errors.New("interceptor rejection requires a bounded message")
		}
		return InterceptionDecision{Reject: true, Message: message}, nil
	default:
		return InterceptionDecision{}, errors.New("invalid interceptor action")
	}
}

// InterceptTool calls the captured policy sequentially. It never holds host
// authority while awaiting plugin code or nested user interactions.
func (h *Host) InterceptTool(ctx context.Context, identity, callID, name string, input json.RawMessage) (InterceptionDecision, error) {
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil || identity != h.interceptorIdentityLocked() {
		h.mu.Unlock()
		return InterceptionDecision{}, ErrInterceptorUnavailable
	}
	chain := append([]interceptor(nil), h.interceptors...)
	policyContext := h.interceptorContext
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(policyContext, cancel)
	defer stop()
	defer cancel()
	if len(chain) > 0 && (len(input) > 1024*1024 || !json.Valid(input) || !contributionText(callID, 512, true) || !contributionText(name, 512, true)) {
		return InterceptionDecision{}, errors.New("invalid or oversized intercepted tool call")
	}
	params, err := json.Marshal(struct {
		ToolCall struct {
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"toolCall"`
	}{ToolCall: struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}{callID, name, input}})
	if err != nil {
		return InterceptionDecision{}, err
	}
	for _, item := range chain {
		decision, err := h.callInterceptor(ctx, identity, item, params)
		if err != nil {
			return InterceptionDecision{}, fmt.Errorf("plugin %s interception failed; tool execution blocked: %w", item.owner.PluginID, err)
		}
		if decision.Reject {
			return decision, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return InterceptionDecision{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || identity != h.interceptorIdentityLocked() {
		return InterceptionDecision{}, ErrInterceptorUnavailable
	}
	return InterceptionDecision{}, nil
}

func (h *Host) callInterceptor(ctx context.Context, identity string, item interceptor, params json.RawMessage) (InterceptionDecision, error) {
	for {
		if err := ctx.Err(); err != nil {
			return InterceptionDecision{}, err
		}
		h.mu.Lock()
		entry := h.activeEntryLocked(item.owner)
		if h.closed || identity != h.interceptorIdentityLocked() || entry == nil {
			h.mu.Unlock()
			return InterceptionDecision{}, ErrInterceptorUnavailable
		}
		instance := entry.instance
		changed := h.stateChanged
		synchronized := instance != nil && entry.context.Project.Cwd == h.view.cwd && sameSessionContext(entry.context.Session, h.view.session)
		h.mu.Unlock()
		if !synchronized {
			select {
			case <-ctx.Done():
				return InterceptionDecision{}, ctx.Err()
			case <-h.ctx.Done():
				return InterceptionDecision{}, ErrInterceptorUnavailable
			case <-changed:
				continue
			}
		}
		var decision InterceptionDecision
		_, err := instance.callAdmitted(ctx, "kit/tool-calls/before-execute", params, func(raw json.RawMessage) error {
			var err error
			decision, err = parseInterceptionDecision(raw)
			return err
		}, func(enqueue func() error) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			entry := h.activeEntryLocked(item.owner)
			if h.closed || identity != h.interceptorIdentityLocked() || entry == nil {
				return ErrInterceptorUnavailable
			}
			if entry.context.Project.Cwd != h.view.cwd || !sameSessionContext(entry.context.Session, h.view.session) {
				return errCommandContextPending
			}
			return enqueue()
		})
		if errors.Is(err, errCommandContextPending) {
			continue
		}
		if err != nil {
			return InterceptionDecision{}, err
		}
		if err := ctx.Err(); err != nil {
			return InterceptionDecision{}, err
		}
		h.mu.Lock()
		valid := !h.closed && identity == h.interceptorIdentityLocked()
		h.mu.Unlock()
		if !valid {
			return InterceptionDecision{}, ErrInterceptorUnavailable
		}
		return decision, nil
	}
}
