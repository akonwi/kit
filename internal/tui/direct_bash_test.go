package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestBashRecoveryToastDeduplicatesIdenticalRetries(t *testing.T) {
	_, state, _ := mountRunAbort(t)
	state.reportBashRecoveryError("bash_a", "Bash update failed", errors.New("stream closed"))
	state.reportBashRecoveryError("bash_a", "Bash update failed", errors.New("stream closed"))
	state.reportBashRecoveryError("bash_a", "Could not resume bash", errors.New("connection lost"))
	state.reportBashRecoveryError("bash_b", "Bash update failed", errors.New("stream closed"))
	toasts := state.toasts.Snapshot()
	if len(toasts) != 3 {
		t.Fatalf("recovery toasts = %+v, want one per distinct failure and execution", toasts)
	}
	for _, toast := range toasts {
		if !toast.Persistent || toast.Variant != toastError {
			t.Fatalf("recovery toast = %+v, want persistent error", toast)
		}
	}
}

func TestParseDirectBash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   string
		command string
		exclude bool
		ok      bool
	}{
		{input: "!git status", command: "git status", ok: true},
		{input: "!! git status ", command: "git status", exclude: true, ok: true},
		{input: "!!!echo hi", command: "!echo hi", exclude: true, ok: true},
		{input: "!\nprintf ok", command: "printf ok", ok: true},
		{input: "!"},
		{input: "!!", exclude: true},
		{input: " !git status"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			command, exclude, ok := parseDirectBash(test.input)
			if command != test.command || exclude != test.exclude || ok != test.ok {
				t.Fatalf("parseDirectBash(%q) = %q/%v/%v, want %q/%v/%v", test.input, command, exclude, ok, test.command, test.exclude, test.ok)
			}
		})
	}
}

