package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/akonwi/kit/internal/identifier"
)

// Client-hosted plugin bounds (ADR 0051).
const (
	// MaxClientInstances bounds the client plugin instances of one session.
	MaxClientInstances = 8
	// MaxClientTools bounds the tools one client instance declares.
	MaxClientTools = 32
	// MaxClientPendingCalls bounds one client instance's unsettled calls.
	MaxClientPendingCalls = 16
	// DefaultClientGrace is how long an instance stays ready without a stream.
	DefaultClientGrace = 10 * time.Second
	// DefaultClientCallTimeout bounds one client tool call.
	DefaultClientCallTimeout = 2 * time.Minute
	// MaxClientFailureBytes bounds a client's error message for a call.
	MaxClientFailureBytes = 8 * 1024
)

var (
	// ErrClientInstanceNotFound rejects an unknown or stopped client instance.
	ErrClientInstanceNotFound = errors.New("client plugin instance not found")
	// ErrClientCallConflict rejects a result for a call that is unknown,
	// already ended, or addressed to another instance.
	ErrClientCallConflict = errors.New("client plugin call is not pending for this instance")
	// ErrClientCapacity rejects an instance beyond MaxClientInstances.
	ErrClientCapacity = errors.New("session client plugin limit exceeded")
	// ErrClientUnavailable rejects client plugin operations on a closed host.
	ErrClientUnavailable = errors.New("client plugins are unavailable")
)

// InvalidClientError rejects a malformed client declaration or result. Its
// message names the rule that was broken and is safe to return to the client.
type InvalidClientError struct{ Message string }

func (e *InvalidClientError) Error() string { return e.Message }

func invalidClient(format string, args ...any) error {
	return &InvalidClientError{Message: fmt.Sprintf(format, args...)}
}

// ClientAttachment declares or replaces one client instance. Each tool is an
// object with the kit/tools/register fields.
type ClientAttachment struct {
	InstanceID string
	PluginID   string
	Tools      []json.RawMessage
}

// ToolCall identifies one model tool call and the agent that made it. TurnID
// is set for the primary agent and SubagentConversationID for a subagent.
type ToolCall struct {
	ID                     string
	TurnID                 string
	SubagentConversationID string
}

// ClientCallRequest asks one client instance to run a tool.
type ClientCallRequest struct {
	CallID     string
	InstanceID string
	PluginID   string
	ToolID     string
	Call       ToolCall
	Input      json.RawMessage
}

// ClientCallOutcome is how a client tool call ended.
type ClientCallOutcome string

// Client call outcomes.
const (
	ClientCallCompleted   ClientCallOutcome = "completed"
	ClientCallFailed      ClientCallOutcome = "failed"
	ClientCallTimedOut    ClientCallOutcome = "timed_out"
	ClientCallWithdrawn   ClientCallOutcome = "withdrawn"
	ClientCallUnavailable ClientCallOutcome = "unavailable"
)

// ClientCallResolution reports how one published call ended.
type ClientCallResolution struct {
	CallID     string
	InstanceID string
	Outcome    ClientCallOutcome
}

// ClientStreamBinding names one event stream bound to a client instance.
// Only the most recent binding of an instance receives its calls.
type ClientStreamBinding struct {
	InstanceID string
	Token      uint64
}

type clientInstance struct {
	id          string
	owner       InstanceID
	tools       []Tool
	declaration [][]byte
	// binding is the token of the open stream, or 0 without one.
	binding uint64
	// opened orders instances by their most recent stream.
	opened uint64
	// ready is set once a stream binds and stays set through the grace period.
	ready   bool
	expiry  *time.Timer
	epoch   uint64
	pending map[string]*clientCall
}

type clientCall struct {
	id       string
	request  ClientCallRequest
	instance *clientInstance
	done     chan struct{}
	settled  bool
	outcome  ClientCallOutcome
	result   ToolResult
	failure  string
}

func (h *Host) clientGrace() time.Duration {
	if h.config.ClientGrace > 0 {
		return h.config.ClientGrace
	}
	return DefaultClientGrace
}

func (h *Host) clientCallTimeout() time.Duration {
	if h.config.ClientCallTimeout > 0 {
		return h.config.ClientCallTimeout
	}
	return DefaultClientCallTimeout
}

