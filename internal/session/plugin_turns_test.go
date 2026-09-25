package session

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

type blockingHistoryStore struct {
	droids.Store
	block              atomic.Bool
	ignoreCancellation bool
	started            chan struct{}
	release            chan struct{}
	once               sync.Once
}

func (s *blockingHistoryStore) Records(ctx context.Context, query droids.RecordQuery) (droids.RecordPage, error) {
	if !s.block.Load() {
		return s.Store.Records(ctx, query)
	}
	s.once.Do(func() { close(s.started) })
	if s.ignoreCancellation {
		<-s.release
		return droids.RecordPage{}, context.Canceled
	}
	<-ctx.Done()
	return droids.RecordPage{}, ctx.Err()
}

type turnBridgeTestHost struct{}

func (*turnBridgeTestHost) Start()                      {}
func (*turnBridgeTestHost) ChangeCWD(string)            {}
func (*turnBridgeTestHost) Rename(string)               {}
func (*turnBridgeTestHost) Reload()                     {}
func (*turnBridgeTestHost) TurnStarted(string) bool     { return true }
func (*turnBridgeTestHost) TurnCompleted(PluginTurn)    {}
func (*turnBridgeTestHost) Close(context.Context) error { return nil }
func (*turnBridgeTestHost) Warnings() []string          { return nil }

