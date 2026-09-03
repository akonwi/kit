package droids

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// droid.go — the ergonomic facade composing the three layers: a Provider
// (model abstraction), the agent loop, and Storage (durability). One Droid is
// one session/conversation; create many for many concurrent sessions.

// Options configures a Droid.
type Options struct {
	// Providers is the model abstraction (see NewProviders). Required.
	Providers Providers
	// Model is the user-facing model id, resolved against Providers. Required.
	Model string
	// SystemPrompt is sent with every turn.
	SystemPrompt string
	// Tools are available to the model.
	Tools []AnyTool
	// Storage persists and rehydrates the transcript. Defaults to an
	// in-memory store (NewMemoryStorage) when nil.
	Storage Storage
	// Session identifies this conversation for Storage. When Storage is set
	// and a transcript exists, it is loaded on New (resume).
	Session string
	// MaxSteps bounds the tool loop per run. Default: 16.
	MaxSteps int
	// Reasoning selects a default thinking level for turns ("" = provider
	// default, "none"/"off" = explicitly disabled, or "minimal"…"max").
	Reasoning string
	// MaxTokens is the per-request output allowance. Zero defaults to 4096,
	// capped by the selected model's MaxOutputTokens capability. Reasoning modes
	// that require a larger allowance return a configuration error. Models with
	// OutputLimitProviderControlled reject an explicit value.
	MaxTokens int

	// ToolExecution sets the default execution mode for a batch of tool calls
	// (ModeParallel | ModeSequential). Default: parallel. A batch runs
	// sequentially if this is sequential or any tool in it is ModeSequential.
	ToolExecution ExecutionMode

	// MaxParallelTools caps concurrent tool executions in a parallel batch.
	// 0 (default) means unbounded.
	MaxParallelTools int

	// BeforeToolCall runs after receiving a tool call and before its arguments
	// are decoded or the tool executes. It can reject the call (returning an
	// error result to the model while the run continues), or short-circuit it
	// with a cached result
	// (idempotency). A returned error degrades to an error tool result; it never
	// crashes the loop. Optional.
	BeforeToolCall func(ctx context.Context, in BeforeToolContext) (BeforeToolResult, error)

	// AfterToolCall runs after a tool executes and before its result is
	// emitted/persisted. Return a non-nil result to replace it; nil leaves it
	// unchanged. A replacement's IsError becomes the final error status. A
	// returned Go error degrades to an error tool result. Optional.
	AfterToolCall func(ctx context.Context, in AfterToolContext) (*ToolResult, error)

	// Compact is invoked before the first provider request of a run when the
	// estimated context crosses Droids' compaction threshold, or once after a
	// provider context-overflow error. Nil disables application compaction.
	Compact CompactionHook
}

// Droid is a live agent session.
type Droid struct {
	opts              Options
	providers         Providers
	model             Model
	maxTokens         int
	compactionReserve int
	tools             map[string]AnyTool

	mu         sync.Mutex
	transcript []Message
	steering   []Message
	cancelRun  context.CancelFunc

	queueMu      sync.Mutex
	accepting    bool
	queue        chan queuedPrompt
	eventsMu     sync.Mutex
	events       chan Event
	activeRun    *agentRun
	eventsClosed bool
	closeOnce    sync.Once
	closed       chan struct{}
}

type queuedPrompt struct {
	ctx          context.Context
	messages     []Message
	continuation bool
	result       chan runResult
	stream       *agentRun
}

type runResult struct {
	message AssistantMessage
	err     error
}

