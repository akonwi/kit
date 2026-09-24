// Package session owns Kit's session registry and live droid harnesses.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/mcpruntime"
	"github.com/akonwi/kit/internal/peer"
	"github.com/akonwi/kit/internal/scratchpad"
	"github.com/akonwi/kit/internal/subagent"
)

const (
	maxPromptTextBytes           = 128 << 10
	maxPromptAttachments         = 8
	maxPromptAttachmentBytes     = 20 << 20
	maxPromptTextAttachmentBytes = 1 << 20
	maxFollowUps                 = 64
)

var (
	ErrBusy                        = errors.New("session already has an active parent run")
	ErrReloadBusy                  = errors.New("session cannot be reloaded while work is active")
	ErrConfigureBusy               = errors.New("session cannot be configured while work is active")
	ErrDeleteBusy                  = errors.New("session cannot be deleted while work is active")
	ErrClosed                      = errors.New("session manager is closed")
	ErrInvalidInput                = errors.New("invalid session input")
	ErrRunNotAbortable             = errors.New("run is not abortable")
	ErrBashBusy                    = errors.New("bash execution is busy")
	ErrBashNotAbortable            = errors.New("bash execution is not abortable")
	ErrDroidStoreMissing           = errors.New("initialized session droid store is missing")
	ErrNotTemporary                = errors.New("session is not temporary")
	ErrTemporary                   = errors.New("temporary session requires disposal")
	ErrTranscriptCursorUnavailable = errors.New("transcript cursor is unavailable")
)

// CreateInput contains metadata for a new persisted or temporary session.
type CreateInput struct {
	ID              string
	CWD             string
	Name            string
	Model           string
	ThinkingLevel   string
	Temporary       bool
	ParentSessionID string
}

// ForkInput identifies the linked child created from a settled session.
type ForkInput struct {
	ID   string
	Name string
}

// ForkResult is the published child and its exact semantic fork point.
type ForkResult struct {
	Session SessionRecord
	Point   droids.ForkPoint
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

// mcpCloseDeadline bounds graceful MCP shutdown during runtime teardown. A
// remote server that never answers the shutdown handshake must not stall the
// session; supervised server processes still end with the runtime lifetime.
const mcpCloseDeadline = 5 * time.Second

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

// When locks must nest, acquire them in this order: runtime transitionMu,
// admissionMu, runtime mu, workspace mutationMu, event-log mu, bashMu, then
// Manager.mu.
// Registry lookups should otherwise release Manager.mu before touching a runtime.
type Manager struct {
	store                 Repository
	scratchpads           scratchpad.Repository
	providers             droids.Providers
	bundleBuilder         RuntimeBundleBuilder
	pluginFactory         PluginHostFactory
	pluginSubagents       PluginSubagentCatalogRegistry
	pluginContext         context.Context
	cancelPlugins         context.CancelFunc
	modelContextWindow    func(string) int
	autoName              bool
	attachments           attachment.Store
	annotations           *kitannotation.Service
	mailbox               subagent.Repository
	peerQueries           peer.Repository
	peerLimits            peer.Limits
	subagentOwnerCanceler interface{ CancelOwner(string) }
	droidDirectory        string
	temporaryDroids       bool
	bashContext           context.Context
	cancelBash            context.CancelCauseFunc
	mailboxContext        context.Context
	cancelMailbox         context.CancelFunc

	mu                sync.Mutex
	metadataGates     map[string]*sync.Mutex
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
	mailboxWorkers    map[string]*mailboxReactionWorker
	mailboxBlocked    map[string]bool
	mailboxSlots      chan struct{}
	mailboxStarted    bool
	peerWorkers       map[string]*mailboxReactionWorker
	peerStarted       bool

	bashMu           sync.Mutex
	bashActive       map[string]*activeBashExecution
	bashHistory      map[string]map[string]BashExecution
	bashNextSequence map[string]int64
	bashSlots        chan struct{}
	bashRuns         sync.WaitGroup
}

type mailboxReactionWorker struct {
	dirty bool
}

type runtimeLoad struct {
	pluginName *string // Latest rename accepted while this runtime is loading.
	done       chan struct{}
	runtime    *runtime
	err        error
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
	closeMu      sync.Mutex
	closeStarted bool
	closeDone    chan struct{}
	closeErr     error
	cleanupOwner *sync.WaitGroup

	pluginToolState            pluginToolState
	pluginContributions        pluginContributionState
	interception               *pluginInterceptorBridge
	turnEvents                 *pluginTurnEventBridge
	plugins                    PluginHost
	closePluginSubagentCatalog func()
	pluginNotifications        pluginNotificationHub
	droid                      *droids.Droid
	model                      droids.Model
	store                      droids.Store
	closeStore                 func() error
	bundle                     RuntimeBundle
	workspace                  *workspaceScope
	events                     *eventLog
	eventCursor                droids.EventSequence
	eventChanged               chan struct{}
	scratchpadOwnerID          string

	subagentEventMu       sync.Mutex
	subagentEventPending  *NewEvent
	subagentEventDraining bool

	// Lock order is transitionMu, admissionMu, then mu, workspace.mutationMu,
	// and the event-log mutex. Manager.mu is never held while acquiring them.
	// admissionMu remains held for a complete parent turn. mu protects the current droid and
	// immutable bundle snapshot together with run bookkeeping.
	transitionMu          sync.Mutex
	admissionMu           sync.Mutex
	mu                    sync.Mutex
	activeRun             string
	autoNameRunning       bool
	autoNameAgain         bool
	autoNameSettled       bool
	runs                  map[string]*liveRun
	recovery              *droids.ExecutionSnapshot
	configurationWarnings []string
	followUps             []PromptInput
	interactions          *interactionBroker
}

func (r *runtime) signalEventChangedLocked() {
	close(r.eventChanged)
	r.eventChanged = make(chan struct{})
}

// PromptInput is one ordered prompt with optional durable attachments.
type PromptInput struct {
	queuedAnnotations *kitannotation.QueuedSubmission
	fromQueue         bool
	Text              string
	AttachmentIDs     []string
	AnnotationIDs     []uint64
}

// FollowUpQueue is the renderer-safe state of one session's deferred prompts.
type FollowUpQueue struct {
	Count         int
	Previews      []string
	AnnotationIDs []uint64
}

// PromptSubmission reports whether a prompt started immediately or was queued.
type PromptSubmission struct {
	Reservation RunReservation
	Queued      bool
	Queue       FollowUpQueue
}

// FollowUpRestore contains every atomically drained follow-up in queue order.
type FollowUpRestore struct {
	Messages []PromptInput
	Queue    FollowUpQueue
}

// FollowUpPromotion reports how many queued messages became steering.
type FollowUpPromotion struct {
	Promoted int
	Queue    FollowUpQueue
}

type liveRun struct {
	record         RunProjection
	result         PromptResult
	done           chan struct{}
	completeStream bool
	autonomous     bool
	peerRequest    *peer.Request
}

type ManagerOption func(*managerOptions) error
type managerOptions struct {
	pluginFactory      PluginHostFactory
	pluginSubagents    PluginSubagentCatalogRegistry
	droidDirectory     string
	attachments        attachment.Store
	annotations        *kitannotation.Service
	modelContextWindow func(string) int
	autoName           bool
}

// WithAutomaticNaming names an unnamed session from a private in-memory fork
// after two successful user turns. Explicit names are left unchanged.
func WithAutomaticNaming() ManagerOption {
	return func(options *managerOptions) error {
		options.autoName = true
		return nil
	}
}

// WithPluginSubagentCatalogRegistry connects live applied definitions to the
// existing parent subagent tool.
func WithPluginSubagentCatalogRegistry(registry PluginSubagentCatalogRegistry) ManagerOption {
	return func(options *managerOptions) error {
		if registry == nil {
			return fmt.Errorf("plugin subagent catalog registry is required")
		}
		options.pluginSubagents = registry
		return nil
	}
}

// WithModelContextWindow supplies a context-window override for an exact model selector.
func WithModelContextWindow(resolve func(string) int) ManagerOption {
	return func(options *managerOptions) error {
		if resolve == nil {
			return fmt.Errorf("model context window resolver is required")
		}
		options.modelContextWindow = resolve
		return nil
	}
}

// WithAnnotationService supplies server-owned draft annotation resolution.
func WithAnnotationService(service *kitannotation.Service) ManagerOption {
	return func(options *managerOptions) error {
		if service == nil {
			return fmt.Errorf("annotation service is required")
		}
		options.annotations = service
		return nil
	}
}

func WithAttachmentStore(store attachment.Store) ManagerOption {
	return func(options *managerOptions) error {
		if store == nil {
			return fmt.Errorf("attachment store is required")
		}
		options.attachments = store
		return nil
	}
}

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
	mailboxContext, cancelMailbox := context.WithCancel(context.Background())
	pluginContext, cancelPlugins := context.WithCancel(context.Background())
	manager := &Manager{
		store: store, providers: providers, bundleBuilder: bundleBuilder, modelContextWindow: options.modelContextWindow, attachments: options.attachments, annotations: options.annotations, autoName: options.autoName,
		droidDirectory: options.droidDirectory, temporaryDroids: temporary,
		pluginFactory: options.pluginFactory, pluginSubagents: options.pluginSubagents, pluginContext: pluginContext, cancelPlugins: cancelPlugins,
		bashContext: bashContext, cancelBash: cancelBash,
		mailboxContext: mailboxContext, cancelMailbox: cancelMailbox,
		runtimes: make(map[string]*runtime), loading: make(map[string]*runtimeLoad), deleting: make(map[string]bool), creating: make(map[string]*sessionCreation), temporary: make(map[string]SessionRecord), disposals: make(map[string]*temporaryDisposal), disposedTemporary: make(map[string]struct{}),
		shutdownDone: make(chan struct{}), mailboxWorkers: make(map[string]*mailboxReactionWorker),
		mailboxBlocked: make(map[string]bool), mailboxSlots: make(chan struct{}, maxConcurrentReactions),
		peerLimits: peer.DefaultLimits(), peerWorkers: make(map[string]*mailboxReactionWorker),
		bashActive: make(map[string]*activeBashExecution), bashHistory: make(map[string]map[string]BashExecution),
		bashNextSequence: make(map[string]int64),
		bashSlots:        make(chan struct{}, maxConcurrentDirectBash),
	}
	if mailbox, ok := store.(subagent.Repository); ok {
		manager.mailbox = mailbox
	}
	if scratchpads, ok := store.(scratchpad.Repository); ok {
		manager.scratchpads = scratchpads
	}
	if queries, ok := store.(peer.Repository); ok {
		manager.peerQueries = queries
	}
	return manager, nil
}

