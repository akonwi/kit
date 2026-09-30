package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/protocol"
)

func pluginToastRecord(data string) string { return "event: plugin.toast\ndata: " + data + "\n\n" }

func TestPluginToastWireReaderDeliversRecordsAndSkipsComments(t *testing.T) {
	t.Parallel()
	expected := protocol.PluginToast{PluginID: "demo", Instance: "owner:1", Title: "Notice", Subtitle: "first\nsecond", Variant: protocol.PluginToastWarning}
	encoded, _ := json.Marshal(expected)
	body := ": connected\n\n" + pluginToastRecord(string(encoded)) + ": heartbeat\r\n\r\n" + "event: plugin.toast\r\ndata: " + string(encoded) + "\r\n\r\n"
	var received []protocol.PluginToast
	if err := ReadPluginToasts(strings.NewReader(body), func(toast protocol.PluginToast) error { received = append(received, toast); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || received[0] != expected || received[1] != expected {
		t.Fatalf("decoded = %#v", received)
	}
}

func TestPluginToastWireReaderRejectsInvalidRecordsAsProtocolViolations(t *testing.T) {
	t.Parallel()
	valid, _ := json.Marshal(protocol.PluginToast{PluginID: "demo", Instance: "owner:1", Title: "Notice", Variant: protocol.PluginToastInfo})
	tests := map[string]string{
		"empty object":     pluginToastRecord(`{}`),
		"control title":    pluginToastRecord(`{"pluginId":"demo","instance":"owner:1","title":"\u001b","variant":"info"}`),
		"unknown variant":  pluginToastRecord(`{"pluginId":"demo","instance":"owner:1","title":"Notice","variant":"loud"}`),
		"trailing value":   pluginToastRecord(string(valid) + ` {}`),
		"invalid UTF-8":    pluginToastRecord(strings.Replace(string(valid), "Notice", "\xff", 1)),
		"unknown field":    pluginToastRecord(strings.Replace(string(valid), `{`, `{"unknown":true,`, 1)),
		"duplicate member": pluginToastRecord(strings.Replace(string(valid), `{`, `{"title":"Other",`, 1)),
		"wrong record":     "event: vcs.status\ndata: " + string(valid) + "\n\n",
		"record id":        "id: 1\n" + pluginToastRecord(string(valid)),
		"oversized":        pluginToastRecord(string(valid) + strings.Repeat(" ", httpapi.MaxPluginToastRecordBytes)),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			err := ReadPluginToasts(strings.NewReader(body), func(protocol.PluginToast) error {
				t.Error("invalid record delivered")
				return nil
			})
			var violation *StreamError
			if !errors.As(err, &violation) {
				t.Fatalf("err = %v, want protocol violation", err)
			}
		})
	}
}

func TestPluginToastWireReaderEnforcesExactRecordBound(t *testing.T) {
	t.Parallel()
	valid, _ := json.Marshal(protocol.PluginToast{PluginID: "demo", Instance: "owner:1", Title: "Notice", Variant: protocol.PluginToastInfo})
	record := pluginToastRecord(string(valid))
	// Record size counts field lines and their terminators, not the blank line.
	padding := strings.Repeat(" ", httpapi.MaxPluginToastRecordBytes-(len(record)-1))
	maximum := strings.Replace(record, string(valid), string(valid)+padding, 1)
	called := false
	if err := ReadPluginToasts(strings.NewReader(maximum), func(protocol.PluginToast) error { called = true; return nil }); err != nil || !called {
		t.Fatalf("maximum record: called=%t err=%v", called, err)
	}
	over := strings.Replace(record, string(valid), string(valid)+padding+" ", 1)
	var violation *StreamError
	if err := ReadPluginToasts(strings.NewReader(over), func(protocol.PluginToast) error { t.Error("oversized record delivered"); return nil }); !errors.As(err, &violation) {
		t.Fatalf("oversized record: err = %v", err)
	}
}