// AttachClient declares or replaces a client instance. An identical
// redeclaration keeps the instance's tool registrations, so a client that
// attaches again on every reconnect does not invalidate in-flight model
// requests. A declaration that changes the plugin ID stops the previous
// instance and its pending calls.
func (h *Host) AttachClient(attachment ClientAttachment) error {
	if !identifier.Valid(attachment.InstanceID, "clientplugin_") {
		return invalidClient("invalid client plugin instance id")
	}
	if !pluginID.MatchString(attachment.PluginID) {
		return invalidClient("invalid plugin id")
	}
	for _, reserved := range h.config.ReservedIDs {
		if attachment.PluginID == reserved {
			return invalidClient("plugin id %q is reserved", attachment.PluginID)
		}
	}
	if len(attachment.Tools) > MaxClientTools {
		return invalidClient("a client plugin declares at most %d tools", MaxClientTools)
	}
	tools := make([]Tool, 0, len(attachment.Tools))
	declaration := make([][]byte, 0, len(attachment.Tools))
	seen := make(map[string]struct{}, len(attachment.Tools))
	for index, raw := range attachment.Tools {
		tool, err := parseTool(raw)
		if err != nil {
			return invalidClient("tool %d: %s", index, clientErrorMessage(err))
		}
		if _, duplicate := seen[tool.LocalID]; duplicate {
			return invalidClient("tool %q is declared twice", tool.LocalID)
		}
		seen[tool.LocalID] = struct{}{}
		tool.ID = attachment.PluginID + "." + tool.LocalID
		tool.ModelName = attachment.PluginID + "__" + tool.LocalID
		if len(tool.ModelName) > 64 {
			return invalidClient("model-facing tool name %q exceeds 64 characters", tool.ModelName)
		}
		tool.ClientInstance = attachment.InstanceID
		tools = append(tools, tool)
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			return invalidClient("tool %d is not valid JSON", index)
		}
		declaration = append(declaration, compact.Bytes())
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].ID < tools[j].ID })

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil {
		return ErrClientUnavailable
	}
	instance := h.clients[attachment.InstanceID]
	if instance != nil && instance.owner.PluginID != attachment.PluginID {
		h.stopClientLocked(instance)
		instance = nil
	}
	if instance == nil {
		if len(h.clients) >= MaxClientInstances {
			return ErrClientCapacity
		}
		h.nextClient++
		instance = &clientInstance{
			id:      attachment.InstanceID,
			owner:   InstanceID{HostID: h.id, SessionID: h.view.session.ID, PluginID: attachment.PluginID, Generation: h.nextClient},
			pending: make(map[string]*clientCall),
		}
		h.clients[instance.id] = instance
		h.scheduleClientExpiryLocked(instance)
	} else if sameDeclaration(instance.declaration, declaration) {
		return nil
	}
	for index := range tools {
		h.nextTool++
		tools[index].Owner = instance.owner
		tools[index].Registration = h.nextTool
	}
	instance.tools = tools
	instance.declaration = declaration
	h.publishChange()
	return nil
}

func sameDeclaration(left, right [][]byte) bool {
	if left == nil || len(left) != len(right) {
		return false
	}
	for index := range left {
		if !bytes.Equal(left[index], right[index]) {
			return false
		}
	}
	return true
}

func clientErrorMessage(err error) string {
	var rpc *RPCError
	if errors.As(err, &rpc) {
		return rpc.Message
	}
	return err.Error()
}

// DetachClient stops a client instance and ends its pending calls.
func (h *Host) DetachClient(instanceID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	instance := h.clients[instanceID]
	if instance == nil {
		return ErrClientInstanceNotFound
	}
	h.stopClientLocked(instance)
	h.publishChange()
	return nil
}

// BindClientStream makes an open event stream the instance's stream. A later
// binding replaces it. The instance is ready while it is bound and for the
// grace period after its stream is released.
func (h *Host) BindClientStream(instanceID string) (ClientStreamBinding, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	instance := h.clients[instanceID]
	if instance == nil || h.closed {
		return ClientStreamBinding{}, ErrClientInstanceNotFound
	}
	h.nextClientBinding++
	instance.binding = h.nextClientBinding
	instance.opened = h.nextClientBinding
	instance.ready = true
	instance.epoch++
	if instance.expiry != nil {
		instance.expiry.Stop()
		instance.expiry = nil
	}
	h.publishChange()
	return ClientStreamBinding{InstanceID: instanceID, Token: instance.binding}, nil
}

// PendingClientCalls returns the instance's unsettled calls in no particular
// order. A newly bound stream republishes them, so a call is never lost to a
// reconnect or an event stream rotation.
func (h *Host) PendingClientCalls(instanceID string) []ClientCallRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	instance := h.clients[instanceID]
	if instance == nil {
		return nil
	}
	result := make([]ClientCallRequest, 0, len(instance.pending))
	for _, call := range instance.pending {
		request := call.request
		request.Input = append(json.RawMessage(nil), request.Input...)
		result = append(result, request)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CallID < result[j].CallID })
	return result
}

