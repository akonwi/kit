package tui

import (
	"strings"
	"testing"

	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestRunningWorkspaceTabUsesAnimatedSpinner(t *testing.T) {
	if _, ok := workspaceActivityMarker(workspacePaneActivityRunning, ui.Style{}).(spinner); !ok {
		t.Fatal("running workspace marker is not the shared animated spinner")
	}
	application := uitest.New(workspaceTab{Label: "reviewer", Activity: workspacePaneActivityRunning})
	application.Pump(30, 1)
	if row := strings.TrimSpace(paintedRows(application, 30, 1)[0]); !strings.Contains(row, "reviewer") || !strings.Contains(row, spinnerFrames[0]) {
		t.Fatalf("running workspace tab = %q", row)
	}
}

func TestRunningSubagentPickerRowUsesAnimatedSpinner(t *testing.T) {
	if _, ok := subagentStatusWidget("running", glyphCircleFilled, ui.Style{}).(spinner); !ok {
		t.Fatal("running subagent status is not the shared animated spinner")
	}
	theme := ui.DefaultTheme()
	presentation := pickerRowPresentation{Theme: theme, ItemText: theme.Foreground, FocusedText: theme.Foreground, FocusedBg: theme.Surface}
	application := uitest.New((shellView{}).subagentRosterRow(theme, presentation, subagentRosterItem{
		Name: "reviewer", Description: "Review code", Status: "running",
	}, true))
	application.Pump(50, 2)
	if row := paintedRows(application, 50, 2)[0]; !strings.Contains(row, spinnerFrames[0]) || !strings.Contains(row, "reviewer") {
		t.Fatalf("running subagent picker row = %q", row)
	}
}
