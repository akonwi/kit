// Package session owns Kit's session registry and live droid harnesses.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
	"github.com/akonwi/kit/internal/identifier"
)

const maxPromptTextBytes = 128 << 10

var (
	ErrBusy              = errors.New("session already has an active parent run")
	ErrReloadBusy        = errors.New("session cannot be reloaded while work is active")
	ErrDeleteBusy        = errors.New("session cannot be deleted while work is active")
	ErrClosed            = errors.New("session manager is closed")
	ErrInvalidInput      = errors.New("invalid session input")
	ErrRunNotAbortable   = errors.New("run is not abortable")
	ErrBashBusy          = errors.New("bash execution is busy")
	ErrBashNotAbortable  = errors.New("bash execution is not abortable")
	ErrDroidStoreMissing = errors.New("initialized session droid store is missing")
	ErrNotTemporary      = errors.New("session is not temporary")
	ErrTemporary         = errors.New("temporary session requires disposal")
)

// CreateInput contains metadata for a new persisted or temporary session.
type CreateInput struct {
	ID            string
	CWD           string
	Name          string
	Model         string
	ThinkingLevel string
	Temporary     bool
}

// RunReservation identifies a durably admitted droid turn.
type RunReservation struct {
	SessionID string
	TurnID    string
	RunID     string
}

// ProviderErrorKind is Kit's protocol-facing provider failure class.
type ProviderErrorKind string

const (
	ProviderErrorAuthentication ProviderErrorKind = "authentication"
	ProviderErrorEntitlement    ProviderErrorKind = "entitlement"
	ProviderErrorUsageLimit     ProviderErrorKind = "usage_limit"
	ProviderErrorRateLimit      ProviderErrorKind = "rate_limit"
	ProviderErrorTransport      ProviderErrorKind = "transport"
	ProviderErrorProtocol       ProviderErrorKind = "protocol"
)

// PromptResult is the terminal projection of one droid turn.
type PromptResult struct {
	SessionID    string
	TurnID       string
	RunID        string
	Text         string
	StopReason   string
	ErrorKind    ProviderErrorKind
	ErrorMessage string
	Status       RunStatus
}

// When locks must nest, acquire them in this order: runtime admissionMu,
// runtime mu, workspace mutationMu, event-log mu, bashMu, then Manager.mu.
// Registry lookups should otherwise release Manager.mu before touching a runtime.
type Manager struct {
	store           Repository
	providers       droids.Providers
	bundleBuilder   RuntimeBundleBuilder
	droidDirectory  string
	temporaryDroids bool
	bashContext     context.Context
	cancelBash      context.CancelCauseFunc

	mu                sync.Mutex
	runtimes          map[string]*runtime
	loading           map[string]*runtimeLoad
	deleting          map[string]bool
	creating          map[string]*sessionCreation
	temporary         map[string]SessionRecord
	disposals         map[string]*temporaryDisposal
	disposedTemporary map[string]struct{}
	disposedOrder     []string
	closed            bool
	shutdownDone      chan struct{}
	shutdownErr       error
	runs              sync.WaitGroup
	loads             sync.WaitGroup
	ops               sync.WaitGroup
	admissions        sync.WaitGroup
	cleanups          sync.WaitGroup

	bashMu           sync.Mutex
	bashActive       map[string]*activeBashExecution
	bashHistory      map[string]map[string]BashExecution
	bashNextSequence map[string]int64
	bashSlots        chan struct{}
	bashRuns         sync.WaitGroup
}

type runtimeLoad struct {
	done    chan struct{}
	runtime *runtime
	err     error
}

type sessionCreation struct {
	temporary bool
	done      chan struct{}
}

type temporaryDisposal struct {
	done chan struct{}
	err  error
}

type runtime struct {
	droid       *droids.Droid
	store       droids.Store
	closeStore  func() error
	bundle      RuntimeBundle
	workspace   *workspaceScope
	events      *eventLog
	eventCursor droids.EventSequence

	// Lock order is admissionMu, then mu, then workspace.mutationMu. admissionMu
	// remains held for a complete parent turn. mu protects the current droid and
	// immutable bundle snapshot together with run bookkeeping.
	admissionMu sync.Mutex
	mu          sync.Mutex
	activeRun   string
	runs        map[string]*liveRun
	recovery    *droids.ExecutionSnapshot
}

type liveRun struct {
	record         RunProjection
	result         PromptResult
	done           chan struct{}
	completeStream bool
}

type ManagerOption func(*managerOptions) error
type managerOptions struct{ droidDirectory string }

func WithDroidStoreDirectory(directory string) ManagerOption {
	return func(options *managerOptions) error {
		if strings.TrimSpace(directory) == "" {
			return fmt.Errorf("droid store directory is required")
		}
		absolute, err := filepath.Abs(directory)
		if err != nil {
			return fmt.Errorf("resolve droid store directory: %w", err)
		}
		options.droidDirectory = absolute
		return nil
	}
}

