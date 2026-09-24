package vcs

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/githubpr"
)

// Snapshot is one session workspace's volatile combined local/remote VCS state.
// Git and PR values are detached copies; nil means unavailable or still loading.
type Snapshot struct {
	CWD         string
	Git         *Status
	PullRequest *githubpr.PullRequest
}

// ErrSubscriberLimit bounds live observers within one session.
var ErrSubscriberLimit = errors.New("VCS subscriber limit exceeded")

const observeInterval = 10 * time.Second
const maxSubscribers = 32

// Observer owns a loaded session's Git polling and PR observation independently
// of client attachments. It publishes latest-only snapshots without blocking readers.
type Observer struct {
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	done            chan struct{}
	wake            chan struct{}
	updates         chan struct{}
	started, closed bool
	snapshot        Snapshot
	epoch           uint64
	ready           chan struct{}
	initialized     bool
	probeCancel     context.CancelFunc
	subscribers     map[chan Snapshot]struct{}
	pullRequests    *githubpr.Cache
	probe           func(context.Context, string) (*Status, error)
	interval        time.Duration
}

// NewObserver constructs an unstarted session-owned observer.
func NewObserver(ctx context.Context, cwd string, cache *githubpr.Cache) *Observer {
	ctx, cancel := context.WithCancel(ctx)
	return &Observer{ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), updates: make(chan struct{}, 1), snapshot: Snapshot{CWD: cwd}, ready: make(chan struct{}), subscribers: make(map[chan Snapshot]struct{}), pullRequests: cache, probe: Probe, interval: observeInterval}
}

// Start begins background observation without waiting for Git or GitHub.
func (o *Observer) Start() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.started || o.closed {
		return
	}
	o.started = true
	go o.run()
}

// Updates coalesces invalidations for the plugin adapter. Consumers read Current.
func (o *Observer) Updates() <-chan struct{} { return o.updates }

// Current returns a detached latest snapshot, including pending/unavailable state.
func (o *Observer) Current() Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return cloneSnapshot(o.snapshot)
}

// Read waits only for the first local Git probe of the current cwd, never GitHub.
func (o *Observer) Read(ctx context.Context) (Snapshot, error) {
	for {
		o.mu.Lock()
		ready, closed := o.ready, o.closed
		o.mu.Unlock()
		if closed {
			return Snapshot{}, context.Canceled
		}
		select {
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		case <-o.ctx.Done():
			return Snapshot{}, o.ctx.Err()
		case <-ready:
		}
		o.mu.Lock()
		if o.ready == ready {
			value := cloneSnapshot(o.snapshot)
			o.mu.Unlock()
			return value, nil
		}
		o.mu.Unlock()
	}
}

// ChangeCWD immediately clears the previous workspace, revokes an in-flight
// probe, and schedules a fresh one. It performs no filesystem work or waits.
func (o *Observer) ChangeCWD(cwd string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.snapshot.CWD == cwd {
		return
	}
	o.epoch++
	if o.probeCancel != nil {
		o.probeCancel()
	}
	if !o.initialized {
		close(o.ready)
	}
	o.ready = make(chan struct{})
	o.initialized = false
	o.publishLocked(Snapshot{CWD: cwd})
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

// Close cancels observation and all subscriptions. Caller cancellation does not
// abandon the worker; subsequent callers may wait for the same owned cleanup.
func (o *Observer) Close(ctx context.Context) error {
	o.mu.Lock()
	if !o.closed {
		o.closed = true
		o.cancel()
		for c := range o.subscribers {
			delete(o.subscribers, c)
			close(c)
		}
		if !o.started {
			close(o.done)
		}
	}
	o.mu.Unlock()
	select {
	case <-o.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Subscription is a bounded latest-state stream, beginning with a fresh snapshot.
type Subscription struct {
	observer *Observer
	values   chan Snapshot
}

// Subscribe snapshots atomically with admission. Slow readers coalesce updates.
func (o *Observer) Subscribe() (*Subscription, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, context.Canceled
	}
	if len(o.subscribers) >= maxSubscribers {
		return nil, ErrSubscriberLimit
	}
	c := make(chan Snapshot, 1)
	c <- cloneSnapshot(o.snapshot)
	o.subscribers[c] = struct{}{}
	return &Subscription{o, c}, nil
}

// Next waits for one current state or the caller's cancellation.
func (s *Subscription) Next(ctx context.Context) (Snapshot, error) {
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case value, ok := <-s.values:
		if !ok {
			return Snapshot{}, context.Canceled
		}
		return value, nil
	}
}

// Close releases the subscription and is safe to call repeatedly.
func (s *Subscription) Close() {
	o := s.observer
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.subscribers[s.values]; ok {
		delete(o.subscribers, s.values)
		close(s.values)
	}
}

func cloneSnapshot(value Snapshot) Snapshot {
	result := value
	if value.Git != nil {
		git := *value.Git
		result.Git = &git
	}
	if value.PullRequest != nil {
		pr := *value.PullRequest
		result.PullRequest = &pr
	}
	return result
}
func (o *Observer) publishLocked(value Snapshot) {
	if reflect.DeepEqual(o.snapshot, value) {
		return
	}
	o.snapshot = cloneSnapshot(value)
	for c := range o.subscribers {
		select {
		case <-c:
		default:
		}
		c <- cloneSnapshot(value)
	}
	select {
	case o.updates <- struct{}{}:
	default:
	}
}
func (o *Observer) run() {
	defer close(o.done)
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()
	o.refreshGit()
	for {
		var changes <-chan struct{}
		if o.pullRequests != nil {
			changes = o.pullRequests.Changes()
		}
		o.refreshPR()
		select {
		case <-o.ctx.Done():
			return
		case <-o.wake:
			o.refreshGit()
		case <-ticker.C:
			o.refreshGit()
		case <-changes:
		}
	}
}
func (o *Observer) refreshGit() {
	o.mu.Lock()
	if o.closed || o.ctx.Err() != nil {
		o.mu.Unlock()
		return
	}
	cwd, epoch := o.snapshot.CWD, o.epoch
	ctx, cancel := context.WithTimeout(o.ctx, 2*time.Second)
	o.probeCancel = cancel
	o.mu.Unlock()
	git, err := o.probe(ctx, cwd)
	cancel()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.epoch != epoch || o.ctx.Err() != nil {
		return
	}
	o.probeCancel = nil
	// An unavailable local probe clears local/remote information. Cancellation of
	// a superseded cwd was fenced above and cannot clear the new workspace.
	if err != nil {
		git = nil
	}
	value := Snapshot{CWD: cwd, Git: git}
	if git != nil && o.snapshot.Git != nil && git.Root == o.snapshot.Git.Root && git.Head == o.snapshot.Git.Head {
		value.PullRequest = o.snapshot.PullRequest
	}
	o.publishLocked(value)
	if !o.initialized {
		o.initialized = true
		close(o.ready)
	}
}
func (o *Observer) refreshPR() {
	if o.pullRequests == nil {
		return
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	value, epoch := cloneSnapshot(o.snapshot), o.epoch
	o.mu.Unlock()
	if value.Git == nil || value.Git.Head.Kind != HeadBranch {
		return
	}
	pr := o.pullRequests.Get(value.CWD, value.Git.Root, value.Git.Head.Name)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.epoch != epoch || !reflect.DeepEqual(o.snapshot.Git, value.Git) {
		return
	}
	value.PullRequest = pr
	o.publishLocked(value)
}