// StartSubagentMailbox begins startup reconciliation and periodic delivery of
// durable child completions after daemon component wiring is complete.
func (m *Manager) StartSubagentMailbox() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.mailbox == nil {
		return nil
	}
	if m.mailboxStarted {
		return nil
	}
	m.mailboxStarted = true
	m.ops.Add(1)
	go m.scanPendingSubagentMailbox()
	return nil
}

// SetSubagentOwnerCanceler connects owner archival to the daemon-wide child supervisor.
// It must be called during composition before sessions accept work.
func (m *Manager) SetSubagentOwnerCanceler(canceler interface{ CancelOwner(string) }) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.runtimes) != 0 || len(m.loading) != 0 {
		return ErrBusy
	}
	m.subagentOwnerCanceler = canceler
	return nil
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
	if !validSessionName(name) {
		return SessionRecord{}, fmt.Errorf("%w: session name must be renderer-safe UTF-8 and at most 256 bytes", ErrInvalidInput)
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return SessionRecord{}, fmt.Errorf("%w: session cwd must be an existing directory", ErrInvalidInput)
	}
	model, ok := m.providers.Model(input.Model)
	if !ok {
		return SessionRecord{}, fmt.Errorf("%w: unknown model %q", ErrInvalidInput, input.Model)
	}
	var requestedThinking *string
	if input.ThinkingLevel != "" {
		requestedThinking = &input.ThinkingLevel
	}
	effectiveThinking, _, err := resolveConfigurationThinking(model, "", requestedThinking)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	input.ThinkingLevel = effectiveThinking
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
	scratchpadOwnerID := id
	if input.Temporary {
		scratchpadOwnerID = ""
	}
	requested := NewSession{
		ID: id, CWD: cwd, Name: name, Persistent: !input.Temporary, ParentSessionID: input.ParentSessionID,
		ScratchpadOwnerID: scratchpadOwnerID,
		ModelProvider:     model.Provider, ModelID: model.ID, ThinkingLevel: input.ThinkingLevel,
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
			ConfigurationRevision: 1, CreatedAt: now, UpdatedAt: now,
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

// Fork publishes a linked persistent child copied from a settled source.
func (m *Manager) Fork(ctx context.Context, sourceSessionID string, input ForkInput) (ForkResult, error) {
	if err := m.beginOperation(); err != nil {
		return ForkResult{}, err
	}
	defer m.ops.Done()

	source, err := m.runtime(ctx, sourceSessionID)
	if err != nil {
		return ForkResult{}, err
	}
	sourceMetadata, err := m.sessionRecord(ctx, sourceSessionID)
	if err != nil {
		return ForkResult{}, err
	}
	if !sourceMetadata.Persistent {
		return ForkResult{}, fmt.Errorf("%w: temporary sessions cannot be forked", ErrInvalidInput)
	}

	childID := input.ID
	if childID == "" {
		childID, err = identifier.New("session_")
		if err != nil {
			return ForkResult{}, err
		}
	} else if !identifier.Valid(childID, "session_") {
		return ForkResult{}, fmt.Errorf("%w: invalid child session id", ErrInvalidInput)
	}
	if childID == sourceSessionID {
		return ForkResult{}, fmt.Errorf("%w: child session id must differ from source", ErrInvalidInput)
	}
	name := strings.TrimSpace(input.Name)
	if input.ID != "" {
		if existing, lookupErr := m.store.GetSession(ctx, childID); lookupErr == nil {
			if existing.ParentSessionID == sourceSessionID && existing.ScratchpadOwnerID == sourceMetadata.ScratchpadOwnerID && existing.DroidInitializedAt != nil && (name == "" || existing.Name == name) {
				childRuntime, runtimeErr := m.runtime(ctx, childID)
				if runtimeErr == nil {
					childRuntime.transitionMu.Lock()
					childRuntime.mu.Lock()
					snapshot, snapshotErr := childRuntime.droid.Snapshot(ctx, droids.SnapshotOptions{})
					childRuntime.mu.Unlock()
					childRuntime.transitionMu.Unlock()
					if snapshotErr == nil && snapshot.Conversation.ForkedFrom != nil && snapshot.Conversation.ForkedFrom.ConversationID == droids.ConversationID(sourceSessionID) {
						source.transitionMu.Lock()
						source.mu.Lock()
						informDroidOfFork(ctx, source, childID)
						source.mu.Unlock()
						source.transitionMu.Unlock()
						return ForkResult{Session: existing, Point: *snapshot.Conversation.ForkedFrom}, nil
					}
				}
			}
			return ForkResult{}, fmt.Errorf("%w: child session id is already in use", ErrInvalidInput)
		} else if !errors.Is(lookupErr, ErrNotFound) {
			return ForkResult{}, lookupErr
		}
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ForkResult{}, ErrClosed
	}
	if m.creating[childID] != nil || m.deleting[childID] || m.loading[childID] != nil || m.runtimes[childID] != nil {
		m.mu.Unlock()
		return ForkResult{}, fmt.Errorf("%w: child session id is already in use", ErrInvalidInput)
	}
	creation := &sessionCreation{done: make(chan struct{})}
	m.creating[childID] = creation
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.creating[childID] == creation {
			delete(m.creating, childID)
		}
		close(creation.done)
		m.mu.Unlock()
	}()
	if _, lookupErr := m.store.GetSession(ctx, childID); lookupErr == nil {
		return ForkResult{}, fmt.Errorf("%w: child session id is already in use", ErrInvalidInput)
	} else if !errors.Is(lookupErr, ErrNotFound) {
		return ForkResult{}, lookupErr
	}

	path := filepath.Join(m.droidDirectory, childID+".db")
	if _, statErr := os.Stat(path); statErr == nil {
		return ForkResult{}, fmt.Errorf("%w: child droid store already exists", ErrInvalidInput)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return ForkResult{}, statErr
	}
	destination, err := sqlitestore.Open(ctx, sqlitestore.Options{Path: path})
	if err != nil {
		return ForkResult{}, fmt.Errorf("open child droid store: %w", err)
	}
	published := false
	attachmentsForked := false
	defer func() {
		_ = destination.Close()
		if !published && attachmentsForked {
			_ = m.attachments.RemoveSession(context.Background(), childID)
		}
		if !published {
			_ = os.Remove(path)
			_ = os.Remove(path + "-wal")
			_ = os.Remove(path + "-shm")
		}
	}()

	source.transitionMu.Lock()
	defer source.transitionMu.Unlock()
	if !source.admissionMu.TryLock() {
		return ForkResult{}, ErrBusy
	}
	defer source.admissionMu.Unlock()
	record, err := m.sessionRecord(ctx, sourceSessionID)
	if err != nil {
		return ForkResult{}, err
	}
	if name == "" {
		name = forkSessionName(record.Name)
	}
	if !validSessionName(name) {
		return ForkResult{}, fmt.Errorf("%w: fork name must be renderer-safe UTF-8 and at most 256 bytes", ErrInvalidInput)
	}
	source.mu.Lock()
	busy := source.activeRun != "" || len(source.followUps) != 0
	source.mu.Unlock()
	m.bashMu.Lock()
	busy = busy || m.bashActive[sourceSessionID] != nil
	m.bashMu.Unlock()
	if busy {
		return ForkResult{}, ErrBusy
	}
	forked, err := source.droid.Fork(ctx, droids.ConversationID(childID), droids.ForkOptions{Store: destination})
	if err != nil {
		return ForkResult{}, err
	}
	if forked.Droid == nil {
		return ForkResult{}, fmt.Errorf("fork child droid is unavailable")
	}
	if err := forked.Droid.Close(); err != nil {
		return ForkResult{}, fmt.Errorf("close fork child: %w", err)
	}
	if err := destination.Close(); err != nil {
		return ForkResult{}, fmt.Errorf("close child droid store: %w", err)
	}
	if m.attachments != nil {
		forker, ok := m.attachments.(attachment.SessionForker)
		if !ok {
			return ForkResult{}, fmt.Errorf("fork attachments: attachment store does not support session forks")
		}
		attachmentsForked = true
		if err := forker.ForkSession(ctx, sourceSessionID, childID); err != nil {
			return ForkResult{}, fmt.Errorf("fork attachments: %w", err)
		}
	}

	initializedAt := time.Now().UTC()
	child, err := m.store.CreateSession(ctx, NewSession{
		ID: childID, CWD: record.CWD, Name: name, Persistent: true,
		ParentSessionID: sourceSessionID, ScratchpadOwnerID: record.ScratchpadOwnerID,
		ModelProvider: record.ModelProvider,
		ModelID:       record.ModelID, ThinkingLevel: record.ThinkingLevel,
		DroidInitializedAt: &initializedAt,
	})
	if err != nil {
		reconciled, lookupErr := m.store.GetSession(context.Background(), childID)
		if lookupErr != nil || reconciled.ParentSessionID != sourceSessionID || reconciled.ScratchpadOwnerID != record.ScratchpadOwnerID || reconciled.DroidInitializedAt == nil {
			return ForkResult{}, err
		}
		child = reconciled
	}
	child.ParentSessionName = record.Name
	published = true
	// The fork is published; a best-effort notification must not fail it.
	informDroidOfFork(ctx, source, childID)
	return ForkResult{Session: child, Point: forked.Point}, nil
}