func NewManager(store Repository, providers droids.Providers, bundleBuilder RuntimeBundleBuilder, opts ...ManagerOption) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is required")
	}
	if providers == nil {
		return nil, fmt.Errorf("droids providers are required")
	}
	if nilRuntimeBundleBuilder(bundleBuilder) {
		return nil, fmt.Errorf("runtime bundle builder is required")
	}
	options := managerOptions{}
	for _, apply := range opts {
		if apply != nil {
			if err := apply(&options); err != nil {
				return nil, err
			}
		}
	}
	temporary := false
	if options.droidDirectory == "" {
		if locator, ok := store.(interface{ DroidStoreDirectory() string }); ok {
			options.droidDirectory = locator.DroidStoreDirectory()
		}
	}
	if options.droidDirectory == "" {
		var err error
		options.droidDirectory, err = os.MkdirTemp("", "kit-droids-")
		if err != nil {
			return nil, fmt.Errorf("create temporary droid store directory: %w", err)
		}
		temporary = true
	}
	if err := os.MkdirAll(options.droidDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create droid store directory: %w", err)
	}
	bashContext, cancelBash := context.WithCancelCause(context.Background())
	return &Manager{
		store: store, providers: providers, bundleBuilder: bundleBuilder,
		droidDirectory: options.droidDirectory, temporaryDroids: temporary,
		bashContext: bashContext, cancelBash: cancelBash,
		runtimes: make(map[string]*runtime), loading: make(map[string]*runtimeLoad), deleting: make(map[string]bool), creating: make(map[string]*sessionCreation), temporary: make(map[string]SessionRecord), disposals: make(map[string]*temporaryDisposal), disposedTemporary: make(map[string]struct{}),
		shutdownDone: make(chan struct{}),
		bashActive:   make(map[string]*activeBashExecution), bashHistory: make(map[string]map[string]BashExecution),
		bashNextSequence: make(map[string]int64),
		bashSlots:        make(chan struct{}, maxConcurrentDirectBash),
	}, nil
}

