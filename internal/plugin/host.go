package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/identifier"
)

// FailureEvent is one plugin generation's final failed state. Message and
// Stderr are bounded by the instance supervisor before publication.
type FailureEvent struct {
	Owner   InstanceID
	Phase   string
	Message string
	Stderr  string
}

// HostConfig composes one session's plugin processes. Construction does not
// launch processes; Start must follow successful runtime publication.
type HostConfig struct {
	Home              apphome.Paths
	Session           SessionContext
	CWD               string
	ReservedIDs       []string
	ReservedSubagents []string
	// Project must honor cancellation and permit concurrent initialization and
	// live Git probes. Returned metadata must not be mutated after return.
	Project func(context.Context, string) (ProjectContext, error)
	// ProjectChanged optionally signals shared-observer invalidations.
	ProjectChanged <-chan struct{}
	Changed        func()
	Toast          func(context.Context, Toast) error
	// Failure is called outside host locks once for each generation that reaches
	// a final failed state.
	Failure func(FailureEvent)
	// Interaction is supplied by the session owner, never by a client attachment.
	// It must honor ctx cancellation; individual client detachment is not cancellation.
	Interaction func(context.Context, InteractionRequest) (InteractionResponse, error)
}

type hostEntry struct {
	installation        Installation
	instance            *Instance
	owner               InstanceID
	epoch, projectEpoch uint64
	context             PluginContext
	failure             *InstanceFailure
	cleanupRecorded     bool
	turns               map[string]struct{}
}

type hostView struct {
	epoch, projectEpoch uint64
	cwd                 string
	session             SessionContext
}

// Host owns discovery and instances for exactly one session. Its worker performs
// sequential initialization without blocking turns. Mutators only publish desired
// state and revoke affected instances; process waits run on the owned worker.
type Host struct {
	interceptorContext   context.Context
	cancelInterceptors   context.CancelFunc
	interceptors         []interceptor
	interceptorRevision  uint64
	tools                map[string]Tool
	nextTool             uint64
	subagents            map[string]Subagent
	nextSubagent         uint64
	reservedSubagents    map[string]struct{}
	commands             map[string]Command
	nextCommand          uint64
	footerItems          []FooterItem
	footerClaims         map[InstanceID]map[string]struct{}
	interactionsInFlight int
	stateChanged         chan struct{}
	changed              chan struct{}
	runDone              chan struct{}
	notifyDone           chan struct{}
	gitDone              chan struct{}
	id                   string
	config               HostConfig
	ctx                  context.Context
	cancel               context.CancelFunc
	mu                   sync.Mutex
	started, closed      bool
	view                 hostView
	next                 uint64
	entries              []*hostEntry
	diagnostics          []string
	cleanupDiagnostics   []string
	discoveryDiagnostics []Diagnostic
	reportedFailures     map[InstanceID]struct{}
	closeErr             error
	wake                 chan struct{}
	done                 chan struct{}
	// Worker-owned discovery state; user installations survive cwd changes.
	plan                        []Installation
	planEpoch, planProjectEpoch uint64
}

// NewHost creates an unstarted, session-local host. A cancelled constructor
// context or Close-before-Start prevents subsequent process launch.
func NewHost(parent context.Context, config HostConfig) *Host {
	ctx, cancel := context.WithCancel(parent)
	config.ReservedIDs = append([]string(nil), config.ReservedIDs...)
	config.ReservedSubagents = append([]string(nil), config.ReservedSubagents...)
	config.Session = cloneSessionContext(config.Session)
	hostID, err := identifier.New("pluginhost_")
	host := &Host{tools: make(map[string]Tool), subagents: make(map[string]Subagent), reservedSubagents: make(map[string]struct{}), commands: make(map[string]Command), stateChanged: make(chan struct{}), changed: make(chan struct{}, 1), runDone: make(chan struct{}), notifyDone: make(chan struct{}), gitDone: make(chan struct{}), id: hostID, config: config, ctx: ctx, cancel: cancel, view: hostView{epoch: 1, projectEpoch: 1, cwd: config.CWD, session: config.Session}, reportedFailures: make(map[InstanceID]struct{}), wake: make(chan struct{}, 1), done: make(chan struct{})}
	for _, name := range config.ReservedSubagents {
		host.reservedSubagents[name] = struct{}{}
	}
	if err != nil {
		host.diagnostics = []string{diagnosticMessage(err)}
		cancel()
	}
	return host
}

