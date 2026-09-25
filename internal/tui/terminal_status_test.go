package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
)

func TestFormatTerminalTitleShowsSessionContextAndStatus(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		sessionName string
		cwd         string
		state       terminalStatusState
		want        string
	}{
		{name: "idle", sessionName: "Fix pager", cwd: "/work/kit", state: terminalStatusIdle, want: "kit - Fix pager - kit"},
		{name: "running", sessionName: "Fix pager", cwd: "/work/kit", state: terminalStatusRunning, want: "⠋ kit - Fix pager - kit"},
		{name: "feedback", sessionName: "Fix pager", cwd: "/work/kit", state: terminalStatusFeedback, want: "? kit - Fix pager - kit"},
		{name: "unnamed", cwd: "/work/kit", state: terminalStatusRunning, want: "⠋ kit - kit"},
		{name: "unknown cwd", sessionName: "Setup", state: terminalStatusFeedback, want: "? kit - Setup"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := formatTerminalTitle(test.sessionName, test.cwd, test.state); got != test.want {
				t.Fatalf("formatTerminalTitle() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFormatTerminalTitleRemovesControlCharacters(t *testing.T) {
	t.Parallel()

	got := formatTerminalTitle("Review\x1b]2;ow\u202ened\a", "/work/ki\nt", terminalStatusIdle)
	if strings.ContainsAny(got, "\x1b\a\n\u202e") || got != "kit - Review]2;owned - kit" {
		t.Fatalf("formatTerminalTitle() = %q", got)
	}
}

func TestResolveTerminalStatusPrioritizesFeedback(t *testing.T) {
	t.Parallel()

	if got := resolveTerminalStatus(true, true); got != terminalStatusFeedback {
		t.Fatalf("running feedback status = %d, want feedback", got)
	}
	if got := resolveTerminalStatus(true, false); got != terminalStatusRunning {
		t.Fatalf("running status = %d, want running", got)
	}
	if got := resolveTerminalStatus(false, false); got != terminalStatusIdle {
		t.Fatalf("idle status = %d, want idle", got)
	}
	if got := resolveTerminalStatus(false, true); got != terminalStatusIdle {
		t.Fatalf("feedback without an active turn = %d, want idle", got)
	}
}

func TestTerminalStatusReporterAnimatesRunningTitleAtBoundedCadence(t *testing.T) {
	t.Parallel()

	var titles []string
	reporter := &terminalStatusReporter{titleFrameDuration: terminalTitleFrameDuration}
	setTitle := func(title string) { titles = append(titles, title) }
	started := time.Unix(10, 0)
	reporter.Update(started, "Animate", "/work/kit", "run_1", terminalStatusRunning, setTitle)
	reporter.Update(started.Add(159*time.Millisecond), "Animate", "/work/kit", "run_1", terminalStatusRunning, setTitle)
	reporter.Update(started.Add(160*time.Millisecond), "Animate", "/work/kit", "run_1", terminalStatusRunning, setTitle)
	reporter.Update(started.Add(320*time.Millisecond), "Animate", "/work/kit", "run_1", terminalStatusRunning, setTitle)
	reporter.Update(started.Add(time.Second), "Animate", "/work/kit", "run_1", terminalStatusFeedback, setTitle)
	reporter.Update(started.Add(2*time.Second), "Animate", "/work/kit", "run_1", terminalStatusRunning, setTitle)

	want := []string{
		"⠋ kit - Animate - kit",
		"⠹ kit - Animate - kit",
		"⠼ kit - Animate - kit",
		"? kit - Animate - kit",
		"⠋ kit - Animate - kit",
	}
	if len(titles) != len(want) {
		t.Fatalf("titles = %#v, want %#v", titles, want)
	}
	for index := range want {
		if titles[index] != want[index] {
			t.Fatalf("title %d = %q, want %q", index, titles[index], want[index])
		}
	}
}

func TestTerminalStatusReporterRestartsAnimationForReplacementRun(t *testing.T) {
	t.Parallel()

	var titles []string
	reporter := &terminalStatusReporter{titleFrameDuration: terminalTitleFrameDuration}
	setTitle := func(title string) { titles = append(titles, title) }
	started := time.Unix(10, 0)
	reporter.Update(started, "Animate", "/work/kit", "run_1", terminalStatusRunning, setTitle)
	reporter.Update(started.Add(160*time.Millisecond), "Animate", "/work/kit", "run_1", terminalStatusRunning, setTitle)
	reporter.Update(started.Add(161*time.Millisecond), "Animate", "/work/kit", "run_2", terminalStatusRunning, setTitle)

	want := []string{
		"⠋ kit - Animate - kit",
		"⠹ kit - Animate - kit",
		"⠋ kit - Animate - kit",
	}
	if len(titles) != len(want) {
		t.Fatalf("titles = %#v, want %#v", titles, want)
	}
	for index := range want {
		if titles[index] != want[index] {
			t.Fatalf("title %d = %q, want %q", index, titles[index], want[index])
		}
	}
}

func TestTerminalStatusReporterKeepsAnimationPhaseWhenAdmissionGetsID(t *testing.T) {
	t.Parallel()

	var titles []string
	reporter := &terminalStatusReporter{titleFrameDuration: terminalTitleFrameDuration}
	setTitle := func(title string) { titles = append(titles, title) }
	started := time.Unix(10, 0)
	reporter.Update(started, "Animate", "/work/kit", "", terminalStatusRunning, setTitle)
	reporter.Update(started.Add(160*time.Millisecond), "Animate", "/work/kit", "", terminalStatusRunning, setTitle)
	reporter.Update(started.Add(161*time.Millisecond), "Animate", "/work/kit", "run_1", terminalStatusRunning, setTitle)
	reporter.Update(started.Add(320*time.Millisecond), "Animate", "/work/kit", "run_1", terminalStatusRunning, setTitle)

	want := []string{
		"⠋ kit - Animate - kit",
		"⠹ kit - Animate - kit",
		"⠼ kit - Animate - kit",
	}
	if len(titles) != len(want) {
		t.Fatalf("titles = %#v, want %#v", titles, want)
	}
	for index := range want {
		if titles[index] != want[index] {
			t.Fatalf("title %d = %q, want %q", index, titles[index], want[index])
		}
	}
}

func TestTerminalStatusReporterEmitsTransitionsAndRestoresIdle(t *testing.T) {
	t.Parallel()

	var titles, writes []string
	reporter := &terminalStatusReporter{
		write: func(sequence string) bool {
			writes = append(writes, sequence)
			return true
		},
		progressSupported: true,
	}
	setTitle := func(title string) { titles = append(titles, title) }
	reporter.Update(time.Time{}, "Fix pager", "/work/kit", "run_1", terminalStatusRunning, setTitle)
	reporter.Update(time.Time{}, "Fix pager", "/work/kit", "run_1", terminalStatusRunning, setTitle)
	reporter.Update(time.Time{}, "Fix pager", "/work/kit", "run_1", terminalStatusFeedback, setTitle)
	reporter.Bell()
	reporter.Close()

	wantTitles := []string{"⠋ kit - Fix pager - kit", "? kit - Fix pager - kit"}
	if len(titles) != len(wantTitles) || titles[0] != wantTitles[0] || titles[1] != wantTitles[1] {
		t.Fatalf("titles = %#v, want %#v", titles, wantTitles)
	}
	wantWrites := []string{
		terminalProgressSequence(terminalProgressIndeterminate),
		terminalProgressSequence(terminalProgressPaused),
		"\a",
		terminalTitleSequence("kit - Fix pager - kit") + terminalProgressSequence(terminalProgressRemove),
	}
	if len(writes) != len(wantWrites) {
		t.Fatalf("writes = %#v, want %#v", writes, wantWrites)
	}
	for index := range wantWrites {
		if writes[index] != wantWrites[index] {
			t.Fatalf("write %d = %q, want %q", index, writes[index], wantWrites[index])
		}
	}
}

func TestTerminalStatusReporterSkipsUnsupportedProgress(t *testing.T) {
	t.Parallel()

	var titles, writes []string
	reporter := &terminalStatusReporter{write: func(sequence string) bool {
		writes = append(writes, sequence)
		return true
	}}
	reporter.Update(time.Time{}, "", "/work/kit", "run_1", terminalStatusRunning, func(title string) { titles = append(titles, title) })
	if len(titles) != 1 || titles[0] != "⠋ kit - kit" || len(writes) != 0 {
		t.Fatalf("titles = %#v writes = %#v", titles, writes)
	}
}

func TestTerminalStatusReporterRetriesFailedProgressWrite(t *testing.T) {
	t.Parallel()

	attempts := 0
	reporter := &terminalStatusReporter{
		progressSupported: true,
		write: func(string) bool {
			attempts++
			return attempts > 1
		},
	}
	now := time.Unix(1, 0)
	reporter.Update(now, "", "/work/kit", "run_1", terminalStatusRunning, func(string) {})
	reporter.Update(now.Add(100*time.Millisecond), "", "/work/kit", "run_1", terminalStatusRunning, func(string) {})
	if attempts != 1 {
		t.Fatalf("attempts before retry deadline = %d, want 1", attempts)
	}
	reporter.Update(now.Add(250*time.Millisecond), "", "/work/kit", "run_1", terminalStatusRunning, func(string) {})
	if attempts != 2 {
		t.Fatalf("attempts after retry deadline = %d, want 2", attempts)
	}
}

func TestTerminalStatusReporterBoundsPersistentProgressFailures(t *testing.T) {
	t.Parallel()

	attempts := 0
	reporter := &terminalStatusReporter{
		progressSupported: true,
		write: func(string) bool {
			attempts++
			return false
		},
	}
	now := time.Unix(1, 0)
	for _, elapsed := range []time.Duration{0, 250 * time.Millisecond, 750 * time.Millisecond, 2 * time.Second} {
		reporter.Update(now.Add(elapsed), "", "/work/kit", "run_1", terminalStatusRunning, func(string) {})
	}
	if attempts != 3 {
		t.Fatalf("persistent failure attempts = %d, want 3", attempts)
	}
}

func TestAppTerminalStatusUsesAttachedSessionAndFeedbackPrecedence(t *testing.T) {
	t.Parallel()

	var titles []string
	reporter := &terminalStatusReporter{}
	state := appState{
		phase: phaseReady, runPending: true, terminalRunActive: true,
		session:        protocol.SessionInfo{Name: "Status work", CWD: "/work/kit"},
		terminalStatus: reporter,
	}
	setTitle := func(title string) { titles = append(titles, title) }
	state.syncTerminalStatus(time.Time{}, setTitle)
	state.phase = phaseAuthSelect
	state.syncTerminalStatus(time.Time{}, setTitle)
	state.agentFeedbackPending = true
	state.syncTerminalStatus(time.Time{}, setTitle)
	if len(titles) != 2 || titles[0] != "⠋ kit - Status work - kit" || titles[1] != "? kit - Status work - kit" {
		t.Fatalf("titles = %#v", titles)
	}
}

func TestAppTerminalStatusLeavesNonAgentPromptsIdle(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		state *appState
	}{
		{name: "authentication", state: &appState{phase: phaseAuthSelect}},
		{name: "startup failure", state: &appState{phase: phaseFailed}},
		{name: "attached rename", state: &appState{phase: phaseReady, sessionRename: currentSessionRenameController{Open: true}}},
		{name: "explorer rename", state: &appState{phase: phaseReady, sessionExplorer: sessionExplorerController{Open: true, RenameOpen: true}}},
		{name: "delete confirmation", state: &appState{phase: phaseReady, sessionExplorer: sessionExplorerController{Open: true, DeleteOpen: true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var title string
			test.state.terminalCWD = "/work/kit"
			test.state.terminalStatus = &terminalStatusReporter{}
			test.state.syncTerminalStatus(time.Time{}, func(value string) { title = value })
			if title != "kit - kit" {
				t.Fatalf("title = %q, want %q", title, "kit - kit")
			}
		})
	}
}

func TestSupportsTerminalProgressUsesGhosttyOutsideMultiplexers(t *testing.T) {
	t.Parallel()

	environment := map[string]string{"TERM_PROGRAM": "ghostty"}
	getenv := func(name string) string { return environment[name] }
	if got := terminalTitleAnimationDuration(getenv); got != terminalTitleFrameDuration {
		t.Fatalf("direct title cadence = %s, want %s", got, terminalTitleFrameDuration)
	}
	if !supportsTerminalProgress(getenv) {
		t.Fatal("Ghostty was not detected")
	}
	environment["TMUX"] = "/tmp/tmux"
	if got := terminalTitleAnimationDuration(getenv); got != terminalMultiplexerFrameDuration {
		t.Fatalf("tmux title cadence = %s, want %s", got, terminalMultiplexerFrameDuration)
	}
	if supportsTerminalProgress(getenv) {
		t.Fatal("tmux should disable raw progress sequences")
	}
	delete(environment, "TMUX")
	environment["TERM_PROGRAM"] = ""
	environment["TERM"] = "xterm-ghostty"
	if !supportsTerminalProgress(getenv) {
		t.Fatal("Ghostty TERM was not detected")
	}
	environment["STY"] = "123.screen"
	if supportsTerminalProgress(getenv) {
		t.Fatal("screen should disable raw progress sequences")
	}
}