func (m *Manager) Create(ctx context.Context, input CreateInput) (SessionRecord, error) {
	if err := m.beginOperation(); err != nil {
		return SessionRecord{}, err
	}
	defer m.ops.Done()
	if !filepath.IsAbs(input.CWD) || !validWorkspacePath(input.CWD) {
		return SessionRecord{}, fmt.Errorf("%w: session cwd must be a safe bounded absolute path", ErrInvalidInput)
	}
	cwd := filepath.Clean(input.CWD)
	name := strings.TrimSpace(input.Name)
	if len(name) > 256 || !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 {
		return SessionRecord{}, fmt.Errorf("%w: session name must be valid UTF-8 without NUL and at most 256 bytes", ErrInvalidInput)
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return SessionRecord{}, fmt.Errorf("%w: session cwd must be an existing directory", ErrInvalidInput)
	}
	model, ok := m.providers.Model(input.Model)
	if !ok {
		return SessionRecord{}, fmt.Errorf("%w: unknown model %q", ErrInvalidInput, input.Model)
	}
	if err := validateThinkingLevel(model, input.ThinkingLevel); err != nil {
		return SessionRecord{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	id := input.ID
	clientSelectedID := id != ""
	if clientSelectedID {
		if !identifier.Valid(id, "session_") {
			return SessionRecord{}, fmt.Errorf("%w: invalid session id", ErrInvalidInput)
		}
	} else {
		var err error
		id, err = identifier.New("session_")
		if err != nil {
			return SessionRecord{}, err
		}
	}
	creation := &sessionCreation{temporary: input.Temporary, done: make(chan struct{})}
	m.mu.Lock()
	if m.creating[id] != nil {
		m.mu.Unlock()
		return SessionRecord{}, fmt.Errorf("%w: session id is already being created", ErrInvalidInput)
	}
	if _, disposed := m.disposedTemporary[id]; disposed {
		m.mu.Unlock()
		return SessionRecord{}, fmt.Errorf("%w: temporary session id was already disposed", ErrInvalidInput)
	}
	if m.deleting[id] {
		m.mu.Unlock()
		return SessionRecord{}, ErrDeleteBusy
	}
	if _, temporaryExists := m.temporary[id]; temporaryExists && !input.Temporary {
		m.mu.Unlock()
		return SessionRecord{}, fmt.Errorf("%w: session id is assigned to a temporary session", ErrInvalidInput)
	}
	m.creating[id] = creation
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.creating[id] == creation {
			delete(m.creating, id)
		}
		close(creation.done)
		m.mu.Unlock()
	}()
	requested := NewSession{
		ID: id, CWD: cwd, Name: name, Persistent: !input.Temporary,
		ModelProvider: model.Provider, ModelID: model.ID, ThinkingLevel: input.ThinkingLevel,
	}
	if input.Temporary {
		if persisted, loadErr := m.store.GetSession(ctx, id); loadErr == nil && persisted.ID != "" {
			return SessionRecord{}, fmt.Errorf("%w: session id is already assigned to a persisted session", ErrInvalidInput)
		} else if loadErr != nil && !errors.Is(loadErr, ErrNotFound) {
			return SessionRecord{}, loadErr
		}
		now := time.Now().UTC()
		record := SessionRecord{
			ID: id, CWD: cwd, Name: requested.Name, Persistent: false,
			ModelProvider: model.Provider, ModelID: model.ID, ThinkingLevel: input.ThinkingLevel,
			CreatedAt: now, UpdatedAt: now,
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed {
			return SessionRecord{}, ErrClosed
		}
		if m.deleting[id] {
			return SessionRecord{}, ErrDeleteBusy
		}
		if _, disposed := m.disposedTemporary[id]; disposed {
			return SessionRecord{}, fmt.Errorf("%w: temporary session id was already disposed", ErrInvalidInput)
		}
		if existing, ok := m.temporary[id]; ok {
			if sessionMatchesCreate(existing, requested) {
				return existing, nil
			}
			return SessionRecord{}, fmt.Errorf("%w: session id is already assigned to a different request", ErrInvalidInput)
		}
		if m.deleting[id] || m.loading[id] != nil || m.runtimes[id] != nil {
			return SessionRecord{}, fmt.Errorf("%w: session id is already active", ErrInvalidInput)
		}
		m.temporary[id] = record
		return record, nil
	}
	record, err := m.store.CreateSession(ctx, requested)
	if err == nil || !clientSelectedID {
		return record, err
	}
	existing, loadErr := m.store.GetSession(ctx, id)
	if loadErr == nil {
		if sessionMatchesCreate(existing, requested) {
			return existing, nil
		}
		return SessionRecord{}, fmt.Errorf("%w: session id is already assigned to a different request", ErrInvalidInput)
	}
	return SessionRecord{}, err
}

func sessionMatchesCreate(record SessionRecord, input NewSession) bool {
	// Name is intentionally omitted: it is mutable after creation, while a delayed
	// replay of the original create request must still resolve to this session.
	return record.ID == input.ID && record.CWD == input.CWD &&
		record.Persistent == input.Persistent && record.ParentSessionID == input.ParentSessionID &&
		record.ModelProvider == input.ModelProvider && record.ModelID == input.ModelID &&
		record.ThinkingLevel == input.ThinkingLevel && record.ArchivedAt == nil
}

// Rename replaces one persisted session's display name.
func (m *Manager) Rename(ctx context.Context, sessionID, name string) (SessionRecord, error) {
	if err := m.beginOperation(); err != nil {
		return SessionRecord{}, err
	}
	defer m.ops.Done()
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 256 || !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 {
		return SessionRecord{}, fmt.Errorf("%w: session name must be non-empty valid UTF-8 without NUL and at most 256 bytes", ErrInvalidInput)
	}
	m.mu.Lock()
	_, temporary := m.temporary[sessionID]
	m.mu.Unlock()
	if temporary {
		return SessionRecord{}, fmt.Errorf("%w: temporary sessions cannot be renamed", ErrInvalidInput)
	}
	return m.store.RenameSession(ctx, sessionID, name)
}

// Delete archives a persisted session and releases an idle loaded runtime.
func (m *Manager) Delete(ctx context.Context, sessionID string) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if !identifier.Valid(sessionID, "session_") {
		return fmt.Errorf("%w: invalid session id", ErrInvalidInput)
	}
	m.mu.Lock()
	if m.deleting[sessionID] || m.creating[sessionID] != nil || m.loading[sessionID] != nil {
		m.mu.Unlock()
		return ErrDeleteBusy
	}
	_, temporary := m.temporary[sessionID]
	if temporary {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrTemporary, sessionID)
	}
	m.deleting[sessionID] = true
	loaded := m.runtimes[sessionID]
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.deleting, sessionID)
		m.mu.Unlock()
	}()

	if loaded != nil {
		if !loaded.admissionMu.TryLock() {
			return ErrDeleteBusy
		}
		defer loaded.admissionMu.Unlock()
		loaded.mu.Lock()
		defer loaded.mu.Unlock()
		loaded.workspace.mutationMu.Lock()
		defer loaded.workspace.mutationMu.Unlock()
		active := loaded.activeRun != ""
		if active {
			return ErrDeleteBusy
		}
	}
	m.bashMu.Lock()
	if m.bashActive[sessionID] != nil {
		m.bashMu.Unlock()
		return ErrDeleteBusy
	}
	if err := m.store.ArchiveSession(ctx, sessionID, time.Now().UTC()); err != nil {
		m.bashMu.Unlock()
		return err
	}
	delete(m.bashHistory, sessionID)
	delete(m.bashNextSequence, sessionID)
	m.bashMu.Unlock()

	m.mu.Lock()
	if loaded != nil && m.runtimes[sessionID] == loaded {
		delete(m.runtimes, sessionID)
	}
	m.mu.Unlock()
	if loaded != nil {
		// Archival is the authoritative delete commit. Runtime cleanup is best
		// effort so an interrupted client cannot turn a committed delete into a
		// misleading retryable failure or allow a second runtime to load.
		_ = loaded.close(ctx)
	}
	return nil
}

// DisposeTemporary revokes a temporary session, cancels its active work, and
// removes all process-local state. Cleanup continues if the caller stops
// waiting, while concurrent callers observe the same disposal result.
func (m *Manager) DisposeTemporary(ctx context.Context, sessionID string) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if !identifier.Valid(sessionID, "session_") {
		return fmt.Errorf("%w: invalid session id", ErrInvalidInput)
	}

	m.mu.Lock()
	if pending := m.disposals[sessionID]; pending != nil {
		m.mu.Unlock()
		return waitTemporaryDisposal(ctx, pending)
	}
	if _, disposed := m.disposedTemporary[sessionID]; disposed {
		m.mu.Unlock()
		return nil
	}
	_, temporary := m.temporary[sessionID]
	creation := m.creating[sessionID]
	if !temporary && creation == nil {
		m.mu.Unlock()
		_, err := m.store.GetSession(ctx, sessionID)
		if err == nil {
			return fmt.Errorf("%w: %s", ErrNotTemporary, sessionID)
		}
		return err
	}
	if creation != nil && !creation.temporary {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrNotTemporary, sessionID)
	}
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	pending := &temporaryDisposal{done: make(chan struct{})}
	m.disposals[sessionID] = pending
	m.deleting[sessionID] = true
	m.rememberDisposedTemporary(sessionID)
	m.cleanups.Add(1)
	m.mu.Unlock()

	var creationDone <-chan struct{}
	if creation != nil {
		creationDone = creation.done
	}
	go m.finishTemporaryDisposal(sessionID, creationDone, pending)
	return waitTemporaryDisposal(ctx, pending)
}

