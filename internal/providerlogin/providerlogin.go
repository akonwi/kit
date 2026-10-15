// Package providerlogin runs a server's interactive provider logins. A login
// is a short-lived, server-owned process: starting one reports instructions
// for the user, and the login then completes on the server, which stores the
// resulting credential. Clients follow a login by its ID.
package providerlogin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/akonwi/kit/internal/identifier"
)

// Method is a way to log in to a provider.
type Method string

// Login methods.
const (
	// MethodDevice shows a verification URL and code to enter there.
	MethodDevice Method = "device"
	// MethodBrowser opens an authorization page whose redirect, or the code
	// it shows, is submitted to the login.
	MethodBrowser Method = "browser"
	// MethodAPIKey saves an API key; it is not an interactive login.
	MethodAPIKey Method = "apiKey"
)

// Provider is a model provider the server can hold credentials for.
type Provider struct {
	ID      string
	Name    string
	Methods []Method
}

// Supports reports whether the provider can be logged in to with method.
func (p Provider) Supports(method Method) bool {
	for _, supported := range p.Methods {
		if supported == method {
			return true
		}
	}
	return false
}

// Providers are the providers a server knows, in display order.
var Providers = []Provider{
	{ID: "openai-codex", Name: "OpenAI Codex", Methods: []Method{MethodDevice}},
	{ID: "anthropic", Name: "Anthropic", Methods: []Method{MethodBrowser, MethodAPIKey}},
	{ID: "openai", Name: "OpenAI", Methods: []Method{MethodAPIKey}},
	{ID: "opencode-go", Name: "OpenCode Go", Methods: []Method{MethodAPIKey}},
}

// Find returns the provider with id.
func Find(id string) (Provider, bool) {
	for _, provider := range Providers {
		if provider.ID == id {
			return provider, true
		}
	}
	return Provider{}, false
}

// State is a login's progress. Every state but StatePending is final.
type State string

// Login states.
const (
	StatePending    State = "pending"
	StateSucceeded  State = "succeeded"
	StateFailed     State = "failed"
	StateExpired    State = "expired"
	StateCancelled  State = "cancelled"
	StateSuperseded State = "superseded"
)

// DeviceInstructions tell the user where to enter a device code.
type DeviceInstructions struct {
	VerificationURI string
	UserCode        string
	ExpiresAt       time.Time
}

// BrowserInstructions tell the user which page authorizes the login. The
// page redirects to RedirectURI on the machine running the browser.
type BrowserInstructions struct {
	AuthorizationURL string
	RedirectURI      string
}

// Snapshot is a login's state at one moment.
type Snapshot struct {
	ID       string
	Provider string
	Method   Method
	State    State
	// Device or Browser holds the instructions for the login's method while
	// it is pending; they are dropped once it finishes.
	Device  *DeviceInstructions
	Browser *BrowserInstructions
	// Failure explains a failed login in display-safe text.
	Failure string
	// ExpiresAt is when a pending login expires.
	ExpiresAt time.Time
}

// Flows run the interactive login methods. Each reports its instructions with
// notify, then completes the login, storing its credential, or fails.
type Flows struct {
	Device  func(ctx context.Context, provider string, notify func(DeviceInstructions)) error
	Browser func(ctx context.Context, provider string, authorization <-chan string, notify func(BrowserInstructions)) error
}

// Errors returned by Registry operations.
var (
	ErrUnknownProvider   = errors.New("unknown provider")
	ErrUnsupportedMethod = errors.New("the provider does not support this login method")
	ErrNotFound          = errors.New("login not found")
	ErrCapacity          = errors.New("too many logins in progress")
	ErrNotPending        = errors.New("the login is no longer pending")
	ErrNotBrowser        = errors.New("only browser logins accept an authorization")
	ErrSubmitted         = errors.New("the login already received an authorization")
)

const (
	// MaxLogins bounds the logins a registry tracks, pending and finished.
	MaxLogins = 16
	// Lifetime bounds a login from its start.
	Lifetime = 15 * time.Minute
	// MaxWait bounds how long one Wait call waits.
	MaxWait = 25 * time.Second
	// MaxAuthorizationBytes bounds a submitted authorization.
	MaxAuthorizationBytes = 4 << 10
	// maxFailureBytes bounds a failure message.
	maxFailureBytes = 512
	// startTimeout bounds how long Start waits for a flow's instructions.
	startTimeout = 30 * time.Second
	// retainFinished is how long a finished login stays readable, so a
	// client that was waiting can read how it ended.
	retainFinished = 5 * time.Minute
)