func TestRuntimeCloseJoinsInFlightTurnProjectionBeforeStoreClose(t *testing.T) {
	store := &blockingHistoryStore{Store: droids.NewMemoryStore(), started: make(chan struct{})}
	baseModel := droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
	model, err := droids.BindModel(droids.AdaptProvider("test", []droids.Model{baseModel}, func(context.Context, droids.Model, droids.Request) droids.Stream { return nil }), baseModel)
	if err != nil {
		t.Fatal(err)
	}
	droid, err := droids.Spawn(t.Context(), "conversation_bridge_close", droids.Config{Store: store, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	bridge := newPluginTurnEventBridge(t.Context())
	bridge.droid.Store(droid)
	bridge.binding.Store(&pluginTurnEventBinding{host: &turnBridgeTestHost{}})
	bridge.started("turn_bridge_close")
	store.block.Store(true)
	bridge.turnSettled("turn_bridge_close")
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("turn projection did not enter history read")
	}
	storeClosed := false
	runtime := &runtime{turnEvents: bridge, droid: droid, closeStore: func() error {
		select {
		case <-bridge.done:
			storeClosed = true
		default:
			t.Fatal("store closed before turn-event worker joined")
		}
		return nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := runtime.close(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if !storeClosed {
		t.Fatal("runtime did not close store")
	}
}

func TestRuntimeCloseTransfersCleanupAfterTurnProjectionDeadline(t *testing.T) {
	store := &blockingHistoryStore{Store: droids.NewMemoryStore(), ignoreCancellation: true, started: make(chan struct{}), release: make(chan struct{})}
	baseModel := droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
	model, err := droids.BindModel(droids.AdaptProvider("test", []droids.Model{baseModel}, func(context.Context, droids.Model, droids.Request) droids.Stream { return nil }), baseModel)
	if err != nil {
		t.Fatal(err)
	}
	droid, err := droids.Spawn(t.Context(), "conversation_bridge_deadline", droids.Config{Store: store, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	bridge := newPluginTurnEventBridge(t.Context())
	bridge.droid.Store(droid)
	bridge.binding.Store(&pluginTurnEventBinding{host: &turnBridgeTestHost{}})
	bridge.started("turn_bridge_deadline")
	store.block.Store(true)
	bridge.turnSettled("turn_bridge_deadline")
	<-store.started
	storeClosed := make(chan struct{})
	runtime := &runtime{turnEvents: bridge, droid: droid, closeStore: func() error { close(storeClosed); return nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := runtime.close(ctx, "test"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want deadline exceeded", err)
	}
	select {
	case <-storeClosed:
		t.Fatal("store closed while turn projection still owned it")
	default:
	}
	secondClose := make(chan error, 1)
	go func() { secondClose <- runtime.close(context.Background(), "test") }()
	close(store.release)
	select {
	case <-storeClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("transferred runtime cleanup did not close store")
	}
	if err := <-secondClose; err != nil {
		t.Fatalf("second close = %v", err)
	}
}

func TestRuntimeCloseOwnsCleanupAfterCallerCancellation(t *testing.T) {
	store := &blockingHistoryStore{Store: droids.NewMemoryStore(), ignoreCancellation: true, started: make(chan struct{}), release: make(chan struct{})}
	baseModel := droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
	model, err := droids.BindModel(droids.AdaptProvider("test", []droids.Model{baseModel}, func(context.Context, droids.Model, droids.Request) droids.Stream { return nil }), baseModel)
	if err != nil {
		t.Fatal(err)
	}
	droid, err := droids.Spawn(t.Context(), "conversation_bridge_cancelled", droids.Config{Store: store, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	bridge := newPluginTurnEventBridge(t.Context())
	bridge.droid.Store(droid)
	bridge.binding.Store(&pluginTurnEventBinding{host: &turnBridgeTestHost{}})
	bridge.started("turn_bridge_cancelled")
	store.block.Store(true)
	bridge.turnSettled("turn_bridge_cancelled")
	<-store.started
	storeClosed := make(chan struct{})
	runtime := &runtime{turnEvents: bridge, droid: droid, closeStore: func() error { close(storeClosed); return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.close(ctx, "test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("close error = %v, want cancelled", err)
	}
	select {
	case <-storeClosed:
		t.Fatal("store closed while turn projection still owned it")
	default:
	}
	close(store.release)
	select {
	case <-storeClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("owned cleanup did not close store after caller cancellation")
	}
}

func TestProjectPluginTurnMessageKeepsOnlyOrderedPublicText(t *testing.T) {
	user, ok := projectPluginTurnMessage(droids.UserMessage{Content: []droids.InputContent{
		droids.TextInput{Text: "first"},
		droids.FileInput{Filename: "photo.png", MediaType: "image/png", URL: "data:image/png;base64,AA=="},
		droids.AnnotationInput{SubmissionID: "annotation_submission_test", Text: "synthetic annotation"},
		droids.TextInput{Text: "second", AttachmentID: "attachment_test", Filename: "notes.txt", MediaType: "text/plain"},
	}})
	if !ok || !reflect.DeepEqual(user, PluginTurnMessage{Role: "user", Content: []string{"first", "second"}}) {
		t.Fatalf("user = %#v, %v", user, ok)
	}
	assistant, ok := projectPluginTurnMessage(droids.AssistantMessage{Content: []droids.AssistantContent{
		droids.ThinkingContent{Thinking: "private"},
		droids.TextContent{Text: "visible one", Signature: "provider metadata"},
		droids.ToolCall{ID: "tool_call_test", Name: "read"},
		droids.TextContent{Text: "visible two"},
	}})
	if !ok || !reflect.DeepEqual(assistant, PluginTurnMessage{Role: "assistant", Content: []string{"visible one", "visible two"}}) {
		t.Fatalf("assistant = %#v, %v", assistant, ok)
	}
	if _, ok := projectPluginTurnMessage(droids.ToolResultMessage{}); ok {
		t.Fatal("tool result was exposed as a public turn message")
	}
}

// completionBlockingHost exposes the interval between claiming and delivering a
// completion, which must not unblock native turn settlement.
type completionBlockingHost struct {
	turnBridgeTestHost
	entered chan struct{}
	release chan struct{}
}

func (h *completionBlockingHost) TurnCompleted(PluginTurn) {
	close(h.entered)
	<-h.release
}

func TestPluginTurnSettlementWaitsForCompletionDelivery(t *testing.T) {
	for _, superseded := range []bool{false, true} {
		name := "projected"
		if superseded {
			name = "superseded"
		}
		t.Run(name, func(t *testing.T) {
			bridge := newPluginTurnEventBridge(t.Context())
			defer closePluginTurnEventBridge(bridge)
			host := &completionBlockingHost{entered: make(chan struct{}), release: make(chan struct{})}
			bridge.binding.Store(&pluginTurnEventBinding{host: host})
			bridge.started("first")
			bridge.mu.Lock()
			settlement := bridge.settled["first"]
			bridge.mu.Unlock()
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				if superseded {
					bridge.started("second")
				} else {
					bridge.deliverCompletion(PluginTurn{ID: "first"})
				}
			}()
			defer func() {
				close(host.release)
				<-finished
				select {
				case <-settlement.done:
				default:
					t.Error("turn did not settle after completion callback returned")
				}
			}()
			select {
			case <-host.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("completion callback did not start")
			}
			select {
			case <-settlement.done:
				t.Fatal("turn settled before completion callback returned")
			default:
			}
		})
	}
}