// ReleaseClientStream records that a bound stream closed. It starts the grace
// period only when binding is still the instance's current stream.
func (h *Host) ReleaseClientStream(binding ClientStreamBinding) {
	h.mu.Lock()
	defer h.mu.Unlock()
	instance := h.clients[binding.InstanceID]
	if instance == nil || instance.binding != binding.Token {
		return
	}
	instance.binding = 0
	h.scheduleClientExpiryLocked(instance)
}

// ClientCallDeliverable reports whether a stream should carry a call's
// payload: binding is the instance's current stream and the call is pending.
func (h *Host) ClientCallDeliverable(binding ClientStreamBinding, callID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	instance := h.clients[binding.InstanceID]
	return instance != nil && binding.Token != 0 && instance.binding == binding.Token && instance.pending[callID] != nil
}

// CompleteClientCall settles a pending call with a result or, when failure
// is not empty, with the client's error message. result holds the plugin tool
// result fields content and details; terminate is not allowed. A malformed
// result fails without settling the call.
func (h *Host) CompleteClientCall(instanceID, callID string, result json.RawMessage, failure string) error {
	var parsed ToolResult
	if failure == "" {
		var err error
		parsed, err = parseToolResult(result)
		if err != nil {
			return invalidClient("invalid tool result: %s", err.Error())
		}
		if parsed.Terminate {
			return invalidClient("client tool results cannot terminate the turn")
		}
	} else if !interactionText(failure, MaxClientFailureBytes) {
		return invalidClient("invalid tool error message")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	instance := h.clients[instanceID]
	if instance == nil {
		return ErrClientCallConflict
	}
	call := instance.pending[callID]
	if call == nil {
		return ErrClientCallConflict
	}
	if failure != "" {
		h.settleClientCallLocked(call, ClientCallFailed, ToolResult{}, failure)
	} else {
		h.settleClientCallLocked(call, ClientCallCompleted, parsed, "")
	}
	return nil
}

// clientToolsLocked returns the tools of each plugin ID's current client
// instance, omitting plugin IDs that a process plugin claims.
func (h *Host) clientToolsLocked() []Tool {
	var result []Tool
	for _, instance := range h.currentClientsLocked() {
		for _, tool := range instance.tools {
			tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
			tool.PromptGuidelines = append([]string(nil), tool.PromptGuidelines...)
			tool.validator = nil
			result = append(result, tool)
		}
	}
	return result
}

// currentClientsLocked returns, for each plugin ID, the ready instance whose
// stream opened most recently, unless a process plugin claims the ID.
func (h *Host) currentClientsLocked() map[string]*clientInstance {
	if h.closed {
		return nil
	}
	current := make(map[string]*clientInstance)
	for _, instance := range h.clients {
		if !instance.ready {
			continue
		}
		if best := current[instance.owner.PluginID]; best == nil || instance.opened > best.opened {
			current[instance.owner.PluginID] = instance
		}
	}
	for pluginID := range current {
		if h.processPluginLocked(pluginID) {
			delete(current, pluginID)
		}
	}
	return current
}

// processPluginLocked reports whether a process plugin of the current view
// claims pluginID.
func (h *Host) processPluginLocked(pluginID string) bool {
	for _, entry := range h.entries {
		if entry.owner.PluginID == pluginID && entry.failure == nil && entry.epoch == h.view.epoch {
			return true
		}
	}
	return false
}

// clientConflictsLocked names the plugin IDs whose client tools a process
// plugin hides.
func (h *Host) clientConflictsLocked() []string {
	seen := make(map[string]struct{})
	var result []string
	for _, instance := range h.clients {
		id := instance.owner.PluginID
		if _, ok := seen[id]; ok || !instance.ready {
			continue
		}
		seen[id] = struct{}{}
		if h.processPluginLocked(id) {
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result
}

func (h *Host) executeClientTool(ctx context.Context, selection Tool, call ToolCall, value any, input json.RawMessage) (ToolResult, error) {
	h.mu.Lock()
	instance := h.currentClientsLocked()[selection.Owner.PluginID]
	var tool *Tool
	if instance != nil && instance.id == selection.ClientInstance && instance.owner == selection.Owner {
		for index := range instance.tools {
			if candidate := &instance.tools[index]; candidate.ID == selection.ID && candidate.Registration == selection.Registration {
				tool = candidate
				break
			}
		}
	}
	if tool == nil {
		h.mu.Unlock()
		return ToolResult{}, ErrToolUnavailable
	}
	if err := tool.validator.Validate(value); err != nil {
		h.mu.Unlock()
		return ToolResult{}, rpcError(-32602, "Tool input does not match schema")
	}
	if len(instance.pending) >= MaxClientPendingCalls {
		h.mu.Unlock()
		return ToolResult{}, rpcError(-32005, "Client plugin call limit exceeded")
	}
	callID, err := identifier.New("clientcall_")
	if err != nil {
		h.mu.Unlock()
		return ToolResult{}, err
	}
	request := ClientCallRequest{CallID: callID, InstanceID: instance.id, PluginID: instance.owner.PluginID, ToolID: tool.LocalID, Call: call, Input: append(json.RawMessage(nil), input...)}
	pending := &clientCall{id: callID, request: request, instance: instance, done: make(chan struct{})}
	instance.pending[callID] = pending
	publish, resolved := h.config.ClientCallRequested, h.config.ClientCallResolved
	timeout := h.clientCallTimeout()
	h.mu.Unlock()

	if publish == nil {
		err = ErrToolUnavailable
	} else {
		err = publish(ctx, request)
	}
	if err != nil {
		// Nothing reached a client, so no resolution is reported.
		h.mu.Lock()
		h.settleClientCallLocked(pending, ClientCallUnavailable, ToolResult{}, "")
		h.mu.Unlock()
		return ToolResult{}, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-pending.done:
	case <-ctx.Done():
		h.mu.Lock()
		h.settleClientCallLocked(pending, ClientCallWithdrawn, ToolResult{}, "")
		h.mu.Unlock()
	case <-timer.C:
		h.mu.Lock()
		h.settleClientCallLocked(pending, ClientCallTimedOut, ToolResult{}, "")
		h.mu.Unlock()
	}
	<-pending.done
	if resolved != nil {
		resolved(ClientCallResolution{CallID: callID, InstanceID: request.InstanceID, Outcome: pending.outcome})
	}
	switch pending.outcome {
	case ClientCallCompleted:
		return pending.result, nil
	case ClientCallFailed:
		return ToolResult{}, fmt.Errorf("%s failed: %s", selection.ModelName, pending.failure)
	case ClientCallTimedOut:
		return ToolResult{}, fmt.Errorf("%s timed out after %s; effects may have partially completed", selection.ModelName, timeout)
	case ClientCallWithdrawn:
		if err := ctx.Err(); err != nil {
			return ToolResult{}, err
		}
		return ToolResult{}, context.Canceled
	default:
		return ToolResult{}, fmt.Errorf("%s is unavailable: the client hosting plugin %q is no longer attached; effects may have partially completed", selection.ModelName, request.PluginID)
	}
}

// settleClientCallLocked ends a call once. Later settlements are ignored.
func (h *Host) settleClientCallLocked(call *clientCall, outcome ClientCallOutcome, result ToolResult, failure string) {
	if call.settled {
		return
	}
	call.settled = true
	call.outcome = outcome
	call.result = result
	call.failure = failure
	delete(call.instance.pending, call.id)
	close(call.done)
}

// scheduleClientExpiryLocked stops an instance after the grace period unless
// a stream binds it first.
func (h *Host) scheduleClientExpiryLocked(instance *clientInstance) {
	if instance.expiry != nil {
		instance.expiry.Stop()
	}
	instance.epoch++
	epoch := instance.epoch
	instance.expiry = time.AfterFunc(h.clientGrace(), func() { h.expireClient(instance, epoch) })
}

func (h *Host) expireClient(instance *clientInstance, epoch uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[instance.id] != instance || instance.binding != 0 || instance.epoch != epoch {
		return
	}
	h.stopClientLocked(instance)
	h.publishChange()
}

// stopClientLocked removes an instance and ends its pending calls as
// unavailable.
func (h *Host) stopClientLocked(instance *clientInstance) {
	if h.clients[instance.id] == instance {
		delete(h.clients, instance.id)
	}
	if instance.expiry != nil {
		instance.expiry.Stop()
		instance.expiry = nil
	}
	instance.epoch++
	instance.binding = 0
	instance.ready = false
	for _, call := range instance.pending {
		h.settleClientCallLocked(call, ClientCallUnavailable, ToolResult{}, "")
	}
}

func (h *Host) stopClientsLocked() {
	for _, instance := range h.clients {
		h.stopClientLocked(instance)
	}
}
