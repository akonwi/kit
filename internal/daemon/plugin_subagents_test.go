package daemon

import (
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
)

func TestPluginSubagentFixtureProjectsStartsAndUnregisters(t *testing.T) {
	client, sessionID, command := pluginFixtureClient(t, "subagent-demo", 1)
	var snapshot protocol.SessionSnapshot
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		snapshot, err = client.GetSessionSnapshot(t.Context(), sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.SubagentDefinitions) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(snapshot.SubagentDefinitions) != 1 {
		t.Fatalf("plugin subagent definitions = %#v", snapshot.SubagentDefinitions)
	}
	definition := snapshot.SubagentDefinitions[0]
	if definition.Name != "subagent-demo.reviewer" || definition.Description != "Reviews code changes for regressions" || definition.Source.Kind != "plugin" || definition.Source.PluginID != "subagent-demo" || definition.Source.Path == "" {
		t.Fatalf("plugin subagent definition = %#v", definition)
	}
	started, err := client.Subagent(t.Context(), sessionID, protocol.SubagentOperationInput{Action: protocol.SubagentStart, Agent: definition.Name, Message: "Return a concise review."})
	if err != nil || started.Conversation == nil || started.Conversation.AgentName != definition.Name {
		t.Fatalf("start plugin subagent = %#v, %v", started, err)
	}
	if err := client.ExecutePluginCommand(t.Context(), sessionID, protocol.PluginCommandInput{ID: "subagent-demo.clear", Instance: command.Instance}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = client.GetSessionSnapshot(t.Context(), sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.SubagentDefinitions) == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(snapshot.SubagentDefinitions) != 0 {
		t.Fatalf("unregistered plugin definition survived = %#v", snapshot.SubagentDefinitions)
	}
	inspected, err := client.Subagent(t.Context(), sessionID, protocol.SubagentOperationInput{Action: protocol.SubagentInspect, Agent: definition.Name})
	if err != nil || inspected.Conversation == nil || inspected.Conversation.AgentName != definition.Name {
		t.Fatalf("existing plugin conversation after unregister = %#v, %v", inspected, err)
	}
}