func waitTemporaryDisposal(ctx context.Context, pending *temporaryDisposal) error {
	select {
	case <-pending.done:
		return pending.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

const maxDisposedTemporaryTombstones = 1024

func (m *Manager) rememberDisposedTemporary(sessionID string) {
	if _, exists := m.disposedTemporary[sessionID]; exists {
		return
	}
	m.disposedTemporary[sessionID] = struct{}{}
	m.disposedOrder = append(m.disposedOrder, sessionID)
	if len(m.disposedOrder) > maxDisposedTemporaryTombstones {
		oldest := m.disposedOrder[0]
		m.disposedOrder = m.disposedOrder[1:]
		delete(m.disposedTemporary, oldest)
	}
}

func (m *Manager) finishTemporaryDisposal(
	sessionID string,
	creationDone <-chan struct{},
	pending *temporaryDisposal,
) {
	defer m.cleanups.Done()
	if creationDone != nil {
		<-creationDone
	}
	m.mu.Lock()
	loaded := m.runtimes[sessionID]
	loading := m.loading[sessionID]
	m.mu.Unlock()
	if loading != nil {
		<-loading.done
		if loaded == nil {
			loaded = loading.runtime
		}
	}

	var cleanupErr error
	var runDone <-chan struct{}
	if loaded != nil {
		loaded.mu.Lock()
		if run := loaded.runs[loaded.activeRun]; run != nil {
			runDone = run.done
		}
		if runDone != nil {
			if err := loaded.droid.Abort(context.Background()); err != nil && !errors.Is(err, droids.ErrNoActiveExecution) {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
		loaded.mu.Unlock()
	}

	var bashDone <-chan struct{}
	m.bashMu.Lock()
	if active := m.bashActive[sessionID]; active != nil {
		active.cancel(errBashTemporaryDisposed)
		bashDone = active.done
	}
	m.bashMu.Unlock()
	if runDone != nil {
		<-runDone
	}
	if bashDone != nil {
		<-bashDone
	}

	if loaded != nil {
		loaded.mu.Lock()
		loaded.workspace.mutationMu.Lock()
		cleanupErr = errors.Join(cleanupErr, loaded.close(context.Background()))
	}
	m.bashMu.Lock()
	delete(m.bashHistory, sessionID)
	delete(m.bashNextSequence, sessionID)
	delete(m.bashActive, sessionID)
	m.bashMu.Unlock()
	m.mu.Lock()
	if m.runtimes[sessionID] == loaded {
		delete(m.runtimes, sessionID)
	}
	delete(m.temporary, sessionID)
	delete(m.deleting, sessionID)
	delete(m.disposals, sessionID)
	pending.err = cleanupErr
	close(pending.done)
	m.mu.Unlock()
	if loaded != nil {
		loaded.workspace.mutationMu.Unlock()
		loaded.mu.Unlock()
	}
}

func (m *Manager) List(ctx context.Context, cwd string) ([]SessionRecord, error) {
	if err := m.beginOperation(); err != nil {
		return nil, err
	}
	defer m.ops.Done()
	if cwd != "" {
		if !filepath.IsAbs(cwd) {
			return nil, fmt.Errorf("%w: session list cwd must be absolute", ErrInvalidInput)
		}
		cwd = filepath.Clean(cwd)
	}
	return m.store.ListSessions(ctx, cwd)
}

// StartPrompt admits one droid turn and returns its canonical identity.
func (m *Manager) StartPrompt(ctx context.Context, sessionID, prompt string) (RunReservation, error) {
	return m.startPrompt(ctx, sessionID, prompt, "", "")
}

// StartPromptCommand expands and admits one command from the runtime's immutable snapshot.
func (m *Manager) StartPromptCommand(ctx context.Context, sessionID, name, args string) (RunReservation, error) {
	if strings.TrimSpace(name) != name || name == "" || len(name) > 128 || len(args) > maxPromptTextBytes || !utf8.ValidString(args) || strings.IndexByte(args, 0) >= 0 {
		return RunReservation{}, fmt.Errorf("%w: prompt command name or arguments are invalid", ErrInvalidInput)
	}
	return m.startPrompt(ctx, sessionID, "", name, args)
}

func (m *Manager) startPrompt(ctx context.Context, sessionID, prompt, commandName, commandArgs string) (RunReservation, error) {
	if err := m.beginAdmission(); err != nil {
		return RunReservation{}, err
	}
	defer m.admissions.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if commandName == "" && (strings.TrimSpace(prompt) == "" || len(prompt) > maxPromptTextBytes || !utf8.ValidString(prompt) || strings.IndexByte(prompt, 0) >= 0) {
		return RunReservation{}, fmt.Errorf("%w: prompt must be non-empty valid UTF-8 without NUL and at most 128 KiB", ErrInvalidInput)
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return RunReservation{}, err
	}
	if !loaded.admissionMu.TryLock() {
		return RunReservation{}, ErrBusy
	}
	loaded.mu.Lock()
	release := func() {
		loaded.mu.Unlock()
		loaded.admissionMu.Unlock()
	}
	if m.sessionDeleting(sessionID) {
		release()
		return RunReservation{}, ErrDeleteBusy
	}
	if commandName != "" {
		if loaded.bundle.PromptCommands == nil {
			release()
			return RunReservation{}, fmt.Errorf("prompt command %q: %w", commandName, ErrNotFound)
		}
		command, ok := loaded.bundle.PromptCommands.Lookup(commandName)
		if !ok {
			release()
			return RunReservation{}, fmt.Errorf("prompt command %q: %w", commandName, ErrNotFound)
		}
		prompt, err = command.Expand(commandArgs)
		if err != nil {
			release()
			return RunReservation{}, fmt.Errorf("%w: expand prompt command %q: %v", ErrInvalidInput, commandName, err)
		}
	}
	snapshot, err := loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		release()
		return RunReservation{}, err
	}
	if snapshot.Active != nil {
		release()
		return RunReservation{}, ErrBusy
	}
	subscription, err := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{
		After: loaded.eventCursor, IncludeTransient: true, Buffer: 256,
	})
	if err != nil {
		release()
		return RunReservation{}, err
	}
	handle, err := loaded.droid.Prompt(ctx, droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: prompt}},
	}, droids.PromptOptions{})
	if err != nil {
		subscription.Close()
		release()
		if errors.Is(err, droids.ErrBusy) {
			return RunReservation{}, ErrBusy
		}
		return RunReservation{}, err
	}
	turnID := string(handle.TurnID())
	if err := loaded.events.reset(); err != nil {
		_ = loaded.droid.Abort(context.Background())
		subscription.Close()
		release()
		return RunReservation{}, err
	}
	run := &liveRun{record: RunProjection{
		ID: turnID, SessionID: sessionID, TurnID: turnID, Status: RunStatusRunning,
	}, done: make(chan struct{}), completeStream: true}
	pruneRuns(loaded.runs, 128)
	loaded.activeRun = turnID
	loaded.runs[turnID] = run
	if err := loaded.events.append([]NewEvent{
		{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventRunStarted, Status: RunStatusRunning},
		{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventUserMessage, Text: boundedLiveText(prompt)},
	}); err != nil {
		_ = loaded.droid.Abort(context.Background())
		subscription.Close()
		release()
		return RunReservation{}, err
	}
	loaded.mu.Unlock()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = loaded.droid.Abort(context.Background())
		subscription.Close()
		loaded.admissionMu.Unlock()
		return RunReservation{}, ErrClosed
	}
	m.runs.Add(1)
	m.mu.Unlock()
	go m.executePrompt(loaded, run, handle, subscription, sessionID)
	return RunReservation{SessionID: sessionID, TurnID: turnID, RunID: turnID}, nil
}