func cloneSessionContext(value SessionContext) SessionContext {
	if value.Name != nil {
		name := *value.Name
		value.Name = &name
	}
	return value
}

// Start schedules background loading once. It performs no filesystem work or
// synchronous external callback, and is safe against concurrent Close.
func (h *Host) Start() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started || h.closed {
		return
	}
	h.started = true
	go h.notifyLoop()
	go h.gitLoop()
	go func() { h.run(); close(h.runDone); <-h.notifyDone; <-h.gitDone; close(h.done) }()
}

func (h *Host) signal() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// ChangeCWD preserves user instances and immediately revokes project instances.
// Filesystem discovery and Git probing happen in the background.
func (h *Host) ChangeCWD(cwd string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.view.cwd == cwd {
		return
	}
	h.view.cwd = cwd
	h.view.projectEpoch++
	h.interceptorIdentityLocked()
	h.signalStateLocked()
	h.publishChange()
	for _, entry := range h.entries {
		if entry.installation.Source == Project && entry.instance != nil {
			entry.instance.Revoke()
		}
	}
	h.signal()
}

// Rename updates only the name, so a concurrent cwd change cannot be overwritten
// by a stale metadata record. Ready and initializing instances are reconciled.
func (h *Host) Rename(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.signalStateLocked()
	h.view.session.Name = nil
	if name != "" {
		h.view.session.Name = &name
	}
	h.signal()
}

// Reload revokes all generations immediately and schedules deterministic fresh
// discovery. It never replays old calls or restarts a crash automatically.
func (h *Host) Reload() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.view.epoch++
	h.view.projectEpoch++
	h.interceptorIdentityLocked()
	h.signalStateLocked()
	h.publishChange()
	for _, entry := range h.entries {
		if entry.instance != nil {
			entry.instance.Revoke()
		}
	}
	h.signal()
}

// Close cancels all instance owners before waiting, allowing shutdown across
// plugins to proceed concurrently. Caller cancellation never abandons ownership.
func (h *Host) Close(ctx context.Context) error {
	h.mu.Lock()
	if !h.closed {
		h.closed = true
		h.signalStateLocked()
		h.publishChange()
		h.cancel()
		for _, entry := range h.entries {
			if entry.instance != nil {
				entry.instance.Revoke()
			}
		}
		if !h.started {
			close(h.done)
		}
	}
	h.mu.Unlock()
	h.signal()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closeErr
}

// Instances returns detached status snapshots in deterministic discovery order.
func (h *Host) Instances() []InstanceStatus {
	h.mu.Lock()
	entries := append([]*hostEntry(nil), h.entries...)
	h.mu.Unlock()
	var result []InstanceStatus
	for _, entry := range entries {
		h.mu.Lock()
		instance := entry.instance
		owner := entry.owner
		var failure *InstanceFailure
		if entry.failure != nil {
			value := *entry.failure
			failure = &value
		}
		h.mu.Unlock()
		if instance != nil {
			result = append(result, instance.Status())
		} else if failure != nil {
			result = append(result, InstanceStatus{ID: owner, State: InstanceFailed, Failure: failure})
		} else {
			result = append(result, InstanceStatus{ID: owner, State: InstanceInitializing})
		}
	}
	return result
}

// Warnings returns one bounded, persistent aggregate for the current snapshot.
// Dedicated plugin diagnostics/dismissal UI can consume richer state later.
func (h *Host) Warnings() []string {
	h.mu.Lock()
	messages := append([]string(nil), h.cleanupDiagnostics...)
	messages = append(messages, h.diagnostics...)
	for _, diagnostic := range h.discoveryDiagnostics {
		messages = append(messages, diagnostic.Message)
	}
	h.mu.Unlock()
	for _, status := range h.Instances() {
		if status.Failure != nil {
			messages = append(messages, fmt.Sprintf("%s: %s", status.ID.PluginID, status.Failure.Message))
		}
	}
	if len(messages) == 0 {
		return nil
	}
	return []string{diagnosticMessage(fmt.Errorf("Plugins: %s", strings.Join(messages, "; ")))}
}