// Callers hold the parent's transition lock to keep its droid stable.
// Inform is safe during an active run; do not wait for its admission lock.
func informDroidOfFork(ctx context.Context, parent *runtime, childID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := parent.droid.Inform(ctx, droids.BoundaryMessage{
		ID: "session-fork:" + childID, Kind: "session_forked", Source: "session-manager",
		Content: []droids.InputContent{droids.TextInput{Text: "This session was forked into session " + childID + ". The fork has independent state."}},
	}); err != nil {
		slog.WarnContext(ctx, "Could not notify parent of session fork", "child_session_id", childID, "error", err)
	}
}

func forkSessionName(parent string) string {
	name := "fork"
	if strings.TrimSpace(parent) != "" {
		name = "fork: " + strings.TrimSpace(parent)
	}
	for len(name) > 256 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func validSessionName(name string) bool {
	if len(name) > 256 || !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func sessionMatchesCreate(record SessionRecord, input NewSession) bool {
	// Name is intentionally omitted: it is mutable after creation, while a delayed
	// replay of the original create request must still resolve to this session.
	return record.ID == input.ID && record.CWD == input.CWD &&
		record.Persistent == input.Persistent && record.ParentSessionID == input.ParentSessionID &&
		record.ScratchpadOwnerID == input.ScratchpadOwnerID &&
		record.ModelProvider == input.ModelProvider && record.ModelID == input.ModelID &&
		record.ThinkingLevel == input.ThinkingLevel && record.ArchivedAt == nil
}

func (m *Manager) lockSessionMetadata(sessionID string) func() {
	m.mu.Lock()
	if m.metadataGates == nil {
		m.metadataGates = make(map[string]*sync.Mutex)
	}
	gate := m.metadataGates[sessionID]
	if gate == nil {
		gate = &sync.Mutex{}
		m.metadataGates[sessionID] = gate
	}
	m.mu.Unlock()
	gate.Lock()
	return gate.Unlock
}

// Rename replaces one session's display name.
func (m *Manager) Rename(ctx context.Context, sessionID, name string) (SessionRecord, error) {
	if err := m.beginOperation(); err != nil {
		return SessionRecord{}, err
	}
	defer m.ops.Done()
	name = strings.TrimSpace(name)
	if name == "" || !validSessionName(name) {
		return SessionRecord{}, fmt.Errorf("%w: session name must be non-empty renderer-safe UTF-8 and at most 256 bytes", ErrInvalidInput)
	}
	unlockMetadata := m.lockSessionMetadata(sessionID)
	defer unlockMetadata()
	m.mu.Lock()
	if m.deleting[sessionID] {
		m.mu.Unlock()
		return SessionRecord{}, ErrDeleteBusy
	}
	_, temporary := m.temporary[sessionID]
	m.mu.Unlock()
	if temporary {
		m.mu.Lock()
		if m.deleting[sessionID] {
			m.mu.Unlock()
			return SessionRecord{}, ErrDeleteBusy
		}
		record, ok := m.temporary[sessionID]
		if !ok {
			m.mu.Unlock()
			return SessionRecord{}, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
		}
		record.Name = name
		updatedAt := time.Now().UTC()
		if !updatedAt.After(record.UpdatedAt) {
			updatedAt = record.UpdatedAt.Add(time.Nanosecond)
		}
		record.UpdatedAt = updatedAt
		m.temporary[sessionID] = record
		loaded := m.runtimes[sessionID]
		if loaded == nil {
			if pending := m.loading[sessionID]; pending != nil {
				latest := name
				pending.pluginName = &latest
			}
		}
		m.mu.Unlock()
		m.publishSessionRenamed(loaded, record)
		return record, nil
	}
	renamed, err := m.store.RenameSession(ctx, sessionID, name)
	if err != nil {
		return SessionRecord{}, err
	}
	m.mu.Lock()
	loaded := m.runtimes[sessionID]
	if loaded == nil {
		if pending := m.loading[sessionID]; pending != nil {
			latest := name
			pending.pluginName = &latest
		}
	}
	deleting := m.deleting[sessionID]
	m.mu.Unlock()
	if !deleting {
		m.publishSessionRenamed(loaded, renamed)
	}
	return renamed, nil
}

func (m *Manager) publishSessionRenamed(loaded *runtime, record SessionRecord) {
	if loaded == nil {
		return
	}
	if loaded.plugins != nil {
		loaded.plugins.Rename(record.Name)
	}
	if err := loaded.events.append([]NewEvent{{
		SessionID: record.ID, Kind: EventSessionRenamed, SessionName: record.Name,
	}}); err != nil {
		loaded.events.invalidate()
	}
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
		if !loaded.transitionMu.TryLock() {
			return ErrDeleteBusy
		}
		defer loaded.transitionMu.Unlock()
		if !loaded.admissionMu.TryLock() {
			return ErrDeleteBusy
		}
		defer loaded.admissionMu.Unlock()
		loaded.mu.Lock()
		defer loaded.mu.Unlock()
		loaded.workspace.mutationMu.Lock()
		defer loaded.workspace.mutationMu.Unlock()
		active := loaded.activeRun != "" || len(loaded.followUps) > 0
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
	if m.subagentOwnerCanceler != nil {
		m.subagentOwnerCanceler.CancelOwner(sessionID)
	}

	m.mu.Lock()
	if loaded != nil && m.runtimes[sessionID] == loaded {
		delete(m.runtimes, sessionID)
	}
	m.mu.Unlock()
	if loaded != nil {
		// Archival is the authoritative delete commit. Runtime cleanup is best
		// effort so an interrupted client cannot turn a committed delete into a
		// misleading retryable failure or allow a second runtime to load.
		_ = loaded.close(ctx, "deleted")
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
		loaded.transitionMu.Lock()
		loaded.mu.Lock()
		queued := loaded.followUps
		loaded.followUps = nil
		loaded.mu.Unlock()
		for _, prompt := range queued {
			if prompt.queuedAnnotations != nil {
				prompt.queuedAnnotations.Release()
			}
		}
		loaded.mu.Lock()
		loaded.workspace.mutationMu.Lock()
		cleanupErr = errors.Join(cleanupErr, loaded.close(context.Background(), "deleted"))
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
		loaded.transitionMu.Unlock()
	}
}

// Get returns current metadata for one persisted or temporary session.
func (m *Manager) Get(ctx context.Context, sessionID string) (SessionRecord, error) {
	if err := m.beginOperation(); err != nil {
		return SessionRecord{}, err
	}
	defer m.ops.Done()
	if !identifier.Valid(sessionID, "session_") {
		return SessionRecord{}, fmt.Errorf("%w: invalid session id", ErrInvalidInput)
	}
	m.mu.Lock()
	deleting := m.deleting[sessionID]
	m.mu.Unlock()
	if deleting {
		return SessionRecord{}, ErrDeleteBusy
	}
	record, err := m.sessionRecord(ctx, sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	if record.ArchivedAt != nil {
		return SessionRecord{}, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	return record, nil
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
	records, err := m.store.ListSessions(ctx, cwd)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(records))
	for _, record := range records {
		names[record.ID] = record.Name
	}
	for index := range records {
		parentID := records[index].ParentSessionID
		if parentID == "" {
			continue
		}
		if name, ok := names[parentID]; ok {
			records[index].ParentSessionName = name
			continue
		}
		if parent, lookupErr := m.store.GetSession(ctx, parentID); lookupErr == nil {
			records[index].ParentSessionName = parent.Name
		}
	}
	return records, nil
}

func (m *Manager) touchSessionActivity(ctx context.Context, sessionID string, activityAt time.Time) error {
	activityAt = activityAt.UTC()
	m.mu.Lock()
	if m.deleting[sessionID] {
		m.mu.Unlock()
		return ErrDeleteBusy
	}
	if record, temporary := m.temporary[sessionID]; temporary {
		if activityAt.After(record.UpdatedAt) {
			record.UpdatedAt = activityAt
			m.temporary[sessionID] = record
		}
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	return m.store.TouchSession(ctx, sessionID, activityAt)
}

// SubmitPrompt preserves the text-only caller contract.
func (m *Manager) SubmitPrompt(ctx context.Context, sessionID, prompt string) (PromptSubmission, error) {
	return m.SubmitPromptInput(ctx, sessionID, PromptInput{Text: prompt})
}

// SubmitPromptInput atomically starts an idle session or queues a structured follow-up.
func (m *Manager) SubmitPromptInput(ctx context.Context, sessionID string, prompt PromptInput) (PromptSubmission, error) {
	if err := m.beginOperation(); err != nil {
		return PromptSubmission{}, err
	}
	defer m.ops.Done()
	if err := validatePromptInput(prompt); err != nil {
		return PromptSubmission{}, err
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return PromptSubmission{}, err
	}
	loaded.transitionMu.Lock()
	defer loaded.transitionMu.Unlock()
	if err := m.validateQueueRuntime(sessionID, loaded); err != nil {
		return PromptSubmission{}, err
	}
	loaded.mu.Lock()
	if loaded.activeRun != "" {
		if len(loaded.followUps) >= maxFollowUps {
			loaded.mu.Unlock()
			return PromptSubmission{}, fmt.Errorf("%w: follow-up queue capacity reached", ErrBusy)
		}
		for _, queued := range loaded.followUps {
			for _, id := range queued.AnnotationIDs {
				if slices.Contains(prompt.AnnotationIDs, id) {
					loaded.mu.Unlock()
					return PromptSubmission{}, fmt.Errorf("%w: annotation %d already belongs to a queued message", ErrInvalidInput, id)
				}
			}
		}
		loaded.mu.Unlock()
		prepared, err := m.prepareAnnotations(ctx, loaded, sessionID, prompt)
		if err != nil {
			return PromptSubmission{}, err
		}
		var annotationRecords []kitannotation.Record
		if prepared != nil {
			annotationRecords = prepared.Records
			defer prepared.Abort()
		}
		annotationSubmissionID := ""
		if prepared != nil {
			annotationSubmissionID, err = identifier.New("annotation_submission_")
			if err != nil {
				return PromptSubmission{}, err
			}
		}
		if _, err := m.resolvePromptContent(ctx, sessionID, loaded.model, prompt, annotationRecords, annotationSubmissionID); err != nil {
			return PromptSubmission{}, err
		}
		if prepared != nil {
			prompt.queuedAnnotations = prepared.Queue()
		}
		loaded.mu.Lock()
		loaded.followUps = append(loaded.followUps, clonePromptInputs([]PromptInput{prompt})[0])
		queue := projectFollowUpQueue(loaded.followUps)
		loaded.mu.Unlock()
		return PromptSubmission{Queued: true, Queue: queue}, nil
	}
	loaded.mu.Unlock()
	reservation, err := m.StartPromptInput(ctx, sessionID, prompt)
	if err != nil {
		return PromptSubmission{}, err
	}
	return PromptSubmission{Reservation: reservation}, nil
}

// validateQueueRuntime rejects callers that waited while their runtime was revoked.
func (m *Manager) validateQueueRuntime(sessionID string, loaded *runtime) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.deleting[sessionID] || m.runtimes[sessionID] != loaded {
		return ErrDeleteBusy
	}
	return nil
}

// RestoreFollowUps atomically drains every deferred prompt in queue order.
func (m *Manager) RestoreFollowUps(ctx context.Context, sessionID string) (FollowUpRestore, error) {
	if err := m.beginOperation(); err != nil {
		return FollowUpRestore{}, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return FollowUpRestore{}, err
	}
	loaded.transitionMu.Lock()
	defer loaded.transitionMu.Unlock()
	if err := m.validateQueueRuntime(sessionID, loaded); err != nil {
		return FollowUpRestore{}, err
	}
	loaded.mu.Lock()
	messages := clonePromptInputs(loaded.followUps)
	loaded.mu.Unlock()
	for index := range messages {
		if messages[index].queuedAnnotations != nil {
			messages[index].queuedAnnotations.Release()
			messages[index].queuedAnnotations = nil
		}
	}
	loaded.mu.Lock()
	loaded.followUps = nil
	queue := projectFollowUpQueue(loaded.followUps)
	loaded.mu.Unlock()
	return FollowUpRestore{Messages: messages, Queue: queue}, nil
}

// PromoteFollowUps atomically removes deferred prompts as droid steering accepts them.
func (m *Manager) PromoteFollowUps(ctx context.Context, sessionID string) (FollowUpPromotion, error) {
	if err := m.beginOperation(); err != nil {
		return FollowUpPromotion{}, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return FollowUpPromotion{}, err
	}
	loaded.transitionMu.Lock()
	defer loaded.transitionMu.Unlock()
	if err := m.validateQueueRuntime(sessionID, loaded); err != nil {
		return FollowUpPromotion{}, err
	}
	loaded.mu.Lock()
	if loaded.activeRun == "" {
		loaded.mu.Unlock()
		return FollowUpPromotion{}, ErrBusy
	}
	loaded.mu.Unlock()
	promoted := 0
	for {
		loaded.mu.Lock()
		if len(loaded.followUps) == 0 {
			queue := projectFollowUpQueue(loaded.followUps)
			loaded.mu.Unlock()
			return FollowUpPromotion{Promoted: promoted, Queue: queue}, nil
		}
		prompt := loaded.followUps[0]
		loaded.mu.Unlock()
		prepared, err := m.prepareAnnotations(ctx, loaded, sessionID, prompt)
		var annotationRecords []kitannotation.Record
		if prepared != nil {
			annotationRecords = prepared.Records
		}
		var content []droids.InputContent
		annotationSubmissionID := ""
		if err == nil && prepared != nil {
			annotationSubmissionID, err = identifier.New("annotation_submission_")
		}
		if err == nil {
			content, err = m.resolvePromptContent(ctx, sessionID, loaded.model, prompt, annotationRecords, annotationSubmissionID)
		}
		if err == nil && prepared != nil {
			err = prepared.Reserve(ctx, annotationSubmissionID)
		}
		promptAccepted := false
		if err == nil {
			_, err = loaded.droid.Prompt(ctx, droids.Input{Content: content}, droids.PromptOptions{Steer: true})
			promptAccepted = err == nil
		}
		if promptAccepted && prepared != nil {
			acceptedMessageID, identityErr := loaded.droid.AnnotationSubmissionMessageID(context.Background(), annotationSubmissionID)
			_ = prepared.Commit()
			if identityErr != nil {
				err = identityErr
			} else {
				m.AnnotationsSubmitted(sessionID, string(acceptedMessageID), prompt.AnnotationIDs)
			}
			prepared.Release()
		} else if prepared != nil {
			prepared.Abort()
		}
		if err != nil {
			loaded.mu.Lock()
			if promptAccepted {
				loaded.followUps = loaded.followUps[1:]
				promoted++
			}
			queue := projectFollowUpQueue(loaded.followUps)
			loaded.mu.Unlock()
			return FollowUpPromotion{Promoted: promoted, Queue: queue}, err
		}
		loaded.mu.Lock()
		loaded.followUps = loaded.followUps[1:]
		loaded.mu.Unlock()
		promoted++
	}
}

func validatePromptInput(input PromptInput) error {
	if strings.TrimSpace(input.Text) == "" && len(input.AttachmentIDs) == 0 && len(input.AnnotationIDs) == 0 {
		return fmt.Errorf("%w: prompt must include text, an attachment, or an annotation", ErrInvalidInput)
	}
	if input.Text != "" {
		if err := validatePromptText(input.Text); err != nil {
			return err
		}
	}
	if len(input.AttachmentIDs) > maxPromptAttachments {
		return fmt.Errorf("%w: prompt has too many attachments", ErrInvalidInput)
	}
	seen := make(map[string]struct{}, len(input.AttachmentIDs))
	for _, id := range input.AttachmentIDs {
		if !identifier.Valid(id, attachment.IDPrefix) {
			return fmt.Errorf("%w: invalid attachment id", ErrInvalidInput)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("%w: duplicate attachment id", ErrInvalidInput)
		}
		seen[id] = struct{}{}
	}
	if len(input.AnnotationIDs) > 64 {
		return fmt.Errorf("%w: prompt has too many annotations", ErrInvalidInput)
	}
	seenAnnotations := make(map[uint64]struct{}, len(input.AnnotationIDs))
	for _, id := range input.AnnotationIDs {
		if id == 0 {
			return fmt.Errorf("%w: invalid annotation id", ErrInvalidInput)
		}
		if _, duplicate := seenAnnotations[id]; duplicate {
			return fmt.Errorf("%w: duplicate annotation id", ErrInvalidInput)
		}
		seenAnnotations[id] = struct{}{}
	}
	return nil
}

func clonePromptInputs(inputs []PromptInput) []PromptInput {
	cloned := make([]PromptInput, len(inputs))
	for index, input := range inputs {
		cloned[index] = PromptInput{queuedAnnotations: input.queuedAnnotations, fromQueue: input.fromQueue, Text: input.Text, AttachmentIDs: append([]string(nil), input.AttachmentIDs...), AnnotationIDs: append([]uint64(nil), input.AnnotationIDs...)}
	}
	return cloned
}

func (m *Manager) prepareAnnotations(ctx context.Context, loaded *runtime, sessionID string, input PromptInput) (*kitannotation.PreparedSubmission, error) {
	if len(input.AnnotationIDs) == 0 {
		return nil, nil
	}
	if m.annotations == nil {
		return nil, fmt.Errorf("%w: annotations are unavailable", ErrInvalidInput)
	}
	if err := m.annotations.RecoverSubmissions(ctx, sessionID, loaded.droid.HasAnnotationSubmission); err != nil {
		return nil, err
	}
	if input.queuedAnnotations != nil {
		return input.queuedAnnotations.Prepare(ctx)
	}
	return m.annotations.PrepareSubmission(ctx, sessionID, loaded.workspace.CWD(), input.AnnotationIDs)
}

func (m *Manager) resolvePromptContent(ctx context.Context, sessionID string, model droids.Model, input PromptInput, annotations []kitannotation.Record, annotationSubmissionID string) ([]droids.InputContent, error) {
	content := make([]droids.InputContent, 0, len(input.AttachmentIDs)+2)
	if strings.TrimSpace(input.Text) != "" {
		content = append(content, droids.TextInput{Text: input.Text})
	}
	if len(annotations) > 0 {
		projection, err := kitannotation.ModelProjection(annotations)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		snapshots := make([]droids.SubmittedAnnotation, 0, len(annotations))
		for _, record := range annotations {
			snapshot := droids.SubmittedAnnotation{
				ID: record.ID, Kind: string(record.Anchor.Kind),
				Preview: record.Preview.Text, Truncated: record.Preview.Truncated, Body: record.Body,
			}
			if anchor := record.Anchor.WorkspaceFile; anchor != nil {
				snapshot.WorkspaceID, snapshot.Path, snapshot.FileRevision = anchor.WorkspaceID, anchor.Path, anchor.FileRevision
				snapshot.StartLine, snapshot.EndLine = anchor.StartLine, anchor.EndLine
			} else if anchor := record.Anchor.WorkingTreeDiff; anchor != nil {
				snapshot.TargetID, snapshot.TargetRevision = anchor.TargetID, anchor.TargetRevision
				snapshot.Path, snapshot.FileRevision, snapshot.Side = anchor.Path, anchor.FileRevision, anchor.Side
				snapshot.StartLine, snapshot.EndLine = anchor.StartLine, anchor.EndLine
				if target := record.DiffTarget; target != nil {
					snapshot.TargetWorkspaceID, snapshot.TargetKind = target.WorkspaceID, target.Kind
					snapshot.TargetBaseKind, snapshot.TargetBaseOID = target.Base.Kind, target.Base.OID
					snapshot.TargetHeadKind, snapshot.TargetHeadOID = target.Head.Kind, target.Head.OID
				}
			}
			snapshots = append(snapshots, snapshot)
		}
		content = append(content, droids.AnnotationInput{SubmissionID: annotationSubmissionID, Text: projection, Annotations: snapshots})
	}
	if len(input.AttachmentIDs) == 0 {
		return content, nil
	}
	if m.attachments == nil {
		return nil, fmt.Errorf("%w: attachments are unavailable", ErrInvalidInput)
	}
	var totalBytes, textBytes int64
	for _, id := range input.AttachmentIDs {
		record, reader, err := m.attachments.Open(ctx, sessionID, id)
		if err != nil {
			if errors.Is(err, attachment.ErrNotFound) || errors.Is(err, attachment.ErrInvalidInput) {
				return nil, fmt.Errorf("%w: attachment %q is unavailable", ErrInvalidInput, id)
			}
			return nil, fmt.Errorf("resolve attachment %q: %w", id, err)
		}
		totalBytes += record.Size
		if record.MediaType == "text/plain" {
			textBytes += record.Size
		} else if !ModelSupportsImageAttachment(model) {
			_ = reader.Close()
			return nil, fmt.Errorf("%w: model %q does not support image attachments", ErrInvalidInput, model.ID)
		}
		if totalBytes > maxPromptAttachmentBytes || textBytes > maxPromptTextAttachmentBytes {
			_ = reader.Close()
			return nil, fmt.Errorf("%w: prompt attachment size limit exceeded", ErrInvalidInput)
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, record.Size+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read attachment %q: %w", id, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close attachment %q: %w", id, closeErr)
		}
		if record.MediaType == "text/plain" {
			content = append(content, droids.TextInput{
				Text:         fmt.Sprintf("\n\n--- attachment: %q ---\n%s\n--- end attachment ---", record.Filename, data),
				AttachmentID: record.ID, Filename: record.Filename, MediaType: record.MediaType,
			})
		} else {
			file := droids.NewFileInputData(record.Filename, record.MediaType, data)
			file.AttachmentID = record.ID
			content = append(content, file)
		}
	}
	return content, nil
}

// ModelSupportsImageAttachment reports whether Kit can send image content to a model.
func ModelSupportsImageAttachment(model droids.Model) bool {
	if model.API == droids.ModelAPIAnthropicMessages {
		return false
	}
	for _, supported := range model.Input {
		if supported == "image" {
			return true
		}
	}
	return false
}

func validatePromptText(prompt string) error {
	if strings.TrimSpace(prompt) == "" || len(prompt) > maxPromptTextBytes || !utf8.ValidString(prompt) || strings.IndexByte(prompt, 0) >= 0 {
		return fmt.Errorf("%w: prompt must be non-empty valid UTF-8 without NUL and at most 128 KiB", ErrInvalidInput)
	}
	return nil
}

func projectFollowUpQueue(messages []PromptInput) FollowUpQueue {
	queue := FollowUpQueue{Count: len(messages), Previews: make([]string, 0, len(messages))}
	seenAnnotations := make(map[uint64]bool)
	for _, message := range messages {
		for _, id := range message.AnnotationIDs {
			if !seenAnnotations[id] {
				queue.AnnotationIDs = append(queue.AnnotationIDs, id)
				seenAnnotations[id] = true
			}
		}
		preview := strings.Join(strings.Fields(message.Text), " ")
		if preview == "" && len(message.AttachmentIDs) > 0 {
			preview = "Attachment"
		}
		if preview == "" && len(message.AnnotationIDs) > 0 {
			preview = "Annotation"
		}
		runes := []rune(preview)
		if len(runes) > 160 {
			preview = string(runes[:159]) + "…"
		}
		queue.Previews = append(queue.Previews, preview)
	}
	return queue
}

// StartPrompt preserves the text-only caller contract.
func (m *Manager) StartPrompt(ctx context.Context, sessionID, prompt string) (RunReservation, error) {
	return m.StartPromptInput(ctx, sessionID, PromptInput{Text: prompt})
}

// StartPromptInput admits one structured droid turn and returns its canonical identity.
func (m *Manager) StartPromptInput(ctx context.Context, sessionID string, prompt PromptInput) (RunReservation, error) {
	return m.startPrompt(ctx, sessionID, prompt, "", "")
}

// StartPromptCommand expands and admits one command from the runtime's immutable snapshot.
func (m *Manager) StartPromptCommand(ctx context.Context, sessionID, name, args string) (RunReservation, error) {
	if strings.TrimSpace(name) != name || name == "" || len(name) > 128 || len(args) > maxPromptTextBytes || !utf8.ValidString(args) || strings.IndexByte(args, 0) >= 0 {
		return RunReservation{}, fmt.Errorf("%w: prompt command name or arguments are invalid", ErrInvalidInput)
	}
	return m.startPrompt(ctx, sessionID, PromptInput{}, name, args)
}

func (m *Manager) startPrompt(ctx context.Context, sessionID string, input PromptInput, commandName, commandArgs string) (RunReservation, error) {
	if err := m.beginAdmission(); err != nil {
		return RunReservation{}, err
	}
	defer m.admissions.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if commandName == "" {
		if err := validatePromptInput(input); err != nil {
			return RunReservation{}, err
		}
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
	if len(loaded.followUps) > 0 && !input.fromQueue {
		release()
		return RunReservation{}, fmt.Errorf("%w: restore pending follow-ups before starting a new prompt", ErrBusy)
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
		expanded, err := command.Expand(commandArgs)
		if err != nil {
			release()
			return RunReservation{}, fmt.Errorf("%w: expand prompt command %q: %v", ErrInvalidInput, commandName, err)
		}
		input = PromptInput{Text: expanded}
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
	prepared, err := m.prepareAnnotations(ctx, loaded, sessionID, input)
	if err != nil {
		release()
		return RunReservation{}, err
	}
	var annotationRecords []kitannotation.Record
	if prepared != nil {
		annotationRecords = prepared.Records
	}
	annotationSubmissionID := ""
	if prepared != nil {
		annotationSubmissionID, err = identifier.New("annotation_submission_")
		if err != nil {
			prepared.Abort()
			release()
			return RunReservation{}, err
		}
	}
	content, err := m.resolvePromptContent(ctx, sessionID, loaded.model, input, annotationRecords, annotationSubmissionID)
	if err != nil {
		if prepared != nil {
			prepared.Abort()
		}
		release()
		return RunReservation{}, err
	}
	subscription, err := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{
		After: loaded.eventCursor, IncludeTransient: true, Buffer: 256,
	})
	if err != nil {
		if prepared != nil {
			prepared.Abort()
		}
		release()
		return RunReservation{}, err
	}
	if err := m.touchSessionActivity(ctx, sessionID, time.Now().UTC()); err != nil {
		subscription.Close()
		if prepared != nil {
			prepared.Abort()
		}
		release()
		return RunReservation{}, err
	}
	pendingMailbox, err := m.informPendingSubagentMailbox(ctx, sessionID, loaded.droid)
	if err != nil {
		subscription.Close()
		if prepared != nil {
			prepared.Abort()
		}
		release()
		return RunReservation{}, err
	}
	if prepared != nil {
		if err := prepared.Reserve(ctx, annotationSubmissionID); err != nil {
			subscription.Close()
			prepared.Abort()
			release()
			return RunReservation{}, err
		}
	}
	handle, err := loaded.droid.Prompt(ctx, droids.Input{Content: content}, droids.PromptOptions{})
	if err != nil {
		subscription.Close()
		if prepared != nil {
			prepared.Abort()
		}
		release()
		if errors.Is(err, droids.ErrBusy) {
			return RunReservation{}, ErrBusy
		}
		return RunReservation{}, err
	}
	acceptedAnnotationMessageID := ""
	if prepared != nil {
		messageID, identityErr := loaded.droid.AnnotationSubmissionMessageID(context.Background(), annotationSubmissionID)
		_ = prepared.Commit()
		defer prepared.Release()
		if identityErr != nil {
			subscription.Close()
			abortAbandonedPluginTurn(loaded, string(handle.TurnID()))
			release()
			return RunReservation{}, identityErr
		}
		acceptedAnnotationMessageID = string(messageID)
	}
	turnID := string(handle.TurnID())
	mailboxErr := m.acknowledgeConsumedSubagentMailbox(ctx, loaded.droid, turnID, pendingMailbox)
	livePromptText := input.Text
	if strings.TrimSpace(livePromptText) == "" {
		if len(input.AnnotationIDs) > 0 {
			livePromptText = "Annotations"
		} else {
			livePromptText = "Attachment"
		}
	}
	initialEvents := []NewEvent{
		{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventRunStarted, Status: RunStatusRunning},
		{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventUserMessage, Text: boundedLiveText(livePromptText)},
	}
	if len(input.AnnotationIDs) > 0 {
		initialEvents = append(initialEvents, NewEvent{
			SessionID: sessionID, Kind: EventAnnotationSubmitted,
			AnnotationIDs: append([]uint64(nil), input.AnnotationIDs...), AcceptedMessageID: acceptedAnnotationMessageID,
		})
	}
	reservation, err := m.launchAdmittedRunLocked(loaded, sessionID, handle, subscription, false, nil, initialEvents)
	if err == nil {
		m.mu.Lock()
		delete(m.mailboxBlocked, sessionID)
		m.mu.Unlock()
		if mailboxErr != nil {
			m.wakeSubagentMailbox(sessionID)
		}
	}
	return reservation, err
}

func (m *Manager) launchAdmittedRunLocked(loaded *runtime, sessionID string, handle droids.ExecutionHandle, subscription droids.Subscription, autonomous bool, peerRequest *peer.Request, initialEvents []NewEvent) (RunReservation, error) {
	release := func() {
		loaded.mu.Unlock()
		loaded.admissionMu.Unlock()
	}
	turnID := string(handle.TurnID())
	if err := loaded.events.reset(); err != nil {
		subscription.Close()
		abortAbandonedPluginTurn(loaded, turnID)
		release()
		return RunReservation{}, err
	}
	run := &liveRun{record: RunProjection{
		ID: turnID, SessionID: sessionID, TurnID: turnID, Status: RunStatusRunning,
	}, done: make(chan struct{}), completeStream: true, autonomous: autonomous, peerRequest: peerRequest}
	pruneRuns(loaded.runs, 128)
	loaded.activeRun = turnID
	loaded.runs[turnID] = run
	if err := loaded.events.append(initialEvents); err != nil {
		subscription.Close()
		abortAbandonedPluginTurn(loaded, turnID)
		release()
		return RunReservation{}, err
	}
	loaded.mu.Unlock()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		subscription.Close()
		abortAbandonedPluginTurn(loaded, turnID)
		loaded.admissionMu.Unlock()
		return RunReservation{}, ErrClosed
	}
	m.runs.Add(1)
	m.mu.Unlock()
	go m.executePrompt(loaded, run, handle, subscription, sessionID)
	return RunReservation{SessionID: sessionID, TurnID: turnID, RunID: turnID}, nil
}

func abortAbandonedPluginTurn(loaded *runtime, _ string) {
	_ = loaded.droid.Abort(context.Background())
}

func (m *Manager) RunPrompt(ctx context.Context, sessionID, prompt string) (PromptResult, error) {
	return m.RunPromptInput(ctx, sessionID, PromptInput{Text: prompt})
}

func (m *Manager) RunPromptInput(ctx context.Context, sessionID string, prompt PromptInput) (PromptResult, error) {
	reservation, err := m.StartPromptInput(ctx, sessionID, prompt)
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
	if run.peerRequest != nil {
		m.completePeerRun(*run.peerRequest, result)
	}

	loaded.transitionMu.Lock()
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
		loaded.signalEventChangedLocked()
	}
	loaded.mu.Unlock()
	if loaded.turnEvents != nil {
		loaded.turnEvents.waitCompleted(turnID)
	}
	loaded.admissionMu.Unlock()
	m.startQueuedFollowUps(loaded, sessionID)
	m.scheduleAutoName(sessionID)
	loaded.mu.Lock()
	close(run.done)
	loaded.mu.Unlock()
	loaded.transitionMu.Unlock()
}

func (m *Manager) startQueuedFollowUps(loaded *runtime, sessionID string) {
	loaded.mu.Lock()
	if len(loaded.followUps) == 0 {
		loaded.mu.Unlock()
		return
	}
	prompt := loaded.followUps[0]
	prompt.fromQueue = true
	loaded.mu.Unlock()
	if _, err := m.StartPromptInput(context.Background(), sessionID, prompt); err != nil {
		return
	}
	loaded.mu.Lock()
	loaded.followUps = loaded.followUps[1:]
	loaded.mu.Unlock()
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
		loaded.mu.Lock()
		var appendErr error
		if envelope.TurnID == droids.TurnID(turnID) {
			appendErr = loaded.events.append(projectDroidEvent(sessionID, turnID, runID, envelope.Event))
		}
		if envelope.Durable && envelope.Sequence > loaded.eventCursor {
			loaded.eventCursor = envelope.Sequence
			loaded.signalEventChangedLocked()
		}
		loaded.mu.Unlock()
		if appendErr != nil {
			return appendErr
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
		m.cancelMailbox()
		m.cancelPlugins()
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
		shutdownErr = errors.Join(shutdownErr, loaded.close(context.Background(), "shutdown"))
		loaded.mu.Unlock()
	}
	m.admissions.Wait()
	m.loads.Wait()
	m.runs.Wait()
	m.bashRuns.Wait()
	m.ops.Wait()
	m.cleanups.Wait()
	for _, loaded := range runtimes {
		loaded.transitionMu.Lock()
		loaded.mu.Lock()
		queued := loaded.followUps
		loaded.followUps = nil
		loaded.mu.Unlock()
		for _, prompt := range queued {
			if prompt.queuedAnnotations != nil {
				prompt.queuedAnnotations.Release()
			}
		}
		loaded.transitionMu.Unlock()
	}
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
			_ = loaded.close(context.Background(), "unavailable")
		}
		if m.closed {
			loaded, err = nil, ErrClosed
		} else {
			loaded, err = nil, ErrDeleteBusy
		}
	} else if err == nil {
		m.runtimes[sessionID] = loaded
		if pending.pluginName != nil && loaded.plugins != nil {
			loaded.plugins.Rename(*pending.pluginName)
		}
		startRecovery = loaded.recovery != nil
		if startRecovery {
			loaded.admissionMu.Lock()
			m.runs.Add(1)
		}
	}
	pending.runtime, pending.err = loaded, err
	close(pending.done)
	m.mu.Unlock()
	if err == nil && loaded.plugins != nil {
		loaded.plugins.Start()
	}
	if err == nil {
		m.scheduleAutoName(sessionID)
	}
	if startRecovery {
		go m.resumeRuntimeWithLimits(loaded, sessionID)
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
	record, warnings, err := m.normalizeSessionThinking(ctx, record)
	if err != nil {
		return nil, err
	}
	loaded, err := m.newDroid(ctx, record)
	if err != nil {
		return nil, err
	}
	loaded.configurationWarnings = append([]string(nil), warnings...)
	return loaded, nil
}

