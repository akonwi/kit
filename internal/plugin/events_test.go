package plugin

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

func readTurnEvents(t *testing.T, root string) []rpcMessage {
	t.Helper()
	file, err := os.Open(filepath.Join(root, "turn-events.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var result []rpcMessage
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			t.Fatal(err)
		}
		result = append(result, message)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHostTurnEventsAreOrderedAndGenerationFenced(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	root := hostInstallation(t, home.Plugins, "events", "events", "events")
	host := testHost(t, home, t.TempDir())
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })

	host.TurnStarted("turn-one")
	host.TurnCompleted(PublicTurn{ID: "turn-one", Messages: []PublicMessage{{Role: "user", Content: []PublicTextContent{{Type: "text", Text: "first"}, {Type: "text", Text: "second"}}}, {Role: "assistant", Content: []PublicTextContent{{Type: "text", Text: "answer"}}}}}, "")
	eventuallyHost(t, func() bool { return len(readTurnEvents(t, root)) == 2 })
	events := readTurnEvents(t, root)
	if events[0].ID != nil || events[1].ID != nil || events[0].Method == nil || *events[0].Method != turnStartedMethod || events[1].Method == nil || *events[1].Method != turnCompletedMethod {
		t.Fatalf("events = %#v", events)
	}
	var completed turnCompletedParams
	if err := json.Unmarshal(events[1].Params, &completed); err != nil {
		t.Fatal(err)
	}
	if completed.SessionID != "session-one" || completed.Turn.ID != "turn-one" || len(completed.Turn.Messages) != 2 || len(completed.Turn.Messages[0].Content) != 2 || completed.Turn.Messages[0].Content[1].Text != "second" {
		t.Fatalf("completed = %#v", completed)
	}

	host.TurnStarted("turn-old-generation")
	eventuallyHost(t, func() bool { return len(readTurnEvents(t, root)) == 3 })
	generation := host.Instances()[0].ID.Generation
	host.Reload()
	eventuallyHost(t, func() bool { return readyHost(host, 1) && host.Instances()[0].ID.Generation > generation })
	host.TurnCompleted(PublicTurn{ID: "turn-old-generation"}, "")
	host.TurnStarted("turn-new-generation")
	host.TurnCompleted(PublicTurn{ID: "turn-new-generation"}, "")
	eventuallyHost(t, func() bool { return len(readTurnEvents(t, root)) == 5 })
	events = readTurnEvents(t, root)
	var last turnCompletedParams
	if err := json.Unmarshal(events[4].Params, &last); err != nil {
		t.Fatal(err)
	}
	if events[3].Method == nil || *events[3].Method != turnStartedMethod || events[4].Method == nil || *events[4].Method != turnCompletedMethod || last.Turn.ID != "turn-new-generation" {
		t.Fatalf("post-reload events = %#v", events[3:])
	}
}

func TestHostTurnEventsAreLiveOnlyAndOversizeIsOmitted(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	root := hostInstallation(t, home.Plugins, "events", "events", "hold-init")
	host := testHost(t, home, t.TempDir())
	host.Start()
	eventuallyHost(t, func() bool { _, err := os.Stat(filepath.Join(root, "initial.json")); return err == nil })
	host.TurnStarted("turn-before-ready")
	host.TurnCompleted(PublicTurn{ID: "turn-before-ready"}, "")
	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	if events := readTurnEvents(t, root); len(events) != 0 {
		t.Fatalf("initialization replayed events: %#v", events)
	}
	host.TurnStarted("turn-large")
	host.TurnCompleted(PublicTurn{ID: "turn-large", Messages: []PublicMessage{{Role: "assistant", Content: []PublicTextContent{{Type: "text", Text: strings.Repeat("x", maxTurnEventFrameSize)}}}}}, "")
	eventuallyHost(t, func() bool { return len(readTurnEvents(t, root)) == 1 })
	if warnings := host.Warnings(); len(warnings) != 1 || warnings[0] == "" {
		t.Fatalf("warnings = %v", warnings)
	}
}