func (h *Host) addDiagnostic(message string)        { h.recordDiagnostic(message, false) }
func (h *Host) addCleanupDiagnostic(message string) { h.recordDiagnostic(message, true) }

func (h *Host) reportFailures() {
	for _, status := range h.Instances() {
		h.reportFailure(status)
	}
}

func (h *Host) reportFailure(status InstanceStatus) {
	if status.State != InstanceFailed || status.Failure == nil {
		return
	}
	h.mu.Lock()
	if _, reported := h.reportedFailures[status.ID]; reported {
		h.mu.Unlock()
		return
	}
	h.reportedFailures[status.ID] = struct{}{}
	observer := h.config.Failure
	h.mu.Unlock()
	if observer != nil {
		observer(FailureEvent{Owner: status.ID, Phase: status.Failure.Phase, Message: status.Failure.Message, Stderr: status.Failure.Stderr})
	}
}
func (h *Host) recordDiagnostic(message string, cleanup bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordDiagnosticLocked(message, cleanup)
}

func (h *Host) recordDiagnosticLocked(message string, cleanup bool) {
	message = diagnosticMessage(errors.New(message))
	diagnostics := &h.diagnostics
	if cleanup {
		diagnostics = &h.cleanupDiagnostics
	}
	for _, existing := range *diagnostics {
		if existing == message {
			return
		}
	}
	if len(*diagnostics) < 32 {
		*diagnostics = append(*diagnostics, message)
		h.publishChange()
	}
	h.signal()
}

func (h *Host) run() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	previous := ""
	for {
		h.mu.Lock()
		closed := h.closed || h.ctx.Err() != nil
		view := h.view
		h.mu.Unlock()
		if closed {
			h.mu.Lock()
			h.closed = true
			h.cancel()
			for _, entry := range h.entries {
				if entry.instance != nil {
					entry.instance.Revoke()
				}
			}
			h.mu.Unlock()
			h.retire(view, true)
			return
		}
		h.reconcile(view)
		h.reportFailures()
		warning := strings.Join(h.Warnings(), "\n")
		if warning != previous {
			previous = warning
			h.publishChange()
		}
		select {
		case <-h.ctx.Done():
		case <-h.wake:
		case <-ticker.C:
		}
	}
}

func (h *Host) retire(view hostView, all bool) {
	h.mu.Lock()
	var retiring []*hostEntry
	for _, entry := range h.entries {
		if all || entry.epoch != view.epoch || (entry.installation.Source == Project && entry.projectEpoch != view.projectEpoch) {
			retiring = append(retiring, entry)
			if entry.instance != nil {
				entry.instance.Revoke()
			}
		}
	}
	h.mu.Unlock()
	for index := len(retiring) - 1; index >= 0; index-- {
		entry := retiring[index]
		if entry.instance != nil {
			err := entry.instance.Stop(context.Background())
			h.reportFailure(entry.instance.Status())
			if err != nil && !entry.cleanupRecorded {
				entry.cleanupRecorded = true
				h.addCleanupDiagnostic(fmt.Sprintf("%s cleanup: %v", entry.owner.PluginID, err))
				h.mu.Lock()
				if h.closeErr == nil {
					h.closeErr = err
				}
				h.mu.Unlock()
			}
			// Do not reuse an ID while a process remains unreaped after a cleanup deadline.
			select {
			case <-entry.instance.process.Done():
			default:
				continue
			}
		}
		h.mu.Lock()
		for i, current := range h.entries {
			if current == entry {
				h.entries = append(h.entries[:i], h.entries[i+1:]...)
				delete(h.reportedFailures, entry.owner)
				break
			}
		}
		h.mu.Unlock()
	}
}

