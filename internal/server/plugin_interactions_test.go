package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/protocol"
)

func pluginDialogClient(t *testing.T) (*Client, string, protocol.PluginCommand) {
	t.Helper()
	return pluginFixtureClient(t, "ui-api-demo", 1)
}

func pluginFixtureClient(t *testing.T, name string, commandCount int) (*Client, string, protocol.PluginCommand) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("UI fixture requires Python")
	}
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	root := filepath.Join(paths.Plugins, name)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"plugin.json", "plugin.py"} {
		data, err := os.ReadFile(filepath.Join("..", "..", ".kit", "plugins", name, filename))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filename), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: paths, Providers: &daemonEchoProviders{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("daemon shutdown blocked on plugin interaction")
		}
	})
	client := NewClient(paths)
	t.Cleanup(client.http.CloseIdleConnections)
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
	info, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		snapshot, err := client.GetSessionSnapshot(t.Context(), info.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.PluginCommands) == commandCount {
			return client, info.ID, snapshot.PluginCommands[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("UI plugin not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func nextPluginInteraction(t *testing.T, client *Client, id, previous string) protocol.InteractionRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := client.GetSessionSnapshot(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.ActiveRunID != "" || len(snapshot.Messages) != 0 {
			t.Fatal("plugin dialog fabricated a model turn")
		}
		if len(snapshot.PendingInteractions) == 1 && snapshot.PendingInteractions[0].ID != previous {
			return snapshot.PendingInteractions[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing pending plugin interaction: %#v", snapshot.PendingInteractions)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPluginUIDemoUsesSharedSessionBrokerWithoutModelRun(t *testing.T) {
	client, id, command := pluginDialogClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- client.ExecutePluginCommand(ctx, id, protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance, Args: "initial note"})
	}()
	previous := ""
	for step, kind := range []protocol.InteractionKind{protocol.InteractionSelect, protocol.InteractionSelect, protocol.InteractionInput, protocol.InteractionConfirm} {
		request := nextPluginInteraction(t, client, id, previous)
		previous = request.ID
		if request.Kind != kind || request.Plugin == nil || request.Plugin.PluginID != "ui-api-demo" || request.Plugin.Instance != commandOwnerInstance(command.Instance) || request.RunID != "" || request.ToolCallID != "" {
			t.Fatalf("plugin-owned request = %#v", request)
		}
		response := protocol.InteractionResponse{RequestID: request.ID}
		switch kind {
		case protocol.InteractionSelect:
			if request.Filterable == nil || !*request.Filterable || request.Placeholder == "" {
				t.Fatalf("select metadata = %#v", request)
			}
			response.SelectedOptionID = request.Options[0].ID
		case protocol.InteractionInput:
			if request.InitialValue != "initial note" || request.Placeholder != "Demo note..." {
				t.Fatalf("input metadata = %#v", request)
			}
			value := ""
			response.Value = &value
		case protocol.InteractionConfirm:
			if request.ConfirmLabel != "Show toast" || request.CancelLabel != "Cancel" || request.DefaultValue == nil || !*request.DefaultValue {
				t.Fatalf("confirm metadata = %#v", request)
			}
			value := true
			response.Confirmed = &value
		}
		if step == 0 {
			outcomes := make(chan error, 2)
			for range 2 {
				go func() { outcomes <- client.RespondInteraction(ctx, id, response) }()
			}
			successes := 0
			for range 2 {
				err := <-outcomes
				if err == nil {
					successes++
				} else {
					var failure *APIError
					if !errors.As(err, &failure) || failure.StatusCode != http.StatusConflict {
						t.Fatalf("duplicate response = %v", err)
					}
				}
			}
			if successes != 1 {
				t.Fatalf("successful shared answers = %d", successes)
			}
		} else if err := client.RespondInteraction(ctx, id, response); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("UI command did not finish")
	}
	snapshot, err := client.GetSessionSnapshot(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.PendingInteractions) != 0 || len(snapshot.Messages) != 0 || snapshot.ActiveRunID != "" {
		t.Fatalf("completed UI state = %#v", snapshot)
	}
}

func TestPluginDialogRevocationRejectsOldAnswer(t *testing.T) {
	client, id, command := pluginDialogClient(t)
	result := make(chan error, 1)
	go func() {
		result <- client.ExecutePluginCommand(t.Context(), id, protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance})
	}()
	request := nextPluginInteraction(t, client, id, "")
	if _, err := client.ReloadSession(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if err := client.RespondInteraction(t.Context(), id, protocol.InteractionResponse{RequestID: request.ID, SelectedOptionID: request.Options[0].ID}); err == nil {
		t.Fatal("revoked dialog accepted answer")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("revoked command succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("revoked command remained blocked")
	}
	snapshot, err := client.GetSessionSnapshot(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.PendingInteractions) != 0 {
		t.Fatalf("revoked interactions = %#v", snapshot.PendingInteractions)
	}
}

func TestPluginDialogRemainsAnswerableByAnotherClient(t *testing.T) {
	client, id, command := pluginDialogClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- client.ExecutePluginCommand(ctx, id, protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance})
	}()
	request := nextPluginInteraction(t, client, id, "")
	client.http.CloseIdleConnections()
	// No client event subscription owns this pending dialog. A fresh connection
	// observes the same request and can explicitly cancel it for all clients.
	other := NewClient(client.paths)
	defer other.http.CloseIdleConnections()
	recovered := nextPluginInteraction(t, other, id, "")
	if recovered.ID != request.ID || recovered.Plugin == nil || *recovered.Plugin != *request.Plugin {
		t.Fatalf("reattached request = %#v", recovered)
	}
	if err := other.RespondInteraction(ctx, id, protocol.InteractionResponse{RequestID: request.ID, Cancelled: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("explicit user cancellation should complete the demo: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("cancelled dialog did not release command")
	}
	snapshot, err := other.GetSessionSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.PendingInteractions) != 0 {
		t.Fatalf("cancelled dialog still pending: %#v", snapshot.PendingInteractions)
	}
}

func TestDeletingSessionCancelsPendingPluginDialog(t *testing.T) {
	client, id, command := pluginDialogClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- client.ExecutePluginCommand(ctx, id, protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance})
	}()
	nextPluginInteraction(t, client, id, "")
	if err := client.DeleteSession(ctx, id); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("deleted session's plugin command succeeded")
		}
	case <-ctx.Done():
		t.Fatal("session deletion left plugin callback blocked")
	}
}
