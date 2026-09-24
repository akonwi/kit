package droids_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
)

type toolRecoveryProviders struct {
	*readProviders
	calls atomic.Int32
}

func (p *toolRecoveryProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, errors.New("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (p *toolRecoveryProviders) Stream(context.Context, droids.Model, droids.Request) droids.Stream {
	if p.calls.Add(1) == 1 {
		var content []droids.AssistantContent
		for _, name := range []string{"plugin__owned", "base_first", "base_second"} {
			content = append(content, droids.ToolCall{ID: droids.ToolCallID("call_" + name), Name: name, Arguments: []byte(`{}`)})
		}
		return sdkStaticStream(droids.AssistantMessage{Provider: "test", Model: "read", StopReason: droids.StopReasonToolUse, Content: content})
	}
	return sdkStaticStream(droids.AssistantMessage{Provider: "test", Model: "read", StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "Recovered safely"}}})
}

func TestRecoveredPluginBatchRetainsModeAndRejectsReplacementAfterStoreReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "tools.db")
	store, err := sqlitestore.Open(ctx, sqlitestore.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	providers := &toolRecoveryProviders{readProviders: newReadProviders()}
	model := resolvedTestModel(providers, "test/read")
	var mu sync.Mutex
	var order []string
	names := map[droids.ToolCallID]string{}
	appendStep := func(step string) { mu.Lock(); defer mu.Unlock(); order = append(order, step) }
	var oldRuns, replacementRuns atomic.Int32
	makeTool := func(name, registration string, mode droids.ExecutionMode, counter *atomic.Int32) droids.AnyTool {
		return droids.MustTool(droids.Tool[struct{}]{Name: name, RegistrationID: registration, Mode: mode, Execute: func(context.Context, droids.ToolContext, struct{}, droids.ToolUpdate) (droids.ToolResult, error) {
			if counter != nil {
				counter.Add(1)
			}
			appendStep("execute:" + name)
			return droids.ToolText(name), nil
		}})
	}
	base := []droids.AnyTool{makeTool("base_first", "", droids.ModeParallel, nil), makeTool("base_second", "", droids.ModeParallel, nil)}
	hookStarted := make(chan struct{})
	var once sync.Once
	first, err := droids.Spawn(ctx, "conversation_plugin_recovery", droids.Config{Store: store, Model: model, Tools: base,
		BeforeToolCall: func(ctx context.Context, _ droids.ToolContext, _ droids.ToolCall) (droids.BeforeToolResult, error) {
			once.Do(func() { close(hookStarted) })
			<-ctx.Done()
			return droids.BeforeToolResult{}, ctx.Err()
		},
		AfterToolCall: func(context.Context, droids.ToolContext, droids.ToolResult) (*droids.ToolResult, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.SetAdditionalTools([]droids.AnyTool{makeTool("plugin__owned", "owner:old", droids.ModeSequential, &oldRuns)}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Prompt(ctx, droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "Run the batch"}}}, droids.PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hookStarted:
	case <-ctx.Done():
		t.Fatal("tool batch did not reach durable before-hook boundary")
	}
	if err := first.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlitestore.Open(ctx, sqlitestore.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var pluginError bool
	second, err := droids.Spawn(ctx, "conversation_plugin_recovery", droids.Config{Store: reopened, Model: model, Tools: base,
		BeforeToolCall: func(_ context.Context, call droids.ToolContext, tool droids.ToolCall) (droids.BeforeToolResult, error) {
			mu.Lock()
			names[call.ToolCallID] = tool.Name
			order = append(order, "before:"+tool.Name)
			mu.Unlock()
			return droids.BeforeToolResult{}, nil
		},
		AfterToolCall: func(_ context.Context, call droids.ToolContext, result droids.ToolResult) (*droids.ToolResult, error) {
			mu.Lock()
			defer mu.Unlock()
			name := names[call.ToolCallID]
			order = append(order, "after:"+name)
			if name == "plugin__owned" {
				pluginError = result.IsError
			}
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	// The new owner declares parallel mode. Neither its callback nor its mode
	// may replace the old, durably admitted sequential registration.
	if err := second.SetAdditionalTools([]droids.AnyTool{makeTool("plugin__owned", "owner:new", droids.ModeParallel, &replacementRuns)}, ""); err != nil {
		t.Fatal(err)
	}
	if err := second.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := second.WaitQuiescent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != droids.QuiescentSettled {
		t.Fatalf("recovered state = %#v", state)
	}
	mu.Lock()
	defer mu.Unlock()
	expected := []string{"before:plugin__owned", "after:plugin__owned", "before:base_first", "execute:base_first", "after:base_first", "before:base_second", "execute:base_second", "after:base_second"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("recovered hook/callback order = %v, want %v", order, expected)
	}
	if !pluginError || oldRuns.Load() != 0 || replacementRuns.Load() != 0 {
		t.Fatalf("plugin recovery: error=%v old=%d replacement=%d", pluginError, oldRuns.Load(), replacementRuns.Load())
	}
}
