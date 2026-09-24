package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestMCPStatusMetadataTracksAuthoritativeSnapshots(t *testing.T) {
	state := &appState{}
	state.applySessionMetadataBaseline(protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "session_1"}, EventStreamID: "stream_1", EventCursor: 1,
		MCPServers:  []protocol.MCPServerStatus{{Name: "docs", State: "authorizing", Transport: "http", Source: "kit-user"}},
		MCPWarnings: []string{"User config: invalid MCP configuration"},
	})
	if len(state.mcpServers) != 1 || state.mcpServers[0].State != "authorizing" || len(state.mcpWarnings) != 1 {
		t.Fatalf("MCP metadata = servers:%+v warnings:%+v", state.mcpServers, state.mcpWarnings)
	}
}

func TestMCPStatusDialogPresentsServerStateAndWarnings(t *testing.T) {
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, MCPStatusOpen: true, Scroll: &ui.ScrollController{},
		MCPServers: []protocol.MCPServerStatus{
			{Name: "everything", State: "configured", Transport: "stdio", Description: "Local test tools", Source: "kit-project", ConfigPath: "/repo/.agents/mcp.json"},
			{Name: "granola", State: "connected", Transport: "http", ToolCount: 6, OAuthSaved: true, Description: "Meeting notes", Source: "kit-project", ConfigPath: "/repo/.agents/mcp.json"},
			{Name: "broken", State: "error", Transport: "http", LastError: "Connection failed.", Source: "shared-project", ConfigPath: "/repo/.mcp.json"},
		},
		MCPWarnings: []string{"/repo/.mcp.json: invalid MCP configuration"},
	}})
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	for _, expected := range []string{
		"MCP servers", "○ everything", "STDIO · configured · 0 tools", "✓ granola", "HTTP · connected · 6 tools · OAuth saved",
		"✗ broken", "Connection failed.", "↑↓ scroll · esc close",
	} {
		if !strings.Contains(strings.Join(rows, "\n"), expected) {
			t.Fatalf("missing %q:\n%s", expected, strings.Join(rows, "\n"))
		}
	}
	for range 20 {
		application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	}
	application.Pump(80, 24)
	rows = paintedRows(application, 80, 24)
	for _, expected := range []string{"Warnings", "▲ /repo/.mcp.json: invalid MCP configuration", "↑↓ scroll · esc close"} {
		if !strings.Contains(strings.Join(rows, "\n"), expected) {
			t.Fatalf("missing scrolled %q:\n%s", expected, strings.Join(rows, "\n"))
		}
	}
}

func TestMCPStatusDialogKeyboardScrollRevealsBoundedList(t *testing.T) {
	servers := make([]protocol.MCPServerStatus, 10)
	for index := range servers {
		servers[index] = protocol.MCPServerStatus{Name: fmt.Sprintf("server-%02d", index), State: "configured", Transport: "stdio", Source: "kit-user"}
	}
	application := uitest.New(shellView{Snapshot: shellSnapshot{Phase: phaseReady, MCPStatusOpen: true, MCPServers: servers, Scroll: &ui.ScrollController{}}})
	application.Pump(60, 16)
	for range 50 {
		application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	}
	application.Pump(60, 16)
	rows := paintedRows(application, 60, 16)
	if findPaintedRow(rows, "server-09") < 0 {
		t.Fatalf("keyboard scroll did not reveal final server:\n%s", strings.Join(rows, "\n"))
	}
}

func TestMCPStatusDialogShowsQuietEmptyState(t *testing.T) {
	application := uitest.New(shellView{Snapshot: shellSnapshot{Phase: phaseReady, MCPStatusOpen: true, Scroll: &ui.ScrollController{}}})
	application.Pump(60, 16)
	rows := paintedRows(application, 60, 16)
	row := findPaintedRow(rows, "No MCP servers are configured.")
	if row < 0 || strings.TrimSpace(rows[row]) != "│       No MCP servers are configured.         │" {
		t.Fatalf("empty state row = %q\n%s", func() string {
			if row < 0 {
				return "<missing>"
			}
			return strings.TrimSpace(rows[row])
		}(), strings.Join(rows, "\n"))
	}
}
