package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
)

// InstanceID identifies one process generation within one session. Replacement
// instances must receive a new generation and never inherit outstanding RPCs.
type InstanceID struct {
	HostID     string // Unique runtime-host lifetime, including across reload of a runtime.
	SessionID  string
	PluginID   string
	Generation uint64
}

// PluginContext is the protocol-owned initialization context, explicitly
// projected by the session host rather than sharing runtime or renderer objects.
type PluginContext struct {
	Project ProjectContext `json:"project"`
	Session SessionContext `json:"session"`
}

// ProjectContext carries absolute project paths and nullable Git metadata.
type ProjectContext struct {
	Cwd string      `json:"cwd"`
	Git *GitContext `json:"git"`
}

// GitContext is the public plugin projection of repository state.
type GitContext struct {
	PullRequest *PullRequestContext `json:"pullRequest"`
	Root        string              `json:"root"`
	Branch      *string             `json:"branch"`
	Dirty       bool                `json:"dirty"`
}

// PullRequestContext describes optional cached PR metadata in public Git context.
type PullRequestContext struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// SessionContext identifies the owning session; its display name is nullable.
type SessionContext struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
}

// InstanceState describes initialization, admission, and terminal lifecycle.
type InstanceState string

const (
	InstanceInitializing InstanceState = "initializing"
	InstanceReady        InstanceState = "ready"
	InstanceStopping     InstanceState = "stopping"
	InstanceStopped      InstanceState = "stopped"
	InstanceFailed       InstanceState = "failed"
)

// MaxFailureMessageBytes bounds the sanitized, retained diagnostic summary.
const MaxFailureMessageBytes = 4096

// InstanceFailure is retained after cleanup, including the bounded stderr tail.
// The session host owns publication/persistence and must explain partial effects.
type InstanceFailure struct {
	Phase   string
	Message string
	Stderr  string
}

// InstanceStatus is a copy of one generation's current lifecycle state.
type InstanceStatus struct {
	ID      InstanceID
	State   InstanceState
	Failure *InstanceFailure
}

type instanceDeadlines struct{ initialize, shutdown, cleanup time.Duration }

var defaultInstanceDeadlines = instanceDeadlines{10 * time.Second, 2 * time.Second, 2 * time.Second}

// Instance owns a single process, endpoint, and admission lifetime. Start is
// nonblocking with respect to initialization. The session host remains responsible
// for discovery order, contribution disposal, diagnostics, and explicit restart.
type Instance struct {
	revocationNotify func() // Nonblocking signal only; invoked under instance authority.
	id               InstanceID
	process          *Process
	rpc              *RPCEndpoint
	mu               sync.Mutex
	state            InstanceState
	failure          *InstanceFailure
	cleanupErr       error
	graceful         bool
	work             context.Context
	cancelWork       context.CancelFunc
	cancelInit       context.CancelFunc
	cancelProcess    context.CancelFunc
	revoked          chan struct{}
	ready            chan struct{}
	readyOnce        sync.Once
	done             chan struct{}
	initDone         chan struct{}
	initErr          error // Published by closing initDone.
	calls            sync.WaitGroup
	deadlines        instanceDeadlines
}

// StartInstance launches an isolated generation and initializes it in the
// background. A runtime host can wait sequentially on WaitReady in its own
// background startup worker without imposing a readiness barrier on turns.
func StartInstance(parent context.Context, id InstanceID, installation Installation, initial PluginContext, handlers RPCHandlers) (*Instance, error) {
	return startInstance(parent, id, installation, initial, handlers, defaultInstanceDeadlines)
}

