package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestPluginToastWireReaderBoundsAndValidatesFrames(t *testing.T) {
	expected := protocol.PluginToast{PluginID: "demo", Instance: "owner:1", Title: "Notice", Subtitle: "first\nsecond", Variant: "warning"}
	encoded, _ := json.Marshal(expected)
	var received []protocol.PluginToast
	if err := ReadPluginToasts(strings.NewReader("\n"+string(encoded)+"\n\n"), func(toast protocol.PluginToast) error { received = append(received, toast); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 || received[0] != expected {
		t.Fatalf("decoded = %#v", received)
	}
	for _, frame := range []string{`{}`, `{"pluginId":"demo","instance":"owner:1","title":"\u001b","variant":"info"}`, string(encoded) + ` {}`, strings.Replace(string(encoded), "Notice", "\xff", 1), `{"unknown":true}`, strings.Repeat(" ", 32*1024) + "\n"} {
		if err := ReadPluginToasts(strings.NewReader(frame), func(protocol.PluginToast) error { t.Error("invalid frame delivered"); return nil }); err == nil {
			t.Fatalf("accepted invalid frame of %d bytes", len(frame))
		}
	}
}