func TestBashHistoryRecallReadsDurableHistoryNotLoadedProjection(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	history := &stubBashHistorySession{pages: []protocol.BashHistoryPage{{
		SessionID: "session_1",
		Entries: []protocol.BashHistoryEntry{
			{ID: "bash_2", Sequence: 2, Command: "git log --oneline", Status: "completed", ExcludeFromContext: true, StartedAt: now},
			{ID: "bash_1", Sequence: 1, Command: "git status", Status: "completed", StartedAt: now},
		},
	}}}
	state := appState{
		composer: "!git",
		// The loaded projection knows only one, older, included execution.
		messages: []transcriptMessage{
			{ID: "bash_1", Role: "bash", Bash: bashExecution("bash_1", "git status", false, "")},
		},
	}
	projection := state.bashHistoryEntries()
	if len(projection) != 1 || projection[0].ID != "bash_1" {
		t.Fatalf("loaded projection entries = %+v, want only the projected execution", projection)
	}
	loaded, hasMore, _, err := loadBashHistoryEntries(t.Context(), history, 0, bashHistoryInitialLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || hasMore {
		t.Fatalf("durable entries = %+v, hasMore=%v", loaded, hasMore)
	}
	if loaded[0].ID != "bash_2" || loaded[0].Command != "git log --oneline" || !loaded[0].ExcludeFromContext {
		t.Fatalf("newest durable entry = %+v", loaded[0])
	}
	if loaded[1].ID != "bash_1" || loaded[1].Command != "git status" || loaded[1].ExcludeFromContext {
		t.Fatalf("older durable entry = %+v", loaded[1])
	}
	if history.calls != 1 || history.lastLimit != bashHistoryInitialLimit {
		t.Fatalf("history read = %d calls, limit %d", history.calls, history.lastLimit)
	}
}

func TestBashHistoryRecallLoadsOlderEntriesOnDemand(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	history := &stubBashHistorySession{}
	// Twelve entries so the initial page of ten leaves older history behind.
	for page := 0; page < 2; page++ {
		var entries []protocol.BashHistoryEntry
		for index := 0; index < 6; index++ {
			sequence := int64(12 - (page*6 + index))
			entries = append(entries, protocol.BashHistoryEntry{
				ID: fmt.Sprintf("bash_%032x", sequence), Sequence: sequence,
				Command: fmt.Sprintf("echo %d", sequence), Status: "completed", StartedAt: now,
			})
		}
		page1 := protocol.BashHistoryPage{SessionID: "session_1", Entries: entries}
		if page == 0 {
			page1.HasMore = true
			page1.NextCursor = strconv.FormatInt(entries[len(entries)-1].Sequence, 10)
		}
		history.pages = append(history.pages, page1)
	}
	entries, hasMore, before, err := loadBashHistoryEntries(t.Context(), history, 0, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 || !hasMore || before != 7 {
		t.Fatalf("initial read = %d entries, hasMore=%v before=%d", len(entries), hasMore, before)
	}
	controller := bashHistoryController{Open: true, Entries: entries, HasMore: hasMore, OlderBefore: before}
	controller.Selection = firstBashHistoryID(controller.filtered())
	older, stillMore, next, err := loadBashHistoryEntries(t.Context(), history, before, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(older) != 6 || stillMore || next != 0 {
		t.Fatalf("older read = %d entries, hasMore=%v next=%d", len(older), stillMore, next)
	}
	controller.MergeOlder(older, next, stillMore)
	if len(controller.Entries) != 12 || controller.HasMore || controller.PagesLoaded != 1 {
		t.Fatalf("merged controller = %d entries, hasMore=%v pages=%d", len(controller.Entries), controller.HasMore, controller.PagesLoaded)
	}
	if controller.Selection != "bash_0000000000000000000000000000000c" {
		t.Fatalf("selection moved while loading older entries: %q", controller.Selection)
	}
	if history.calls != 2 {
		t.Fatalf("history read %d pages, want one read per requested page", history.calls)
	}
}

func TestBashHistoryRecallStopsLoadingBeyondBoundedDepth(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	history := &stubBashHistorySession{}
	for page := 0; page < bashHistoryMaxPages+2; page++ {
		entries := []protocol.BashHistoryEntry{
			{ID: fmt.Sprintf("bash_%032x", page+1), Sequence: int64(page + 1), Command: "echo", Status: "completed", StartedAt: now},
		}
		history.pages = append(history.pages, protocol.BashHistoryPage{
			SessionID: "session_1", Entries: entries,
			NextCursor: strconv.FormatInt(entries[len(entries)-1].Sequence, 10), HasMore: true,
		})
	}
	controller := bashHistoryController{Open: true, HasMore: true, OlderBefore: 1, Entries: []bashHistoryEntry{{ID: "bash_1", Command: "echo"}}}
	controller.Selection = "bash_1"
	loads := 0
	controller.OnExhausted = func() { loads++ }
	if !controller.Exhausted() {
		t.Fatal("exhausted state not detected at the oldest entry")
	}
	controller.PagesLoaded = bashHistoryMaxPages
	if controller.Exhausted() {
		t.Fatal("paging continued beyond the bounded depth")
	}
	if loads != 0 {
		t.Fatalf("exhausted callback ran %d times before navigation", loads)
	}
}

type stubBashHistorySession struct {
	sessionclient.Session
	mu        sync.Mutex
	pages     []protocol.BashHistoryPage
	calls     int
	lastLimit int
}

func (s *stubBashHistorySession) BashHistory(ctx context.Context, before uint64, limit int) (protocol.BashHistoryPage, error) {
	// Honor the deadline the way a real transport does, so a request cancelled
	// by its caller is observable here.
	if err := ctx.Err(); err != nil {
		return protocol.BashHistoryPage{}, err
	}
	s.mu.Lock()
	s.calls++
	s.lastLimit = limit
	s.mu.Unlock()
	index := 0
	if before > 0 {
		for page := 0; page < len(s.pages); page++ {
			if s.pages[page].NextCursor == strconv.FormatUint(before, 10) {
				index = page + 1
				break
			}
		}
	}
	if index >= len(s.pages) {
		return protocol.BashHistoryPage{SessionID: "session_1"}, nil
	}
	return s.pages[index], nil
}

func TestBashHistoryRestoresPerEntryContextMode(t *testing.T) {
	t.Parallel()
	state := appState{
		composer: "!git",
		messages: []transcriptMessage{
			{ID: "bash_old", Role: "bash", Bash: bashExecution("bash_old", "git status", false, "")},
			{ID: "bash_new", Role: "bash", Bash: bashExecution("bash_new", "git log --oneline", true, "")},
		},
	}
	entries := state.bashHistoryEntries()
	if !state.bashHistory.OpenFor(entries, state.composer) {
		t.Fatal("history did not open")
	}
	if state.bashHistory.Query != "git" {
		t.Fatalf("history state = %+v", state.bashHistory)
	}
	entries = state.bashHistory.filtered()
	if len(entries) != 2 || entries[0].ID != "bash_new" {
		t.Fatalf("filtered history = %+v, want newest first", entries)
	}
	selected, ok := state.bashHistory.Selected()
	if !ok || bashHistoryComposerText(selected) != "!!git log --oneline" {
		t.Fatalf("selected history = %+v/%v", selected, ok)
	}
}

func TestReadyShellPresentsBashModeAndExecution(t *testing.T) {
	t.Parallel()
	const width, height = 80, 20
	theme := ui.DefaultTheme()
	execution := bashExecution("bash_1", "git status", false, "On branch main")
	app := uitest.New(ui.Provider[ui.Theme]{Value: theme, Child: shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Composer: "!!git status", Location: "/workspace",
		Messages: []transcriptMessage{{ID: execution.ID, Role: "bash", Bash: execution}},
		Scroll:   &ui.ScrollController{}, BashCollapsed: map[string]bool{},
	}}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "✓ git status") || !strings.Contains(joined, "On branch main") {
		t.Fatalf("bash transcript missing:\n%s", joined)
	}
	if !strings.Contains(rows[height-1], "bash command · result exc") {
		t.Fatalf("footer = %q", rows[height-1])
	}
	separatorRow := height - 4
	if app.Cell(0, separatorRow).Style.Foreground != theme.SuccessText {
		t.Fatalf("composer separator foreground = %v, want %v", app.Cell(0, separatorRow).Style.Foreground, theme.SuccessText)
	}
}

func bashExecution(id, command string, exclude bool, output string) *protocol.BashExecution {
	exitCode := 0
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return &protocol.BashExecution{
		ID: id, SessionID: "session_1", Command: command,
		Status: protocol.BashExecutionCompleted, Output: output, ExitCode: &exitCode,
		ExcludeFromContext: exclude, StartedAt: now, CompletedAt: now,
	}
}

// TestBashHistoryRecallSurvivesReloadWithEmptyProjection pins recall after a
// reload or restart, when the loaded transcript holds no direct shell rows.
// Remembered executions exist only in durable session history, so the picker
// must open on the composer alone and present exactly that history.
func TestBashHistoryRecallSurvivesReloadWithEmptyProjection(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	history := &stubBashHistorySession{pages: []protocol.BashHistoryPage{{
		SessionID: "session_1",
		Entries: []protocol.BashHistoryEntry{
			{ID: "bash_3", Sequence: 3, Command: "echo \"testing\"", Status: "completed", ExcludeFromContext: true, StartedAt: now, CompletedAt: now},
			{ID: "bash_2", Sequence: 2, Command: "go test ./...", Status: "completed", StartedAt: now, CompletedAt: now},
		},
	}}}
	// A reloaded session with no role-"bash" rows in the loaded transcript.
	state := appState{composer: "!", messages: []transcriptMessage{
		{ID: "user_1", TurnID: "turn_1", Role: "user", Text: "hello"},
	}}
	entries := state.bashHistoryEntries()
	if len(entries) != 0 {
		t.Fatalf("loaded projection entries = %+v, want none", entries)
	}
	if !state.bashHistory.OpenFor(entries, state.composer) {
		t.Fatal("picker did not open with an empty loaded projection")
	}
	if !state.bashHistory.Loading {
		t.Fatal("picker opened without an in-flight durable read")
	}

	loaded, hasMore, before, err := loadBashHistoryEntries(t.Context(), history, 0, bashHistoryInitialLimit)
	if err != nil {
		t.Fatal(err)
	}
	if history.calls != 1 || history.lastLimit != bashHistoryInitialLimit {
		t.Fatalf("durable read calls=%d limit=%d", history.calls, history.lastLimit)
	}
	if len(loaded) != 2 || hasMore || before != 0 {
		t.Fatalf("durable entries = %+v hasMore=%v before=%d", loaded, hasMore, before)
	}

	state.bashHistory.Entries = loaded
	state.bashHistory.HasMore, state.bashHistory.OlderBefore = hasMore, before
	state.bashHistory.Loading = false
	state.bashHistory.Selection = firstBashHistoryID(state.bashHistory.filtered())

	rows := state.bashHistory.filtered()
	if len(rows) != 2 {
		t.Fatalf("recalled rows = %+v", rows)
	}
	if rows[0].ID != "bash_3" || rows[0].Command != "echo \"testing\"" || !rows[0].ExcludeFromContext {
		t.Fatalf("newest recalled row = %+v", rows[0])
	}
	if rows[1].ID != "bash_2" || rows[1].Command != "go test ./..." || rows[1].ExcludeFromContext {
		t.Fatalf("second recalled row = %+v", rows[1])
	}
	selected, ok := state.bashHistory.Selected()
	if !ok || selected.ID != "bash_3" {
		t.Fatalf("selected row = %+v present=%v", selected, ok)
	}
	if !strings.HasPrefix(bashHistoryComposerText(selected), "!!") {
		t.Fatalf("composer text = %q, want excluded prefix", bashHistoryComposerText(selected))
	}
}

// TestBashHistoryPickerShowsLoadingUntilDurableAnswer pins that an empty picker
// is not reported as "no results" while the durable read is still in flight.
func TestBashHistoryPickerShowsLoadingUntilDurableAnswer(t *testing.T) {
	t.Parallel()
	state := appState{composer: "!"}
	if !state.bashHistory.OpenFor(nil, state.composer) {
		t.Fatal("picker did not open for a session with no history")
	}
	application := uitest.New(bashHistorySurface{Controller: &state.bashHistory, Composer: state.composer, PrimaryPercent: 100})
	application.Pump(72, 20)
	if !application.Contains("Loading history") {
		t.Fatalf("picker did not present an in-flight durable read:\n%s", application.Text())
	}
}

// latencyBashHistory models a durable read slower than the call that starts it.
type latencyBashHistory struct {
	sessionclient.Session
	delay time.Duration
}

func (l latencyBashHistory) BashHistory(ctx context.Context, _ uint64, _ int) (protocol.BashHistoryPage, error) {
	timer := time.NewTimer(l.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return protocol.BashHistoryPage{SessionID: "session_1", Entries: []protocol.BashHistoryEntry{
			{ID: "bash_1", Sequence: 1, Command: "echo \"testing\"", Status: "completed", ExcludeFromContext: true, StartedAt: "2026-01-01T00:00:00Z", CompletedAt: "2026-01-01T00:00:01Z"},
		}}, nil
	case <-ctx.Done():
		return protocol.BashHistoryPage{}, ctx.Err()
	}
}

// TestBashHistoryReadOutlivesTheFunctionThatStartsIt pins the defect that left
// the picker on "Loading history" forever. The durable read outlives the
// function that starts it, so a caller-owned deferred cancel aborted the
// request while it was still in flight; its result was discarded and the
// in-flight state never cleared.
func TestBashHistoryReadOutlivesTheFunctionThatStartsIt(t *testing.T) {
	t.Parallel()
	results := make(chan error, 1)
	var entries []bashHistoryEntry
	// The caller returns as soon as the read is under way, exactly as
	// openBashHistory does.
	readBashHistoryAsync(t.Context(), latencyBashHistory{delay: 150 * time.Millisecond}, 0, bashHistoryInitialLimit,
		func(got []bashHistoryEntry, _ bool, _ uint64, err error, timedOut bool) {
			if timedOut {
				results <- errors.New("read timed out")
				return
			}
			if err != nil {
				results <- err
				return
			}
			entries = got
			results <- nil
		})
	select {
	case err := <-results:
		if err != nil {
			t.Fatalf("durable read did not complete: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("durable read never reported a result")
	}
	if len(entries) != 1 || entries[0].Command != "echo \"testing\"" || !entries[0].ExcludeFromContext {
		t.Fatalf("recalled entries = %+v", entries)
	}
}