func startInstance(parent context.Context, id InstanceID, installation Installation, initial PluginContext, handlers RPCHandlers, deadlines instanceDeadlines) (*Instance, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if !identifier.Valid(id.HostID, "pluginhost_") || id.Generation == 0 || id.PluginID != installation.Manifest.ID || id.SessionID == "" || initial.Session.ID != id.SessionID {
		return nil, errors.New("invalid plugin instance identity")
	}
	if err := initial.validate(); err != nil {
		return nil, err
	}
	params, err := json.Marshal(struct {
		Version int           `json:"protocolVersion"`
		Context PluginContext `json:"context"`
	}{1, initial})
	if err != nil {
		return nil, err
	}
	// Parent cancellation requests graceful shutdown, not immediate process death.
	// The supervisor owns these detached cleanup contexts until the child is reaped.
	lifetime := context.WithoutCancel(parent)
	processContext, cancelProcess := context.WithCancel(lifetime)
	process, err := StartProcess(processContext, installation)
	if err != nil {
		cancelProcess()
		return nil, err
	}
	work, cancelWork := context.WithCancel(lifetime)
	initialize, cancelInit := context.WithTimeout(lifetime, deadlines.initialize)
	instance := &Instance{id: id, process: process, state: InstanceInitializing, work: work, cancelWork: cancelWork, cancelInit: cancelInit, cancelProcess: cancelProcess, revoked: make(chan struct{}), ready: make(chan struct{}), done: make(chan struct{}), initDone: make(chan struct{}), deadlines: deadlines}
	wrapped := RPCHandlers{admit: instance.admit}
	wrapped.Request = func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		if work.Err() != nil {
			return nil, rpcError(-32002, "Plugin is unavailable")
		}
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(work, cancel)
		defer stop()
		defer cancel()
		if handlers.Request == nil {
			return nil, rpcError(-32601, "Method not found")
		}
		return handlers.Request(ctx, method, params)
	}
	wrapped.Notification = func(ctx context.Context, method string, params json.RawMessage) error {
		if work.Err() != nil {
			return nil
		}
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(work, cancel)
		defer stop()
		defer cancel()
		if handlers.Notification == nil {
			return nil
		}
		return handlers.Notification(ctx, method, params)
	}
	instance.rpc = NewRPCEndpoint(lifetime, process.Output(), process.Input(), wrapped)
	go func() {
		defer close(instance.initDone)
		defer cancelInit()
		_, instance.initErr = instance.rpc.callValidated(initialize, "initialize", params, instance.acceptInitialization)
	}()
	go instance.supervise(parent)
	return instance, nil
}

func (context PluginContext) validate() error {
	valid := func(value string) bool { return utf8.ValidString(value) && !strings.ContainsRune(value, 0) }
	if !filepath.IsAbs(context.Project.Cwd) || !valid(context.Project.Cwd) || context.Session.ID == "" || !valid(context.Session.ID) {
		return errors.New("invalid plugin session/project context")
	}
	if context.Session.Name != nil && !valid(*context.Session.Name) {
		return errors.New("invalid plugin session name")
	}
	if git := context.Project.Git; git != nil {
		if !filepath.IsAbs(git.Root) || !valid(git.Root) || (git.Branch != nil && !valid(*git.Branch)) {
			return errors.New("invalid plugin Git context")
		}
	}
	return nil
}

func (i *Instance) acceptInitialization(result json.RawMessage) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil || len(fields) != 1 || !numberIsOne(string(fields["protocolVersion"])) {
		return errors.New("plugin returned an invalid initialization result or unsupported protocol version")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != InstanceInitializing {
		return rpcError(-32002, "Plugin initialization was cancelled")
	}
	// This executes on the RPC reader, before a following request in the same
	// frame or read can register a contribution. No asynchronous ready-state race.
	i.state = InstanceReady
	i.readyOnce.Do(func() { close(i.ready) })
	return nil
}

func (i *Instance) admit(_ string, _ bool) error {
	i.mu.Lock()
	state := i.state
	i.mu.Unlock()
	if state == InstanceReady {
		return nil
	}
	if state == InstanceInitializing {
		i.beginStop("initialize", errors.New("plugin sent work before initialization succeeded"))
	}
	return rpcError(-32002, "Plugin is unavailable")
}