// New creates a Droid, resolving the model and rehydrating stored history.
func New(opts Options) (*Droid, error) {
	if opts.Providers == nil {
		return nil, fmt.Errorf("droids: Options.Providers is required")
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("droids: Options.Model is required")
	}
	model, ok := opts.Providers.Model(opts.Model)
	if !ok {
		return nil, fmt.Errorf("droids: unknown model %q (namespace as \"provider/model\" if ambiguous)", opts.Model)
	}
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = 16
	}
	maxTokens, err := resolveRequestMaxTokens(model, opts.MaxTokens, opts.Reasoning)
	if err != nil {
		return nil, err
	}
	if opts.Storage == nil {
		opts.Storage = NewMemoryStorage()
	}
	d := &Droid{
		opts:              opts,
		providers:         opts.Providers,
		model:             model,
		maxTokens:         maxTokens,
		compactionReserve: maxTokens,
		tools:             map[string]AnyTool{},
		accepting:         true,
		queue:             make(chan queuedPrompt, 64),
		closed:            make(chan struct{}),
	}
	for _, t := range opts.Tools {
		schema := t.schema()
		if schema.Name == "" {
			return nil, fmt.Errorf("droids: tool name is required")
		}
		if _, exists := d.tools[schema.Name]; exists {
			return nil, fmt.Errorf("droids: duplicate tool name %q", schema.Name)
		}
		d.tools[schema.Name] = t
	}

	{
		history, err := opts.Storage.Load(context.Background(), opts.Session)
		if err != nil {
			return nil, fmt.Errorf("droids: load session %q: %w", opts.Session, err)
		}
		d.transcript = history
	}

	go d.worker()
	return d, nil
}

// Events returns the droid's long-lived event channel. The same channel is
// returned on every call, including after Close. It is created lazily: if you
// never call Events, the loop runs without emitting live events (Storage
// persistence still happens). The channel is closed once the droid finishes
// shutting down (shortly after Close); ranging over it therefore terminates.
// A first call after shutdown has completed returns an already-closed channel.
func (d *Droid) Events() <-chan Event {
	d.eventsMu.Lock()
	defer d.eventsMu.Unlock()
	if d.events == nil {
		d.events = make(chan Event, 64)
		if d.eventsClosed {
			close(d.events)
		}
	}
	return d.events
}

func (d *Droid) emit(ev Event) {
	d.eventsMu.Lock()
	// A Run owns events for its run. Bounded delivery applies backpressure and
	// preserves terminal events even during Close.
	stream := d.activeRun
	ch := d.events
	closed := d.eventsClosed
	d.eventsMu.Unlock()
	if stream != nil {
		stream.emit(ev)
		return
	}
	if ch == nil || closed {
		return
	}
	select {
	case ch <- ev:
	case <-d.closed:
	}
}

func (d *Droid) setActiveRun(stream *agentRun) {
	d.eventsMu.Lock()
	d.activeRun = stream
	d.eventsMu.Unlock()
}

// Send enqueues a user prompt. If the droid is idle it starts a run; if a run
// is active the prompt is processed after it (a follow-up). Returns once the
// prompt is enqueued, not when the run completes — use Events to observe
// progress.
//
// ctx bounds the resulting run: cancelling it (or its deadline elapsing) aborts
// the run's provider calls and tool execution. For fire-and-forget work that
// must outlive the calling scope, pass context.Background().
func (d *Droid) Send(ctx context.Context, text string) error {
	return d.SendMessage(ctx, userText(text))
}

// SendMessage enqueues a provider-neutral user message. It is the structured
// content equivalent of Send and supports text, image, and file blocks.
func (d *Droid) SendMessage(ctx context.Context, message UserMessage) error {
	return d.enqueue(ctx, []Message{cloneUserMessage(message)}, false, nil, nil)
}

// Execute enqueues a prompt and blocks until its run completes, returning the
// final assistant message. Convenience for synchronous one-shot workloads; it
// does not require consuming Events.
func (d *Droid) Execute(ctx context.Context, text string) (AssistantMessage, error) {
	return d.ExecuteMessage(ctx, userText(text))
}

// ExecuteMessage executes a provider-neutral user message synchronously. It is
// the structured content equivalent of Execute.
func (d *Droid) ExecuteMessage(ctx context.Context, message UserMessage) (AssistantMessage, error) {
	return d.execute(ctx, []Message{cloneUserMessage(message)}, false)
}