func (h *Host) reconcile(view hostView) {
	h.retire(view, false)
	if h.planEpoch != view.epoch || h.planProjectEpoch != view.projectEpoch {
		var users []Installation
		includeUser := h.planEpoch != view.epoch
		if !includeUser {
			for _, installation := range h.plan {
				if installation.Source == User {
					users = append(users, installation)
				}
			}
		}
		result, err := Discover(h.ctx, Discovery{Home: h.config.Home, Cwd: view.cwd, IncludeUser: includeUser, IncludeProject: true, Existing: users, ReservedIDs: h.config.ReservedIDs})
		if err != nil {
			if h.ctx.Err() == nil {
				h.addDiagnostic(err.Error())
			}
			return
		}
		h.mu.Lock()
		if h.closed || h.view.epoch != view.epoch || h.view.projectEpoch != view.projectEpoch {
			h.mu.Unlock()
			return
		}
		var retained []Diagnostic
		if !includeUser {
			for _, diagnostic := range h.discoveryDiagnostics {
				if diagnostic.Source == User {
					retained = append(retained, diagnostic)
				}
			}
		}
		h.discoveryDiagnostics = retained
		for _, diagnostic := range result.Diagnostics {
			if len(h.discoveryDiagnostics) == 32 {
				break
			}
			diagnostic.Message = diagnosticMessage(errors.New(diagnostic.Message))
			h.discoveryDiagnostics = append(h.discoveryDiagnostics, diagnostic)
		}
		if includeUser {
			h.diagnostics = nil
		}
		h.mu.Unlock()
		h.plan = append(users, result.Installations...)
		if len(h.plan) > 128 {
			h.plan = h.plan[:128]
			h.addDiagnostic("session plugin installation limit (128) exceeded")
		}
		h.planEpoch = view.epoch
		h.planProjectEpoch = view.projectEpoch
	}
	for _, installation := range h.plan {
		h.mu.Lock()
		if h.closed || h.view.epoch != view.epoch || h.view.projectEpoch != view.projectEpoch {
			h.mu.Unlock()
			return
		}
		var existing *hostEntry
		for _, entry := range h.entries {
			if entry.owner.PluginID == installation.Manifest.ID {
				existing = entry
				break
			}
		}
		h.mu.Unlock()
		if existing != nil {
			if existing.epoch == view.epoch && (installation.Source == User || existing.projectEpoch == view.projectEpoch) {
				h.reconcileContext(existing)
			}
			continue
		}
		initial, err := h.contextFor(view)
		if err != nil {
			if h.ctx.Err() == nil {
				h.addDiagnostic(err.Error())
			}
			return
		}
		h.mu.Lock()
		if h.closed || h.view.epoch != view.epoch || h.view.projectEpoch != view.projectEpoch {
			h.mu.Unlock()
			return
		}
		if len(h.entries) >= 128 {
			h.mu.Unlock()
			h.addDiagnostic("session plugin instance limit (128) reached while retiring processes")
			return
		}
		h.next++
		entry := &hostEntry{installation: installation, owner: InstanceID{HostID: h.id, SessionID: view.session.ID, PluginID: installation.Manifest.ID, Generation: h.next}, epoch: view.epoch, projectEpoch: view.projectEpoch, context: initial}
		h.entries = append(h.entries, entry)
		h.mu.Unlock()
		handlers := RPCHandlers{
			Request: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				return h.handleRequest(ctx, entry.owner, method, params)
			},
			Notification: func(ctx context.Context, method string, params json.RawMessage) error {
				return h.handleNotification(ctx, entry.owner, method, params)
			},
		}
		instance, err := StartInstance(h.ctx, entry.owner, installation, initial, handlers)
		if err != nil {
			h.mu.Lock()
			entry.failure = &InstanceFailure{Phase: "launch", Message: diagnosticMessage(err)}
			h.mu.Unlock()
			continue
		}
		h.mu.Lock()
		entry.instance = instance
		h.signalStateLocked()
		h.publishChange()
		stale := h.closed || h.view.epoch != entry.epoch || (installation.Source == Project && h.view.projectEpoch != entry.projectEpoch)
		if stale {
			instance.Revoke()
		}
		h.mu.Unlock()
		instance.setRevocationNotify(h.publishChange)
		_ = instance.WaitReady(h.ctx)
		h.reconcileContext(entry)
	}
}

func (h *Host) contextFor(view hostView) (PluginContext, error) {
	project := ProjectContext{Cwd: view.cwd}
	if h.config.Project != nil {
		var err error
		project, err = h.config.Project(h.ctx, view.cwd)
		if err != nil {
			if h.ctx.Err() != nil {
				return PluginContext{}, h.ctx.Err()
			}
			h.addDiagnostic(fmt.Sprintf("project context for %s: %v", view.cwd, err))
			project = ProjectContext{Cwd: view.cwd}
		}
	}
	// Cwd remains authoritative when optional Git metadata cannot be probed.
	project.Cwd = view.cwd
	return PluginContext{Project: project, Session: cloneSessionContext(view.session)}, nil
}

