package droids_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
)

func interceptionTestTools(counter *atomic.Int32) []droids.AnyTool {
	var tools []droids.AnyTool
	for _, name := range []string{"plugin__owned", "base_first", "base_second"} {
		tools = append(tools, droids.MustTool(droids.Tool[struct{}]{Name: name, Mode: droids.ModeParallel, Execute: func(context.Context, droids.ToolContext, struct{}, droids.ToolUpdate) (droids.ToolResult, error) {
			counter.Add(1)
			return droids.ToolText("executed"), nil
		}}))
	}
	return tools
}

func TestParallelPreparedToolsRecheckPolicyBeforeExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	providers := &toolRecoveryProviders{readProviders: newReadProviders()}
	var executed, rejected atomic.Int32
	var identity atomic.Value
	identity.Store("policy:old")
	var hooks atomic.Int32
	agent, err := droids.Spawn(ctx, "interceptor_parallel", droids.Config{Store: droids.NewMemoryStore(), Model: resolvedTestModel(providers, "test/read"), Tools: interceptionTestTools(&executed), Execution: &droids.ExecutionPolicy{ToolExecution: droids.ModeParallel, MaxParallelTools: 3}, BeforeToolCallIdentity: func() string { return identity.Load().(string) }, BeforeToolCall: func(_ context.Context, call droids.ToolContext, _ droids.ToolCall) (droids.BeforeToolResult, error) {
		if call.BeforeHookIdentity != "policy:old" {
			t.Errorf("admitted identity=%q", call.BeforeHookIdentity)
		}
		if hooks.Add(1) == 2 {
			identity.Store("policy:new")
		}
		return droids.BeforeToolResult{}, nil
	}, AfterToolCall: func(_ context.Context, _ droids.ToolContext, result droids.ToolResult) (*droids.ToolResult, error) {
		if result.IsError {
			rejected.Add(1)
		}
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if _, err := agent.Prompt(ctx, droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "batch"}}}, droids.PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	state, err := agent.WaitQuiescent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != droids.QuiescentSettled || executed.Load() != 0 || rejected.Load() != 1 {
		t.Fatalf("policy change: state=%s executions=%d rejected=%d", state.Kind, executed.Load(), rejected.Load())
	}
}

func TestRecoveredToolsRejectReplacedInterceptionPolicy(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "interception.db")
			store, err := sqlitestore.Open(ctx, sqlitestore.Options{Path: path})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			providers := &toolRecoveryProviders{readProviders: newReadProviders()}
			model := resolvedTestModel(providers, "test/read")
			var executed, rejected atomic.Int32
			started := make(chan struct{})
			var once sync.Once
			var captured func() string
			if !legacy {
				captured = func() string { return "old-host:1" }
			}
			first, err := droids.Spawn(ctx, "interception_recovery", droids.Config{Store: store, Model: model, Tools: interceptionTestTools(&executed), BeforeToolCallIdentity: captured, BeforeToolCall: func(ctx context.Context, _ droids.ToolContext, _ droids.ToolCall) (droids.BeforeToolResult, error) {
				once.Do(func() { close(started) })
				<-ctx.Done()
				return droids.BeforeToolResult{}, ctx.Err()
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			if _, err := first.Prompt(ctx, droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "batch"}}}, droids.PromptOptions{}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("hook did not start")
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
			second, err := droids.Spawn(ctx, "interception_recovery", droids.Config{Store: reopened, Model: model, Tools: interceptionTestTools(&executed), BeforeToolCallIdentity: func() string { return "replacement-host:1" }, BeforeToolCall: func(context.Context, droids.ToolContext, droids.ToolCall) (droids.BeforeToolResult, error) {
				t.Error("replayed old interception against replacement")
				return droids.BeforeToolResult{}, nil
			}, AfterToolCall: func(_ context.Context, _ droids.ToolContext, result droids.ToolResult) (*droids.ToolResult, error) {
				if result.IsError {
					rejected.Add(1)
				}
				return nil, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			if err := second.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			state, err := second.WaitQuiescent(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if state.Kind != droids.QuiescentSettled || executed.Load() != 0 {
				t.Fatalf("recovery: state=%s executions=%d", state.Kind, executed.Load())
			}
		})
	}
}