// Status returns a detached snapshot suitable for retaining diagnostic state.
func (i *Instance) Status() InstanceStatus {
	i.mu.Lock()
	defer i.mu.Unlock()
	status := InstanceStatus{ID: i.id, State: i.state}
	if i.failure != nil {
		failure := *i.failure
		status.Failure = &failure
	}
	return status
}

// Revoked closes as soon as this generation stops accepting work. The host must
// fence contributions by ID/generation and remove them at its ownership boundary.
func (i *Instance) Revoked() <-chan struct{} { return i.revoked }

// Done closes after bounded endpoint cleanup and process-group teardown attempts.
// Stop reports cleanup deadlines/failures; the process supervisor remains owned
// and cancelled even if a stuck OS process cannot be reaped within that deadline.
func (i *Instance) Done() <-chan struct{} { return i.done }

// WaitReady observes readiness without cancelling instance initialization when
// the waiting caller detaches or times out. Only its owning lifetime stops it.
func (i *Instance) WaitReady(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-i.ready:
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state == InstanceReady {
		return nil
	}
	if i.failure != nil {
		return errors.New(i.failure.Message)
	}
	return rpcError(-32002, "Plugin is unavailable")
}

// Call dispatches only to this ready generation. Revocation cancels admitted
// calls; a replacement must not replay them or reuse this instance's endpoint.
func (i *Instance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	return i.callValidated(ctx, method, params, nil)
}

func (i *Instance) callValidated(ctx context.Context, method string, params json.RawMessage, validate func(json.RawMessage) error) (json.RawMessage, error) {
	return i.callAdmitted(ctx, method, params, validate, func(enqueue func() error) error { return enqueue() })
}

func (i *Instance) callAdmitted(ctx context.Context, method string, params json.RawMessage, validate func(json.RawMessage) error, admit func(func() error) error) (json.RawMessage, error) {
	if method == "initialize" || method == "shutdown" {
		return nil, rpcError(-32600, "Lifecycle methods are host-owned")
	}
	i.mu.Lock()
	if i.state != InstanceReady {
		i.mu.Unlock()
		return nil, rpcError(-32002, "Plugin is unavailable")
	}
	i.calls.Add(1)
	i.mu.Unlock()
	defer i.calls.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(i.work, cancel)
	defer stop()
	defer cancel()
	return i.rpc.callAdmitted(ctx, method, params, validate, func(enqueue func() error) error {
		return admit(func() error {
			i.mu.Lock()
			defer i.mu.Unlock()
			if i.state != InstanceReady || i.work.Err() != nil {
				return rpcError(-32002, "Plugin is unavailable")
			}
			return enqueue()
		})
	})
}

