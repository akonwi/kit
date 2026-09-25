package server

import (
	"github.com/akonwi/kit/internal/protocol"
	"testing"
	"time"
)

func TestPluginFooterFixtureProjectsAndRevokesSharedChrome(t *testing.T) {
	client, id, command := pluginFixtureClient(t, "footer-demo", 3)
	invoke := func(local, args string) {
		t.Helper()
		commandID := "footer-demo." + local
		snapshot, err := client.GetSessionSnapshot(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range snapshot.PluginCommands {
			if candidate.ID == commandID {
				if err := client.ExecutePluginCommand(t.Context(), id, protocol.PluginCommandInput{ID: commandID, Instance: candidate.Instance, Args: args}); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("command %s unavailable", commandID)
	}
	wait := func(text string, hidden bool) protocol.SessionSnapshot {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			snapshot, err := client.GetSessionSnapshot(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			footer := snapshot.PluginFooter
			if footer != nil && footer.LocationHidden == hidden {
				if text == "" && len(footer.Items) == 0 {
					return snapshot
				}
				if len(footer.Items) == 1 && len(footer.Items[0].Content) == 1 && footer.Items[0].Content[0].Text == text {
					return snapshot
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("expected text=%q hidden=%v; footer=%#v", text, hidden, footer)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	baseline := wait("", false)
	invoke("set", "Ready")
	visible := wait("Ready", false)
	item := visible.PluginFooter.Items[0]
	if item.ID != "footer-demo.status" || item.PluginID != "footer-demo" || item.Instance != commandOwnerInstance(command.Instance) || item.Content[0].Style.FG != "toolText" || !item.Content[0].Style.Bold {
		t.Fatalf("wire footer = %#v", item)
	}
	if visible.ActiveRunID != "" || len(visible.Messages) != 0 {
		t.Fatal("footer command fabricated model work")
	}
	// Catalog invalidation must also carry footer-only changes to live clients.
	deadline := time.Now().Add(5 * time.Second)
	for visible.EventStreamID == baseline.EventStreamID {
		if time.Now().After(deadline) {
			t.Fatal("footer update did not invalidate metadata stream")
		}
		time.Sleep(5 * time.Millisecond)
		var err error
		visible, err = client.GetSessionSnapshot(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
	}
	invoke("replace", "Replacement")
	wait("Replacement", true)
	invoke("clear", "")
	wait("", false)
	invoke("replace", "Before reload")
	wait("Before reload", true)
	if _, err := client.ReloadSession(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	wait("", false)
}