// Registry tracks a server's logins. At most one login per provider is
// pending; starting another supersedes it.
type Registry struct {
	flows    Flows
	now      func() time.Time
	lifetime time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	// flows counts running flow goroutines, which Close waits for.
	flowsRunning sync.WaitGroup

	mu     sync.Mutex
	closed bool
	logins map[string]*login
}

type login struct {
	snapshot Snapshot
	finished time.Time
	// ready closes once instructions are known or the login finished.
	ready chan struct{}
	// started is the login as it stood when its instructions became known,
	// which Start returns even if the login has finished since.
	started   *Snapshot
	readyOnce sync.Once
	// done closes when the login finishes.
	done          chan struct{}
	cancel        context.CancelFunc
	authorization chan string
	// submitted records that the login received its one authorization.
	submitted bool
}

// NewRegistry returns a registry running flows. now defaults to time.Now.
func NewRegistry(flows Flows, now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Registry{flows: flows, now: now, lifetime: Lifetime, ctx: ctx, cancel: cancel, logins: map[string]*login{}}
}

// ErrClosed reports a registry that has stopped accepting logins.
var ErrClosed = errors.New("the server is stopping")

// Close fails every pending login, so their waiters return, and waits for
// their flows to end. Further logins fail with ErrClosed. Close is safe to
// call more than once.
func (r *Registry) Close() {
	r.mu.Lock()
	r.closed = true
	for _, entry := range r.logins {
		r.finishLocked(entry, StateFailed, "the server stopped")
	}
	r.mu.Unlock()
	r.cancel()
	r.flowsRunning.Wait()
}

// Start begins a login and returns it once its instructions are known, as it
// stood then, so the caller can show them even if the login has finished
// since. A login that finishes before reporting instructions, for example
// because the provider could not be reached, is returned finished.
func (r *Registry) Start(ctx context.Context, providerID string, method Method) (Snapshot, error) {
	provider, ok := Find(providerID)
	if !ok {
		return Snapshot{}, ErrUnknownProvider
	}
	if !provider.Supports(method) || (method == MethodDevice && r.flows.Device == nil) ||
		(method == MethodBrowser && r.flows.Browser == nil) || method == MethodAPIKey {
		return Snapshot{}, ErrUnsupportedMethod
	}
	id, err := identifier.New("login_")
	if err != nil {
		return Snapshot{}, err
	}
	now := r.now()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return Snapshot{}, ErrClosed
	}
	r.purgeLocked(now)
	for _, existing := range r.logins {
		if existing.snapshot.Provider == providerID && existing.snapshot.State == StatePending {
			r.finishLocked(existing, StateSuperseded, "")
		}
	}
	if len(r.logins) >= MaxLogins && !r.evictLocked() {
		r.mu.Unlock()
		return Snapshot{}, ErrCapacity
	}
	expires := now.Add(r.lifetime)
	flowContext, cancel := context.WithDeadline(r.ctx, expires)
	entry := &login{
		snapshot: Snapshot{ID: id, Provider: providerID, Method: method, State: StatePending, ExpiresAt: expires},
		ready:    make(chan struct{}), done: make(chan struct{}), cancel: cancel,
		authorization: make(chan string, 1),
	}
	r.logins[id] = entry
	r.flowsRunning.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.flowsRunning.Done()
		r.run(flowContext, entry)
	}()

	timer := time.NewTimer(startTimeout)
	defer timer.Stop()
	select {
	case <-entry.ready:
	case <-timer.C:
		r.mu.Lock()
		r.finishLocked(entry, StateFailed, fmt.Sprintf("%s did not respond", provider.Name))
		r.mu.Unlock()
	case <-ctx.Done():
		// The caller cannot learn this login's ID, so nobody could follow it.
		r.mu.Lock()
		r.finishLocked(entry, StateCancelled, "")
		r.mu.Unlock()
		return Snapshot{}, ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.started != nil {
		return *entry.started, nil
	}
	return entry.copySnapshot(), nil
}