// tryNotify atomically fences and queues an event for this ready generation.
func (i *Instance) tryNotify(method string, params json.RawMessage) error {
	if method == "initialize" || method == "shutdown" {
		return rpcError(-32600, "Lifecycle methods are host-owned")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != InstanceReady || i.work.Err() != nil {
		return rpcError(-32002, "Plugin is unavailable")
	}
	return i.rpc.tryNotify(method, params)
}

// Notify publishes a session event only to this ready generation. A caller
// cancellation cannot retract an already queued notification.
func (i *Instance) Notify(ctx context.Context, method string, params json.RawMessage) error {
	if method == "initialize" || method == "shutdown" {
		return rpcError(-32600, "Lifecycle methods are host-owned")
	}
	i.mu.Lock()
	if i.state != InstanceReady {
		i.mu.Unlock()
		return rpcError(-32002, "Plugin is unavailable")
	}
	i.calls.Add(1)
	i.mu.Unlock()
	defer i.calls.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(i.work, cancel)
	defer stop()
	defer cancel()
	return i.rpc.Notify(ctx, method, params)
}

// Revoke stops admission synchronously and schedules owned teardown without
// waiting. Hosts revoke every affected instance before joining any cleanup.
func (i *Instance) Revoke() { i.beginStop("", nil) }

// Stop revokes admission immediately, then allows bounded graceful shutdown.
// A caller deadline stops waiting, not cleanup. Expected exits are not crashes.
func (i *Instance) Stop(ctx context.Context) error {
	i.beginStop("", nil)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-i.done:
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.cleanupErr
}

func (i *Instance) beginStop(phase string, reason error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state == InstanceStopping || i.state == InstanceStopped || i.state == InstanceFailed {
		return
	}
	i.graceful = i.state == InstanceReady && reason == nil
	i.state = InstanceStopping
	if reason != nil {
		i.failure = &InstanceFailure{Phase: phase, Message: diagnosticMessage(reason)}
	}
	i.cancelWork()
	i.cancelInit()
	close(i.revoked)
	if i.revocationNotify != nil {
		i.revocationNotify()
	}
	i.readyOnce.Do(func() { close(i.ready) })
}

func (i *Instance) supervise(parent context.Context) {
	initialized := i.initDone
	for {
		select {
		case <-parent.Done():
			i.beginStop("", nil)
		case <-i.revoked:
			i.cleanup()
			return
		case <-i.process.Done():
			exitErr := i.process.Wait(context.Background())
			if exitErr == nil {
				exitErr = errors.New("plugin exited unexpectedly with status zero")
			} else {
				exitErr = fmt.Errorf("plugin exited unexpectedly: %w", exitErr)
			}
			i.beginStop(i.failurePhase(), exitErr)
		case <-i.rpc.closed:
			i.beginStop(i.failurePhase(), i.rpc.Err())
		case <-initialized:
			initialized = nil
			if i.initErr != nil {
				i.beginStop("initialize", i.initErr)
			}
		}
		select {
		case <-i.revoked:
			i.cleanup()
			return
		default:
		}
	}
}

func diagnosticMessage(err error) string {
	var result strings.Builder
	for _, r := range err.Error() {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			r = ' '
		}
		if result.Len()+utf8.RuneLen(r) > MaxFailureMessageBytes-3 {
			result.WriteString("...")
			break
		}
		result.WriteRune(r)
	}
	return result.String()
}

func (i *Instance) failurePhase() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state == InstanceInitializing {
		return "initialize"
	}
	return "runtime"
}

func (i *Instance) cleanup() {
	defer close(i.done)
	defer i.cancelProcess()
	<-i.initDone
	i.calls.Wait() // Admission was revoked before this wait; all call contexts cancelled.
	i.mu.Lock()
	graceful := i.graceful
	i.mu.Unlock()
	var shutdownErr error
	if graceful && i.rpc.Err() == nil {
		ctx, cancel := context.WithTimeout(context.Background(), i.deadlines.shutdown)
		_, err := i.rpc.callValidated(ctx, "shutdown", nil, func(result json.RawMessage) error {
			if string(result) != "null" {
				return errors.New("plugin shutdown result must be null")
			}
			return nil
		})
		if err == nil {
			select {
			case <-i.process.Done():
			case <-ctx.Done():
			}
		} else if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
			shutdownErr = err
		}
		cancel()
	}
	i.rpc.Close(rpcError(-32002, "Plugin is unavailable"))
	ctx, cancel := context.WithTimeout(context.Background(), i.deadlines.cleanup)
	defer cancel()
	processErr := i.process.Stop(ctx)
	// Wait returns the close reason even when joined successfully. Check completion
	// separately so an expected transport close isn't mistaken for cleanup failure.
	var endpointErr error
	select {
	case <-i.rpc.done:
	case <-ctx.Done():
		endpointErr = ctx.Err()
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.cleanupErr = errors.Join(shutdownErr, processErr, endpointErr)
	if i.failure == nil && i.cleanupErr != nil {
		i.failure = &InstanceFailure{Phase: "shutdown", Message: diagnosticMessage(i.cleanupErr)}
	}
	if i.failure != nil {
		i.failure.Stderr = i.process.Stderr()
		i.state = InstanceFailed
	} else {
		i.state = InstanceStopped
	}
}

func (i *Instance) setRevocationNotify(notify func()) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.revocationNotify = notify
	if i.state == InstanceStopping || i.state == InstanceStopped || i.state == InstanceFailed {
		notify()
	}
}