func (m *Manager) sessionRecord(ctx context.Context, sessionID string) (SessionRecord, error) {
	m.mu.Lock()
	record, temporary := m.temporary[sessionID]
	m.mu.Unlock()
	if temporary {
		return record, nil
	}
	record, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	if record.ParentSessionID != "" {
		if parent, lookupErr := m.store.GetSession(ctx, record.ParentSessionID); lookupErr == nil {
			record.ParentSessionName = parent.Name
		}
	}
	return record, nil
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
	var loaded *runtime
	interactions := newInteractionBroker(func(event NewEvent) {
		if loaded == nil {
			return
		}
		if err := loaded.events.append([]NewEvent{event}); err != nil {
			loaded.events.invalidate()
		}
	})
	bundle.Prompt.Prompt += interactionPromptGuidance
	bundle.Tools = append(bundle.Tools, interactionTools(record.ID, interactions)...)
	bundle.Tools = append(bundle.Tools, m.boundSessionTools(record, workspace)...)
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
	var interception *pluginInterceptorBridge
	var turnEvents *pluginTurnEventBridge
	if m.pluginFactory != nil {
		identity, err := identifier.New("interception_")
		if err != nil {
			_ = closeStore()
			return nil, err
		}
		interception = &pluginInterceptorBridge{bootstrap: identity}
		turnEvents = newPluginTurnEventBridge(m.pluginContext)
	}
	droid, snapshot, model, err := m.openDroid(ctx, record, store, bundle, interception, turnEvents)
	if err != nil {
		if turnEvents != nil {
			closePluginTurnEventBridge(turnEvents)
		}
		_ = closeStore()
		return nil, fmt.Errorf("open runtime for session %q: %w", record.ID, err)
	}
	if record.Persistent && record.DroidInitializedAt == nil {
		if err := m.store.MarkDroidInitialized(ctx, record.ID, time.Now().UTC()); err != nil {
			if turnEvents != nil {
				closePluginTurnEventBridge(turnEvents)
			}
			_ = droid.Close()
			_ = closeStore()
			return nil, err
		}
	}
	events, err := newEventLog()
	if err != nil {
		if turnEvents != nil {
			closePluginTurnEventBridge(turnEvents)
		}
		_ = droid.Close()
		_ = closeStore()
		return nil, err
	}
	if turnEvents != nil {
		turnEvents.droid.Store(droid)
	}
	loaded = &runtime{
		cleanupOwner: &m.cleanups,
		interception: interception, turnEvents: turnEvents, droid: droid, model: model, store: store, closeStore: closeStore, bundle: cloneRuntimeBundle(bundle),
		workspace: workspace, events: events, eventCursor: snapshot.LastEvent, eventChanged: make(chan struct{}),
		scratchpadOwnerID: record.ScratchpadOwnerID,
		runs:              make(map[string]*liveRun), interactions: interactions,
	}
	loaded.pluginContributions.initialize(bundle.Subagents, droid)
	interactions.authority = &loaded.mu
	quiescent, err := droid.WaitQuiescent(ctx)
	if err != nil {
		_ = loaded.close(context.Background(), "unavailable")
		return nil, err
	}
	if quiescent.Execution != nil && (quiescent.Execution.Status == droids.ExecutionPaused || quiescent.Execution.Status == droids.ExecutionInterrupted) {
		turnID := string(quiescent.TurnID)
		run := &liveRun{record: RunProjection{ID: turnID, SessionID: record.ID, TurnID: turnID, Status: RunStatusRunning}, done: make(chan struct{}), autonomous: quiescent.Execution.BoundaryReaction}
		loaded.activeRun = turnID
		loaded.runs[turnID] = run
		copy := *quiescent.Execution
		loaded.recovery = &copy
		_ = loaded.events.append([]NewEvent{{SessionID: record.ID, TurnID: turnID, RunID: turnID, Kind: EventRunStarted, Status: RunStatusRunning}})
		loaded.events.invalidate()
	}
	if m.pluginFactory != nil {
		loaded.plugins = m.pluginFactory(m.pluginContext, PluginSession{ID: record.ID, Name: record.Name, CWD: record.CWD, SubagentNames: pluginSubagentNames(bundle.Subagents)}, func() { loaded.refreshPluginContributions(); loaded.events.invalidate() })
		if loaded.plugins != nil {
			turnEvents.binding.Store(&pluginTurnEventBinding{host: loaded.plugins})
		}
		var policy PluginInterceptorHost = emptyPluginInterception{identity: interception.bootstrap}
		if host, ok := loaded.plugins.(PluginInterceptorHost); ok {
			policy = host
		}
		interception.binding.Store(&pluginInterceptorBinding{host: policy})
		if host, ok := loaded.plugins.(PluginToastHost); ok {
			host.SetToastObserver(loaded.pluginNotifications.publish)
		}
		if host, ok := loaded.plugins.(PluginInteractionHost); ok {
			host.SetInteractionObserver(func(ctx context.Context, input PluginInteractionInput, available func() bool) (PluginInteractionResult, error) {
				return loaded.interactions.requestPlugin(ctx, record.ID, input, available)
			})
		}
	}
	if m.pluginSubagents != nil && loaded.plugins != nil {
		cleanup, err := m.pluginSubagents.RegisterPluginCatalogProvider(record.ID, loaded.appliedPluginSubagentCatalog)
		if err != nil {
			_ = loaded.close(context.Background(), "unavailable")
			return nil, fmt.Errorf("register plugin subagent catalog: %w", err)
		}
		loaded.closePluginSubagentCatalog = cleanup
	}
	return loaded, nil
}