func (m *Manager) RunPrompt(ctx context.Context, sessionID, prompt string) (PromptResult, error) {
	reservation, err := m.StartPrompt(ctx, sessionID, prompt)
	if err != nil {
		return PromptResult{}, err
	}
	return m.waitRun(ctx, sessionID, reservation.RunID)
}

func (m *Manager) GetRun(ctx context.Context, sessionID, runID string) (RunProjection, error) {
	if err := m.beginOperation(); err != nil {
		return RunProjection{}, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return RunProjection{}, err
	}
	loaded.mu.Lock()
	run := loaded.runs[runID]
	if run != nil {
		record := run.record
		loaded.mu.Unlock()
		return record, nil
	}
	turn, err := loaded.droid.Turn(ctx, droids.TurnID(runID))
	loaded.mu.Unlock()
	if errors.Is(err, droids.ErrTurnNotFound) {
		return RunProjection{}, fmt.Errorf("run %q: %w", runID, ErrNotFound)
	}
	if err != nil {
		return RunProjection{}, err
	}
	record := RunProjection{
		ID: runID, SessionID: sessionID, TurnID: runID,
		Status: projectExecutionStatus(turn.Status),
	}
	if turn.Error != nil {
		record.Error = turn.Error.Message
	}
	if record.Status != RunStatusCompleted && record.Error == "" {
		record.Error = "droid execution " + string(turn.Status)
	}
	return record, nil
}

func (m *Manager) waitRun(ctx context.Context, sessionID, runID string) (PromptResult, error) {
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return PromptResult{}, err
	}
	loaded.mu.Lock()
	run := loaded.runs[runID]
	loaded.mu.Unlock()
	if run == nil {
		return PromptResult{}, fmt.Errorf("run %q: %w", runID, ErrNotFound)
	}
	select {
	case <-run.done:
		loaded.mu.Lock()
		result := run.result
		loaded.mu.Unlock()
		return result, nil
	case <-ctx.Done():
		return PromptResult{}, ctx.Err()
	}
}

