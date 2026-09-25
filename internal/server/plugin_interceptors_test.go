package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/protocol"
)

type interceptionProviders struct {
	daemonEchoProviders
	mu        sync.Mutex
	requested bool
	toolError bool
	result    string
}

func (p *interceptionProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (p *interceptionProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	defer p.mu.Unlock()
	response := droids.AssistantMessage{Provider: "test", Model: "echo", StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "done"}}}
	if !p.requested {
		p.requested = true
		response.StopReason = droids.StopReasonToolUse
		response.Content = []droids.AssistantContent{droids.ToolCall{ID: "intercepted_call", Name: "bash", Arguments: json.RawMessage(`{"command":"printf interception-executed"}`)}}
	} else {
		for _, message := range request.Messages {
			if result, ok := message.(droids.ToolResultMessage); ok && result.ToolName == "bash" {
				p.toolError = result.IsError
				for _, content := range result.Content {
					if text, ok := content.(droids.TextContent); ok {
						p.result += text.Text
					}
				}
			}
		}
	}
	return &daemonEchoStream{final: response}
}
func TestPluginInterceptionUsesSharedDialogBeforeCoreToolExecution(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python required")
	}
	for _, mode := range []string{"reject", "allow", "cancel"} {
		allow := mode == "allow"
		t.Run(mode, func(t *testing.T) {
			paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
			root := filepath.Join(paths.Plugins, "approval")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			script, err := os.ReadFile(filepath.Join("..", "plugin", "testdata", "approval-plugin.py"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "plugin.py"), script, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"manifestVersion":1,"id":"approval","transport":{"type":"stdio","command":"python3","args":["-u","plugin.py"]}}`), 0600); err != nil {
				t.Fatal(err)
			}
			provider := &interceptionProviders{}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() {
				done <- Run(ctx, RunOptions{Paths: paths, Providers: provider, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			}()
			defer func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(10 * time.Second):
					t.Error("daemon shutdown blocked")
				}
			}()
			client := NewClient(paths)
			defer client.http.CloseIdleConnections()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, _, err := client.Probe(t.Context()); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("daemon not ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "test/echo"})
			if err != nil {
				t.Fatal(err)
			}
			for {
				snapshot, err := client.GetSessionSnapshot(t.Context(), created.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(snapshot.PluginCommands) == 1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("interceptor not ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			turn := make(chan error, 1)
			go func() { _, err := client.RunPrompt(ctx, created.ID, "run the tool"); turn <- err }()
			var interaction protocol.InteractionRequest
			var runID string
			for {
				snapshot, err := client.GetSessionSnapshot(t.Context(), created.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(snapshot.PendingInteractions) == 1 {
					interaction = snapshot.PendingInteractions[0]
					runID = snapshot.ActiveRunID
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("interceptor did not request confirmation")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if interaction.Plugin == nil || interaction.Plugin.PluginID != "approval" || interaction.Detail != "bash" {
				t.Fatalf("interceptor dialog=%#v", interaction)
			}
			if mode == "cancel" {
				if err := client.AbortSession(ctx, created.ID, runID); err != nil {
					t.Fatal(err)
				}
				select {
				case <-turn:
				case <-time.After(5 * time.Second):
					t.Fatal("aborted interception remained blocked")
				}
				end := time.Now().Add(3 * time.Second)
				for {
					snapshot, err := client.GetSessionSnapshot(t.Context(), created.ID)
					if err != nil {
						t.Fatal(err)
					}
					if len(snapshot.PendingInteractions) == 0 {
						break
					}
					if time.Now().After(end) {
						t.Fatal("nested confirmation survived interception cancellation")
					}
					time.Sleep(10 * time.Millisecond)
				}
				return
			}
			if err := client.RespondInteraction(ctx, created.ID, protocol.InteractionResponse{RequestID: interaction.ID, Confirmed: &allow}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-turn:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("intercepted turn did not finish")
			}
			provider.mu.Lock()
			failed, result := provider.toolError, provider.result
			provider.mu.Unlock()
			if failed == allow {
				t.Fatalf("allow=%v toolError=%v result=%q", allow, failed, result)
			}
			expected := "User rejected intercepted tool"
			if allow {
				expected = "interception-executed"
			}
			if !strings.Contains(result, expected) {
				t.Fatalf("tool result=%q want %q", result, expected)
			}
		})
	}
}