func (h *Host) reconcileContext(entry *hostEntry) {
	h.mu.Lock()
	view := h.view
	instance := entry.instance
	previous := entry.context
	current := !h.closed && entry.epoch == view.epoch && (entry.installation.Source == User || entry.projectEpoch == view.projectEpoch)
	h.mu.Unlock()
	if !current || instance == nil || instance.Status().State != InstanceReady {
		return
	}
	if previous.Project.Cwd != view.cwd {
		next, err := h.contextFor(view)
		if err != nil {
			return
		}
		data, err := json.Marshal(next.Project)
		if err != nil {
			instance.beginStop("runtime", err)
			return
		}
		h.mu.Lock()
		// Admit the project and Git events atomically against cwd/reload. In
		// particular, an A -> B -> A transition must not publish a stale B
		// project event while retaining A as the generation's baseline.
		if h.closed || h.view.epoch != view.epoch || h.view.projectEpoch != view.projectEpoch || h.view.cwd != view.cwd || !h.currentReadyEntryLocked(entry) {
			h.mu.Unlock()
			return
		}
		err = instance.tryNotify("kit/events/project.changed", data)
		if err == nil {
			entry.context.Project.Cwd = next.Project.Cwd
			h.publishGitLocked(entry, next.Project.Git)
			h.signalStateLocked()
		}
		h.mu.Unlock()
		if err != nil {
			instance.beginStop("runtime", err)
			return
		}
	}
	if !sameSessionContext(previous.Session, view.session) {
		data, _ := json.Marshal(view.session)
		ctx, cancel := context.WithTimeout(h.ctx, time.Second)
		err := instance.Notify(ctx, "kit/events/session.changed", data)
		cancel()
		if err != nil {
			instance.beginStop("runtime", err)
			return
		}
		h.mu.Lock()
		entry.context.Session = cloneSessionContext(view.session)
		h.signalStateLocked()
		h.mu.Unlock()
	}
}

func sameSessionContext(a, b SessionContext) bool {
	return a.ID == b.ID && ((a.Name == nil && b.Name == nil) || (a.Name != nil && b.Name != nil && *a.Name == *b.Name))
}

// Every mutation from a handler must check the owner against desired epochs,
// not merely the old instance's readiness at dispatch time.
func (h *Host) unsupportedRequest(ctx context.Context, owner InstanceID, method string) (json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !h.closed {
		for _, entry := range h.entries {
			if entry.owner == owner && entry.epoch == h.view.epoch && (entry.installation.Source == User || entry.projectEpoch == h.view.projectEpoch) {
				h.recordDiagnosticLocked(fmt.Sprintf("%s requested unsupported method %s", owner.PluginID, method), false)
				return nil, rpcError(-32601, "Plugin contribution method is not implemented")
			}
		}
	}
	return nil, rpcError(-32002, "Plugin generation is unavailable")
}

func (h *Host) signalStateLocked() { close(h.stateChanged); h.stateChanged = make(chan struct{}) }

// Notification delivery is independently supervised so a slow initialization
// cannot delay an already accepted command-catalog change.
func (h *Host) publishChange() {
	select {
	case h.changed <- struct{}{}:
	default:
	}
}
func (h *Host) notifyLoop() {
	defer close(h.notifyDone)
	type presentation struct {
		Tools     []Tool
		Subagents []Subagent
		Commands  []Command
		Warnings  []string
		Footer    FooterState
	}
	empty, _ := json.Marshal(presentation{})
	previous := string(empty)
	for {
		select {
		case <-h.runDone:
			return
		case <-h.changed:
			h.mu.Lock()
			h.pruneCommandsLocked()
			h.pruneToolsLocked()
			h.pruneSubagentsLocked()
			h.pruneFooterLocked()
			h.interceptorIdentityLocked()
			h.mu.Unlock()
			projection, _ := json.Marshal(presentation{h.Tools(), h.Subagents(), h.Commands(), h.Warnings(), h.Footer()})
			if string(projection) != previous {
				previous = string(projection)
				if h.config.Changed != nil {
					h.config.Changed()
				}
			}
		}
	}
}
