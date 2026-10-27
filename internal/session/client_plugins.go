package session

import (
	"context"
	"encoding/json"
	"errors"
)

// Client plugin failures (ADR 0051).
var (
	// ErrClientPluginNotFound rejects an unknown or stopped client instance.
	ErrClientPluginNotFound = errors.New("client plugin instance not found")
	// ErrClientPluginCallConflict rejects a result for a call that is unknown,
	// already ended, or addressed to another instance.
	ErrClientPluginCallConflict = errors.New("client plugin call is not pending for this instance")
	// ErrClientPluginCapacity rejects an instance beyond the session's limit.
	ErrClientPluginCapacity = errors.New("session client plugin limit exceeded")
	// ErrClientPluginsUnavailable reports a session that cannot host client
	// plugins.
	ErrClientPluginsUnavailable = errors.New("client plugins are unavailable for this session")
)

// ClientPluginAttachment declares or replaces one client instance. Each tool
// is an object with the plugin tool registration fields.
type ClientPluginAttachment struct {
	InstanceID string
	PluginID   string
	Tools      []json.RawMessage
}

// ClientPluginBinding names one event stream bound to a client instance.
type ClientPluginBinding struct {
	InstanceID string
	Token      uint64
}

// ClientPluginCallResult is a client's answer to one call: Result holds the
// plugin tool result fields content and details, or Error is the client's
// failure message.
type ClientPluginCallResult struct {
	Result json.RawMessage
	Error  string
}

// PluginClientHost is a plugin host that also hosts client plugin instances.
// Invalid declarations and results fail with an error wrapping
// ErrInvalidInput whose message names the broken rule.
type PluginClientHost interface {
	AttachClientPlugin(ClientPluginAttachment) error
	DetachClientPlugin(instanceID string) error
	BindClientPluginStream(instanceID string) (ClientPluginBinding, error)
	ReleaseClientPluginStream(ClientPluginBinding)
	// PendingClientPluginCalls returns the instance's unsettled calls.
	PendingClientPluginCalls(instanceID string) []ClientPluginCall
	ClientPluginCallDeliverable(ClientPluginBinding, string) bool
	CompleteClientPluginCall(instanceID, callID string, result ClientPluginCallResult) error
	// SetClientPluginCallObserver installs the session-owned publication port
	// before Start. requested publishes a call and must not wait for a client;
	// resolved reports a published call's outcome and must not block.
	SetClientPluginCallObserver(requested func(context.Context, ClientPluginCall) error, resolved func(ClientPluginCall))
}

// ClientPluginStream is an event stream's binding to a client instance of
// one session runtime. It becomes inert when that runtime is replaced.
type ClientPluginStream struct {
	host    PluginClientHost
	binding ClientPluginBinding
}

// InstanceID names the bound client instance.
func (s *ClientPluginStream) InstanceID() string {
	if s == nil {
		return ""
	}
	return s.binding.InstanceID
}

// Deliverable reports whether this stream should carry callID's payload:
// it is still the instance's current stream and the call is pending.
func (s *ClientPluginStream) Deliverable(callID string) bool {
	return s != nil && s.host.ClientPluginCallDeliverable(s.binding, callID)
}

// Release records that the stream closed, starting the instance's grace
// period if no newer stream replaced it.
func (s *ClientPluginStream) Release() {
	if s != nil {
		s.host.ReleaseClientPluginStream(s.binding)
	}
}

func (m *Manager) clientPluginHost(ctx context.Context, sessionID string) (PluginClientHost, error) {
	_, host, err := m.clientPluginRuntime(ctx, sessionID)
	return host, err
}

func (m *Manager) clientPluginRuntime(ctx context.Context, sessionID string) (*runtime, PluginClientHost, error) {
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	host, ok := loaded.plugins.(PluginClientHost)
	if !ok || host == nil {
		return nil, nil, ErrClientPluginsUnavailable
	}
	return loaded, host, nil
}

// AttachClientPlugin declares or replaces a client instance of a session.
func (m *Manager) AttachClientPlugin(ctx context.Context, sessionID string, attachment ClientPluginAttachment) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	host, err := m.clientPluginHost(ctx, sessionID)
	if err != nil {
		return err
	}
	return host.AttachClientPlugin(attachment)
}

// DetachClientPlugin stops a client instance of a session.
func (m *Manager) DetachClientPlugin(ctx context.Context, sessionID, instanceID string) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	host, err := m.clientPluginHost(ctx, sessionID)
	if err != nil {
		return err
	}
	return host.DetachClientPlugin(instanceID)
}

// BindClientPluginStream binds an opening event stream to a client instance.
// It then republishes the instance's pending calls, so that a call published
// while the client was reconnecting, or lost when the event stream rotated,
// reaches the new stream; the client runs each call ID at most once. The
// caller releases the binding when the stream closes.
func (m *Manager) BindClientPluginStream(ctx context.Context, sessionID, instanceID string) (*ClientPluginStream, error) {
	if err := m.beginOperation(); err != nil {
		return nil, err
	}
	defer m.ops.Done()
	loaded, host, err := m.clientPluginRuntime(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	binding, err := host.BindClientPluginStream(instanceID)
	if err != nil {
		return nil, err
	}
	stream := &ClientPluginStream{host: host, binding: binding}
	for _, call := range host.PendingClientPluginCalls(instanceID) {
		if err := m.appendClientPluginCall(loaded, sessionID, EventClientPluginCallRequested, call); err != nil {
			stream.Release()
			return nil, err
		}
	}
	return stream, nil
}

// CompleteClientPluginCall settles a pending call with the client's answer.
func (m *Manager) CompleteClientPluginCall(ctx context.Context, sessionID, instanceID, callID string, result ClientPluginCallResult) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	host, err := m.clientPluginHost(ctx, sessionID)
	if err != nil {
		return err
	}
	return host.CompleteClientPluginCall(instanceID, callID, result)
}

// publishClientPluginCall appends a client_plugin.call.* event to loaded's
// live stream while loaded still owns the session.
func (m *Manager) publishClientPluginCall(ctx context.Context, loaded *runtime, sessionID string, kind EventKind, call ClientPluginCall) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.appendClientPluginCall(loaded, sessionID, kind, call)
}

func (m *Manager) appendClientPluginCall(loaded *runtime, sessionID string, kind EventKind, call ClientPluginCall) error {
	m.mu.Lock()
	owned := m.runtimes[sessionID] == loaded && !m.deleting[sessionID]
	m.mu.Unlock()
	if !owned {
		return ErrClosed
	}
	call.Input = append(json.RawMessage(nil), call.Input...)
	return loaded.events.append([]NewEvent{{SessionID: sessionID, Kind: kind, ClientPluginCall: &call}})
}