func (r *Registry) run(ctx context.Context, entry *login) {
	var err error
	switch entry.snapshot.Method {
	case MethodDevice:
		err = r.flows.Device(ctx, entry.snapshot.Provider, func(instructions DeviceInstructions) {
			r.mu.Lock()
			if entry.snapshot.State == StatePending {
				entry.snapshot.Device = &instructions
				started := entry.copySnapshot()
				entry.started = &started
			}
			r.mu.Unlock()
			entry.readyOnce.Do(func() { close(entry.ready) })
		})
	case MethodBrowser:
		err = r.flows.Browser(ctx, entry.snapshot.Provider, entry.authorization, func(instructions BrowserInstructions) {
			r.mu.Lock()
			if entry.snapshot.State == StatePending {
				entry.snapshot.Browser = &instructions
				started := entry.copySnapshot()
				entry.started = &started
			}
			r.mu.Unlock()
			entry.readyOnce.Do(func() { close(entry.ready) })
		})
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case err == nil:
		r.finishLocked(entry, StateSucceeded, "")
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.finishLocked(entry, StateExpired, "")
	case r.ctx.Err() != nil:
		r.finishLocked(entry, StateFailed, "the server stopped")
	default:
		r.finishLocked(entry, StateFailed, failureText(err))
	}
}

// finishLocked ends a pending login; a finished login keeps its state. Its
// instructions are dropped, so they stop circulating once they are useless.
func (r *Registry) finishLocked(entry *login, state State, failure string) {
	if entry.snapshot.State != StatePending {
		return
	}
	entry.snapshot.State = state
	entry.snapshot.Failure = failure
	entry.snapshot.Device = nil
	entry.snapshot.Browser = nil
	entry.finished = r.now()
	entry.cancel()
	entry.readyOnce.Do(func() { close(entry.ready) })
	close(entry.done)
}

func (r *Registry) purgeLocked(now time.Time) {
	for id, entry := range r.logins {
		if entry.snapshot.State != StatePending && now.Sub(entry.finished) >= retainFinished {
			delete(r.logins, id)
		}
	}
}

// evictLocked forgets the longest-finished login to make room.
func (r *Registry) evictLocked() bool {
	oldest := ""
	for id, entry := range r.logins {
		if entry.snapshot.State != StatePending && (oldest == "" || entry.finished.Before(r.logins[oldest].finished)) {
			oldest = id
		}
	}
	if oldest == "" {
		return false
	}
	delete(r.logins, oldest)
	return true
}

// Get returns a login's current state.
func (r *Registry) Get(id string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.logins[id]
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	return entry.copySnapshot(), nil
}

// Wait returns a login's state once it finishes, wait elapses (at most
// MaxWait), or ctx ends.
func (r *Registry) Wait(ctx context.Context, id string, wait time.Duration) (Snapshot, error) {
	r.mu.Lock()
	entry, ok := r.logins[id]
	r.mu.Unlock()
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	wait = min(max(wait, 0), MaxWait)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-entry.done:
	case <-timer.C:
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return entry.copySnapshot(), nil
}

// Submit gives a pending browser login its authorization: the redirect URL
// the browser was sent to, or the code the authorization page showed.
func (r *Registry) Submit(id, authorization string) error {
	if strings.TrimSpace(authorization) == "" || len(authorization) > MaxAuthorizationBytes {
		return fmt.Errorf("authorization must be non-empty and at most %d bytes", MaxAuthorizationBytes)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.logins[id]
	switch {
	case !ok:
		return ErrNotFound
	case entry.snapshot.Method != MethodBrowser:
		return ErrNotBrowser
	case entry.snapshot.State != StatePending:
		return ErrNotPending
	case entry.submitted:
		return ErrSubmitted
	}
	// The channel is empty: only this, the one accepted submission, sends.
	entry.authorization <- authorization
	entry.submitted = true
	return nil
}

// Cancel ends a pending login. Cancelling a finished login has no effect.
func (r *Registry) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.logins[id]
	if !ok {
		return ErrNotFound
	}
	r.finishLocked(entry, StateCancelled, "")
	return nil
}

// CancelProvider ends the provider's pending login, if any, so it cannot
// store a credential afterward.
func (r *Registry) CancelProvider(providerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.logins {
		if entry.snapshot.Provider == providerID {
			r.finishLocked(entry, StateCancelled, "")
		}
	}
}

func (l *login) copySnapshot() Snapshot {
	snapshot := l.snapshot
	if snapshot.Device != nil {
		device := *snapshot.Device
		snapshot.Device = &device
	}
	if snapshot.Browser != nil {
		browser := *snapshot.Browser
		snapshot.Browser = &browser
	}
	return snapshot
}

// failureText makes an error display-safe: one line of printable text.
func failureText(err error) string {
	text := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return ' '
		}
		return r
	}, err.Error())
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxFailureBytes {
		// Cutting may split a character; drop its partial bytes.
		text = strings.ToValidUTF8(text[:maxFailureBytes], "")
	}
	if text == "" {
		return "the login failed"
	}
	return text
}