func (m *Manager) applyModelContextWindow(selector string, model droids.Model) droids.Model {
	if m.modelContextWindow != nil {
		if contextWindow := m.modelContextWindow(selector); contextWindow > 0 {
			return model.WithContextWindow(contextWindow)
		}
	}
	return model
}

func (m *Manager) openDroid(ctx context.Context, record SessionRecord, store droids.Store, bundle RuntimeBundle, interception *pluginInterceptorBridge, turnEvents *pluginTurnEventBridge) (*droids.Droid, droids.Snapshot, droids.Model, error) {
	selector := record.ModelProvider + "/" + record.ModelID
	model, err := m.resolveExactModel(selector)
	if err != nil {
		return nil, droids.Snapshot{}, droids.Model{}, err
	}
	if canonical := model.Provider + "/" + model.ID; canonical != selector {
		return nil, droids.Snapshot{}, droids.Model{}, fmt.Errorf("resolved model %q as %q", selector, canonical)
	}
	model = m.applyModelContextWindow(selector, model)
	config := droids.Config{
		Store: store, Model: model,
		Reasoning: record.ThinkingLevel, SystemPrompt: bundle.Prompt.Prompt,
		Tools: bundle.Tools,
	}
	if turnEvents != nil {
		config.TurnStarted = turnEvents.started
		config.TurnSettled = turnEvents.turnSettled
	}
	if interception != nil {
		config.BeforeToolCall = interception.before
		config.BeforeToolCallIdentity = interception.identity
	}
	droid, err := droids.Spawn(ctx, droids.ConversationID(record.ID), config)
	if err != nil {
		return nil, droids.Snapshot{}, droids.Model{}, err
	}
	snapshot, err := droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		_ = droid.Close()
		return nil, droids.Snapshot{}, droids.Model{}, err
	}
	return droid, snapshot, model, nil
}

