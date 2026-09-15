package tui

import (
	"fmt"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestWorkspaceHostUsesSameFullWidthTabbedGeometryAtEveryWidth(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 140} {
		width := width
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			t.Parallel()
			layout := &workspaceLayoutState{}
			application := uitest.New(conversationWorkspaceHost{
				Open: true, ActivitySelected: true,
				Tabs: ui.Text{Value: "Agent  [reviewer ×]"}, Transcript: ui.Text{Value: "AGENT CONTENT"},
				Activity: ui.Text{Value: "SUBAGENT CONTENT"}, Pending: ui.Text{Value: "PENDING"}, PendingHeight: 1,
				ComposerSeparator: ui.Text{Value: strings.Repeat("─", width)},
				Composer:          ui.Text{Value: "COMPOSER"}, ComposerHeightLimit: 1, LayoutState: layout,
			})
			application.Pump(width, 10)
			rows := paintedRows(application, width, 10)
			want := map[int]string{
				0: "Agent  [reviewer ×]",
				2: "SUBAGENT CONTENT",
				7: "PENDING",
				8: strings.Repeat("─", width),
				9: "COMPOSER",
			}
			for row, prefix := range want {
				if !strings.HasPrefix(rows[row], prefix) {
					t.Fatalf("row %d = %q, want prefix %q\n%s", row, rows[row], prefix, strings.Join(rows, "\n"))
				}
			}
			if layout.TranscriptVisible || !layout.ActivityVisible {
				t.Fatalf("layout state = %+v, want one full-width activity surface", *layout)
			}
		})
	}
}

func TestWorkspaceHostOmitsTabsForAgentOnlyShell(t *testing.T) {
	t.Parallel()
	layout := &workspaceLayoutState{}
	application := uitest.New(conversationWorkspaceHost{
		Tabs: ui.Text{Value: "UNEXPECTED TAB STRIP"}, Transcript: ui.Text{Value: "AGENT CONTENT"},
		Activity: ui.Text{Value: "UNEXPECTED ACTIVITY"}, Pending: ui.Text{Value: "PENDING"}, PendingHeight: 1,
		ComposerSeparator: ui.Text{Value: strings.Repeat("─", 80)},
		Composer:          ui.Text{Value: "COMPOSER"}, ComposerHeightLimit: 1, LayoutState: layout,
	})
	application.Pump(80, 8)
	rows := paintedRows(application, 80, 8)
	want := map[int]string{
		0: "AGENT CONTENT",
		5: "PENDING",
		6: strings.Repeat("─", 80),
		7: "COMPOSER",
	}
	for row, prefix := range want {
		if !strings.HasPrefix(rows[row], prefix) {
			t.Fatalf("row %d = %q, want prefix %q\n%s", row, rows[row], prefix, strings.Join(rows, "\n"))
		}
	}
	if !layout.TranscriptVisible || layout.ActivityVisible {
		t.Fatalf("layout state = %+v, want Agent only", *layout)
	}
}