// Stream enqueues a prompt and returns its run-scoped event stream. The
// stream's Events channel closes when this run ends; Result then returns the
// final assistant message and error. Events for this run are delivered to the
// returned stream rather than Droid.Events().
func (d *Droid) Stream(ctx context.Context, text string) (Run, error) {
	return d.StreamMessage(ctx, userText(text))
}

// StreamMessage starts a run from a provider-neutral user message. It is the
// structured content equivalent of Stream.
func (d *Droid) StreamMessage(ctx context.Context, message UserMessage) (Run, error) {
	return d.stream(ctx, []Message{cloneUserMessage(message)}, false)
}

func cloneUserMessage(message UserMessage) UserMessage {
	message.Content = append([]Content(nil), message.Content...)
	return message
}

// Continue resumes the loop from the current transcript without injecting a
// new message. The transcript must end with a complete, source-ordered batch
// of tool results matching the preceding assistant tool calls. A continuation
// receives a fresh MaxSteps budget.
func (d *Droid) Continue(ctx context.Context) (AssistantMessage, error) {
	return d.execute(ctx, nil, true)
}

// ContinueStream is the run-scoped streaming equivalent of Continue. Transcript
// validation occurs when the queued continuation starts; validation failures
// are returned by Run.Result and close Run.Events without emitting events.
func (d *Droid) ContinueStream(ctx context.Context) (Run, error) {
	return d.stream(ctx, nil, true)
}

func (d *Droid) execute(ctx context.Context, messages []Message, continuation bool) (AssistantMessage, error) {
	result := make(chan runResult, 1)
	if err := d.enqueue(ctx, messages, continuation, result, nil); err != nil {
		return AssistantMessage{}, err
	}
	select {
	case r := <-result:
		return r.message, r.err
	case <-ctx.Done():
		return AssistantMessage{}, ctx.Err()
	case <-d.closed:
		return AssistantMessage{}, fmt.Errorf("droids: droid closed")
	}
}

func (d *Droid) stream(ctx context.Context, messages []Message, continuation bool) (Run, error) {
	stream := newAgentRun()
	if err := d.enqueue(ctx, messages, continuation, nil, stream); err != nil {
		return nil, err
	}
	return stream, nil
}

func (d *Droid) enqueue(
	ctx context.Context,
	messages []Message,
	continuation bool,
	result chan runResult,
	stream *agentRun,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Admission and worker shutdown share queueMu. Once accepting becomes
	// false, no prompt can race past the worker's final queue drain.
	d.queueMu.Lock()
	defer d.queueMu.Unlock()
	if !d.accepting {
		return fmt.Errorf("droids: droid closed")
	}
	select {
	case d.queue <- queuedPrompt{
		ctx: ctx, messages: messages, continuation: continuation,
		result: result, stream: stream,
	}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-d.closed:
		return fmt.Errorf("droids: droid closed")
	}
}

// Steer injects a message mid-run. It is picked up between turns of the active
// normal run (or the next normal run if idle). Continuation runs deliberately
// retain steering so their exact tool-result transcript is not changed.
func (d *Droid) Steer(text string) {
	d.mu.Lock()
	d.steering = append(d.steering, userText(text))
	d.mu.Unlock()
}

// Abort interrupts the active run, if any. The current turn ends with an
// aborted error event.
func (d *Droid) Abort() {
	d.mu.Lock()
	cancel := d.cancelRun
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Close shuts the droid down: it aborts any in-flight run and closes the events
// channel. Idempotent.
func (d *Droid) Close() {
	d.closeOnce.Do(func() {
		// Closing the signal first unblocks any enqueue waiting for queue space.
		// queueMu then gates all future admissions before the worker's final
		// drain.
		close(d.closed)
		d.queueMu.Lock()
		d.accepting = false
		d.queueMu.Unlock()
	})
}

func userText(text string) UserMessage {
	return UserMessage{Content: []Content{TextContent{Text: text}}, Timestamp: time.Now().UnixMilli()}
}