func (m *Manager) resumeRuntimeWithLimits(loaded *runtime, sessionID string) {
	acquiredReactionSlot := false
	if loaded.recovery != nil && loaded.recovery.BoundaryReaction {
		select {
		case m.mailboxSlots <- struct{}{}:
			acquiredReactionSlot = true
		case <-m.mailboxContext.Done():
		}
	}
	m.resumeRuntime(loaded, sessionID)
	if acquiredReactionSlot {
		<-m.mailboxSlots
	}
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
		loaded.signalEventChangedLocked()
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
	loaded.signalEventChangedLocked()
	close(run.done)
	loaded.mu.Unlock()
	loaded.admissionMu.Unlock()
}

func (r *runtime) close(ctx context.Context, interactionReason string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := r.startClose(interactionReason)
	select {
	case <-done:
		r.closeMu.Lock()
		err := r.closeErr
		r.closeMu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startClose establishes cleanup ownership synchronously but never waits. It is
// safe for quarantine paths that still hold runtime transition locks.
func (r *runtime) startClose(interactionReason string) <-chan struct{} {
	r.closeMu.Lock()
	defer r.closeMu.Unlock()
	if !r.closeStarted {
		r.closeStarted = true
		r.closeDone = make(chan struct{})
		if r.cleanupOwner != nil {
			r.cleanupOwner.Add(1)
		}
		go r.finishClose(interactionReason)
	}
	return r.closeDone
}

func (r *runtime) finishClose(interactionReason string) {
	defer func() {
		if r.cleanupOwner != nil {
			r.cleanupOwner.Done()
		}
	}()
	var cleanupErr error
	if r.turnEvents != nil {
		cleanupErr = r.turnEvents.close(context.Background())
	}
	if r.interactions != nil {
		r.interactions.cancelAll(interactionReason)
	}
	if r.plugins != nil {
		cleanupErr = errors.Join(cleanupErr, r.plugins.Close(context.Background()))
	}
	// Close MCP after plugins, so plugin-driven tool calls have already stopped,
	// and before the droid shuts down, so in-flight namespace calls are cancelled
	// while the loop that owns them still exists.
	if r.bundle.MCP != nil {
		ctx, cancel := context.WithTimeout(context.Background(), mcpCloseDeadline)
		cleanupErr = errors.Join(cleanupErr, mcpruntime.CloseManager(ctx, r.bundle.MCP))
		cancel()
	}
	if r.closePluginSubagentCatalog != nil {
		r.closePluginSubagentCatalog()
	}
	// Keep notification subscribers available through plugin cleanup so a final
	// shutdown failure can still reach attached clients. No plugin callback can
	// publish after Close returns.
	r.pluginNotifications.close()
	cleanupErr = errors.Join(cleanupErr, r.droid.Shutdown(context.Background()), r.closeStore())
	r.closeMu.Lock()
	r.closeErr = cleanupErr
	close(r.closeDone)
	r.closeMu.Unlock()
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