func (m *Manager) executePrompt(loaded *runtime, run *liveRun, handle droids.ExecutionHandle, subscription droids.Subscription, sessionID string) {
	defer m.runs.Done()
	turnID := run.record.TurnID
	drainDone := make(chan error, 1)
	go func() { drainDone <- m.drainRunEvents(subscription, loaded, sessionID, turnID, turnID) }()
	outcome, waitErr := handle.Wait(context.Background())
	terminalState, continuationErr := continueDroidToTerminal(loaded.droid, outcome.Status)
	waitErr = errors.Join(waitErr, continuationErr)
	if terminalState.Execution != nil {
		outcome.Status = terminalState.Execution.Status
		if outcome.Error == nil {
			outcome.Error = terminalState.Execution.Error
		}
	}
	subscription.Close()
	drainErr := <-drainDone
	if drainErr != nil {
		loaded.events.invalidate()
	}
	result := promptResultFromOutcome(sessionID, turnID, outcome, errors.Join(waitErr, drainErr))

	loaded.mu.Lock()
	if err := loaded.events.append([]NewEvent{{
		SessionID: sessionID, TurnID: turnID, RunID: turnID,
		Kind: EventRunFinished, Status: result.Status, ErrorKind: result.ErrorKind,
		ErrorMessage: result.ErrorMessage,
	}}); err != nil {
		loaded.events.invalidate()
	}
	run.record.Status = result.Status
	run.record.Error = result.ErrorMessage
	run.result = result
	if loaded.activeRun == turnID {
		loaded.activeRun = ""
	}
	close(run.done)
	loaded.mu.Unlock()
	loaded.admissionMu.Unlock()
}

func promptResultFromOutcome(sessionID, turnID string, outcome droids.Outcome, waitErr error) PromptResult {
	result := PromptResult{SessionID: sessionID, TurnID: turnID, RunID: turnID, Status: projectExecutionStatus(outcome.Status)}
	if outcome.FinalMessage != nil {
		if assistant, ok := outcome.FinalMessage.Message.(droids.AssistantMessage); ok {
			result.Text = assistant.Text()
			result.StopReason = string(assistant.StopReason)
			result.ErrorKind = projectDroidErrorKind(assistant.ErrorKind)
			result.ErrorMessage = assistant.ErrorMessage
		}
	}
	if outcome.Error != nil && result.ErrorMessage == "" {
		result.ErrorMessage = outcome.Error.Message
	}
	if result.Status != RunStatusCompleted && result.ErrorMessage == "" {
		if waitErr != nil {
			result.ErrorMessage = waitErr.Error()
		} else {
			result.ErrorMessage = "droid execution " + string(outcome.Status)
		}
	}
	return result
}

func continueDroidToTerminal(droid *droids.Droid, status droids.ExecutionStatus) (droids.QuiescentState, error) {
	state := droids.QuiescentState{Execution: &droids.ExecutionSnapshot{Status: status}}
	var continuationErr error
	for status == droids.ExecutionPaused || status == droids.ExecutionInterrupted {
		if err := droid.Resume(context.Background()); err != nil {
			continuationErr = errors.Join(continuationErr, err)
			if errors.Is(err, droids.ErrUnsafeContinuation) {
				if abortErr := droid.Abort(context.Background()); abortErr != nil {
					return state, errors.Join(continuationErr, abortErr)
				}
			} else if errors.Is(err, droids.ErrClosed) {
				return state, continuationErr
			} else {
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		var err error
		state, err = droid.WaitQuiescent(context.Background())
		if err != nil {
			return state, errors.Join(continuationErr, err)
		}
		if state.Execution == nil {
			return state, continuationErr
		}
		status = state.Execution.Status
	}
	return state, continuationErr
}

func projectExecutionStatus(status droids.ExecutionStatus) RunStatus {
	switch status {
	case droids.ExecutionCompleted:
		return RunStatusCompleted
	case droids.ExecutionFailed:
		return RunStatusFailed
	case droids.ExecutionAborted:
		return RunStatusAborted
	default:
		return RunStatusInterrupted
	}
}

func (m *Manager) drainRunEvents(subscription droids.Subscription, loaded *runtime, sessionID, turnID, runID string) error {
	for envelope := range subscription.Events() {
		if envelope.Durable {
			loaded.mu.Lock()
			if envelope.Sequence > loaded.eventCursor {
				loaded.eventCursor = envelope.Sequence
			}
			loaded.mu.Unlock()
		}
		if err := loaded.events.append(projectDroidEvent(sessionID, turnID, runID, envelope.Event)); err != nil {
			return err
		}
	}
	if err := subscription.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (m *Manager) Abort(ctx context.Context, sessionID, runID string) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return err
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	matches := loaded.activeRun == runID
	if !matches {
		return fmt.Errorf("%w: run %q is not current", ErrRunNotAbortable, runID)
	}
	if err := loaded.droid.Abort(ctx); err != nil && !errors.Is(err, droids.ErrNoActiveExecution) {
		return err
	}
	return nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		runtimes := make([]*runtime, 0, len(m.runtimes))
		for sessionID, loaded := range m.runtimes {
			if m.disposals[sessionID] == nil {
				runtimes = append(runtimes, loaded)
			}
		}
		m.runtimes = nil
		m.cancelBash(errBashShutdown)
		go m.finishShutdown(runtimes)
	}
	done := m.shutdownDone
	m.mu.Unlock()
	select {
	case <-done:
		m.mu.Lock()
		err := m.shutdownErr
		m.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) finishShutdown(runtimes []*runtime) {
	var shutdownErr error
	for _, loaded := range runtimes {
		loaded.mu.Lock()
		shutdownErr = errors.Join(shutdownErr, loaded.close(context.Background()))
		loaded.mu.Unlock()
	}
	m.admissions.Wait()
	m.loads.Wait()
	m.runs.Wait()
	m.bashRuns.Wait()
	m.ops.Wait()
	m.cleanups.Wait()
	if m.temporaryDroids {
		shutdownErr = errors.Join(shutdownErr, os.RemoveAll(m.droidDirectory))
	}
	m.mu.Lock()
	m.shutdownErr = shutdownErr
	close(m.shutdownDone)
	m.mu.Unlock()
}

func (m *Manager) Close() { _ = m.Shutdown(context.Background()) }

func (m *Manager) runtime(ctx context.Context, sessionID string) (*runtime, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	if m.deleting[sessionID] {
		m.mu.Unlock()
		return nil, ErrDeleteBusy
	}
	if loaded := m.runtimes[sessionID]; loaded != nil {
		m.mu.Unlock()
		return loaded, nil
	}
	if pending := m.loading[sessionID]; pending != nil {
		m.mu.Unlock()
		select {
		case <-pending.done:
			return pending.runtime, pending.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pending := &runtimeLoad{done: make(chan struct{})}
	m.loading[sessionID] = pending
	m.loads.Add(1)
	m.mu.Unlock()

	loaded, err := m.loadRuntime(ctx, sessionID)
	m.mu.Lock()
	delete(m.loading, sessionID)
	startRecovery := false
	if m.closed || m.deleting[sessionID] {
		if loaded != nil {
			_ = loaded.close(context.Background())
		}
		if m.closed {
			loaded, err = nil, ErrClosed
		} else {
			loaded, err = nil, ErrDeleteBusy
		}
	} else if err == nil {
		m.runtimes[sessionID] = loaded
		startRecovery = loaded.recovery != nil
		if startRecovery {
			loaded.admissionMu.Lock()
			m.runs.Add(1)
		}
	}
	pending.runtime, pending.err = loaded, err
	close(pending.done)
	m.mu.Unlock()
	if startRecovery {
		go m.resumeRuntime(loaded, sessionID)
	}
	m.loads.Done()
	return loaded, err
}

func (m *Manager) loadRuntime(ctx context.Context, sessionID string) (*runtime, error) {
	record, err := m.sessionRecord(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if record.ArchivedAt != nil {
		return nil, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	return m.newDroid(ctx, record)
}

func (m *Manager) sessionRecord(ctx context.Context, sessionID string) (SessionRecord, error) {
	m.mu.Lock()
	record, temporary := m.temporary[sessionID]
	m.mu.Unlock()
	if temporary {
		return record, nil
	}
	return m.store.GetSession(ctx, sessionID)
}

func (m *Manager) newDroid(ctx context.Context, record SessionRecord) (*runtime, error) {
	if !identifier.Valid(record.ID, "session_") {
		return nil, fmt.Errorf("session %q has an invalid droid identity", record.ID)
	}
	workspace := newWorkspaceScope(record.CWD)
	bundle, err := m.bundleBuilder.Build(ctx, record, workspace.CWD)
	if err != nil {
		return nil, fmt.Errorf("build runtime bundle for session %q: %w", record.ID, err)
	}
	bundle.Tools = append(bundle.Tools, m.changeCWDTool(record.ID, workspace))
	var store droids.Store
	closeStore := func() error { return nil }
	if record.Persistent {
		path := filepath.Join(m.droidDirectory, record.ID+".db")
		if record.DroidInitializedAt != nil {
			if _, err := os.Stat(path); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("%w: %s", ErrDroidStoreMissing, record.ID)
				}
				return nil, err
			}
		}
		sqliteStore, err := sqlitestore.Open(ctx, sqlitestore.Options{Path: path})
		if err != nil {
			return nil, fmt.Errorf("open droid store for session %q: %w", record.ID, err)
		}
		store = sqliteStore
		closeStore = sqliteStore.Close
	} else {
		store = droids.NewMemoryStore()
	}
	droid, snapshot, err := m.openDroid(ctx, record, store, bundle)
	if err != nil {
		_ = closeStore()
		return nil, fmt.Errorf("open runtime for session %q: %w", record.ID, err)
	}
	if record.Persistent && record.DroidInitializedAt == nil {
		if err := m.store.MarkDroidInitialized(ctx, record.ID, time.Now().UTC()); err != nil {
			_ = droid.Close()
			_ = closeStore()
			return nil, err
		}
	}
	events, err := newEventLog()
	if err != nil {
		_ = droid.Close()
		_ = closeStore()
		return nil, err
	}
	loaded := &runtime{
		droid: droid, store: store, closeStore: closeStore, bundle: cloneRuntimeBundle(bundle),
		workspace: workspace, events: events, eventCursor: snapshot.LastEvent,
		runs: make(map[string]*liveRun),
	}
	quiescent, err := droid.WaitQuiescent(ctx)
	if err != nil {
		_ = loaded.close(context.Background())
		return nil, err
	}
	if quiescent.Execution != nil && (quiescent.Execution.Status == droids.ExecutionPaused || quiescent.Execution.Status == droids.ExecutionInterrupted) {
		turnID := string(quiescent.TurnID)
		run := &liveRun{record: RunProjection{ID: turnID, SessionID: record.ID, TurnID: turnID, Status: RunStatusRunning}, done: make(chan struct{})}
		loaded.activeRun = turnID
		loaded.runs[turnID] = run
		copy := *quiescent.Execution
		loaded.recovery = &copy
		_ = loaded.events.append([]NewEvent{{SessionID: record.ID, TurnID: turnID, RunID: turnID, Kind: EventRunStarted, Status: RunStatusRunning}})
		loaded.events.invalidate()
	}
	return loaded, nil
}

func (m *Manager) openDroid(ctx context.Context, record SessionRecord, store droids.Store, bundle RuntimeBundle) (*droids.Droid, droids.Snapshot, error) {
	droid, err := droids.Open(ctx, droids.ConversationID(record.ID), droids.Config{
		Store: store, Providers: m.providers,
		Model:     record.ModelProvider + "/" + record.ModelID,
		Reasoning: record.ThinkingLevel, SystemPrompt: bundle.Prompt.Prompt,
		Tools: bundle.Tools,
	})
	if err != nil {
		return nil, droids.Snapshot{}, err
	}
	snapshot, err := droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		_ = droid.Close()
		return nil, droids.Snapshot{}, err
	}
	return droid, snapshot, nil
}

func (m *Manager) resumeRuntime(loaded *runtime, sessionID string) {
	defer m.runs.Done()
	loaded.mu.Lock()
	turnID := loaded.activeRun
	run := loaded.runs[turnID]
	loaded.mu.Unlock()
	if run == nil {
		loaded.admissionMu.Unlock()
		return
	}
	subscription, err := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{After: loaded.eventCursor, IncludeTransient: true, Buffer: 256})
	if err != nil {
		abortErr := loaded.droid.Abort(context.Background())
		state, waitErr := loaded.droid.WaitQuiescent(context.Background())
		status := RunStatusInterrupted
		if abortErr == nil && waitErr == nil && state.Execution != nil {
			status = projectExecutionStatus(state.Execution.Status)
		}
		result := PromptResult{
			SessionID: sessionID, TurnID: turnID, RunID: turnID, Status: status,
			ErrorMessage: "resume event subscription: " + errors.Join(err, abortErr, waitErr).Error(),
		}
		loaded.mu.Lock()
		loaded.events.invalidate()
		run.record.Status, run.record.Error, run.result = result.Status, result.ErrorMessage, result
		loaded.activeRun = ""
		close(run.done)
		loaded.mu.Unlock()
		loaded.admissionMu.Unlock()
		return
	}
	drainDone := make(chan error, 1)
	go func() { drainDone <- m.drainRunEvents(subscription, loaded, sessionID, turnID, turnID) }()
	state, waitErr := continueDroidToTerminal(loaded.droid, droids.ExecutionInterrupted)
	subscription.Close()
	drainErr := <-drainDone
	if drainErr != nil {
		loaded.events.invalidate()
	}
	outcome := droids.Outcome{Status: droids.ExecutionInterrupted}
	if state.Execution != nil {
		outcome.Status, outcome.Error = state.Execution.Status, state.Execution.Error
	}
	result := promptResultFromOutcome(sessionID, turnID, outcome, errors.Join(waitErr, drainErr))
	loaded.mu.Lock()
	if err := loaded.events.append([]NewEvent{{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventRunFinished, Status: result.Status, ErrorKind: result.ErrorKind, ErrorMessage: result.ErrorMessage}}); err != nil {
		loaded.events.invalidate()
	}
	run.record.Status, run.record.Error, run.result = result.Status, result.ErrorMessage, result
	loaded.activeRun = ""
	close(run.done)
	loaded.mu.Unlock()
	loaded.admissionMu.Unlock()
}

func (r *runtime) close(ctx context.Context) error {
	return errors.Join(r.droid.Shutdown(ctx), r.closeStore())
}

func pruneRuns(runs map[string]*liveRun, limit int) {
	for len(runs) >= limit {
		removed := false
		for id, run := range runs {
			if run.record.Status != RunStatusRunning {
				delete(runs, id)
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
}

func (m *Manager) beginAdmission() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	m.admissions.Add(1)
	return nil
}

func (m *Manager) beginOperation() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	m.ops.Add(1)
	return nil
}

func projectDroidErrorKind(kind droids.ErrorKind) ProviderErrorKind {
	switch kind {
	case droids.ErrorAuthentication:
		return ProviderErrorAuthentication
	case droids.ErrorEntitlement:
		return ProviderErrorEntitlement
	case droids.ErrorUsageLimit:
		return ProviderErrorUsageLimit
	case droids.ErrorRateLimit:
		return ProviderErrorRateLimit
	case droids.ErrorTransport:
		return ProviderErrorTransport
	case droids.ErrorProtocol:
		return ProviderErrorProtocol
	default:
		return ""
	}
}

func validateThinkingLevel(model droids.Model, level string) error {
	if level == "" {
		return nil
	}
	if level == "none" || level == "off" {
		if (model.API != droids.ModelAPIOpenAIResponses && model.API != droids.ModelAPIOpenAICodexResponses) || !model.Reasoning {
			return nil
		}
		for _, supported := range model.ReasoningLevels {
			if supported == "none" {
				return nil
			}
		}
		return fmt.Errorf("model %q does not support disabling reasoning", model.ID)
	}
	if !model.Reasoning {
		return fmt.Errorf("model %q does not support reasoning", model.ID)
	}
	for _, supported := range model.ReasoningLevels {
		if supported == level {
			return nil
		}
	}
	return fmt.Errorf("model %q does not support reasoning level %q", model.ID, level)
}
