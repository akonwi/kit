package tui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func transcriptMessageWithContent(id, turnID, role string, content ...protocol.TranscriptContent) protocol.TranscriptMessage {
	return protocol.TranscriptMessage{ID: id, TurnID: turnID, Role: role, Content: content}
}

func textBlock(text string) protocol.TranscriptContent {
	return protocol.TranscriptContent{Kind: protocol.TranscriptContentText, Text: text}
}

func thinkingBlock(text string) protocol.TranscriptContent {
	return protocol.TranscriptContent{Kind: protocol.TranscriptContentThinking, Text: text}
}

func toolCallBlock(id, name, arguments string) protocol.TranscriptContent {
	return protocol.TranscriptContent{
		Kind: protocol.TranscriptContentToolCall, ToolCallID: id,
		ToolName: name, Arguments: arguments,
	}
}

func TestBuildTurnTranscriptItemsPairsResultsAndMarksAbortedTurns(t *testing.T) {
	t.Parallel()

	messages := []protocol.TranscriptMessage{
		transcriptMessageWithContent("user_1", "turn_1", "user", textBlock("inspect")),
		transcriptMessageWithContent("assistant_1", "turn_1", "assistant",
			textBlock("reading"), toolCallBlock("call_1", "read", `{"path":"README.md"}`)),
		{
			ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read",
			Content: []protocol.TranscriptContent{textBlock("contents")},
		},
		{
			ID: "assistant_2", TurnID: "turn_1", Role: "assistant", StopReason: "aborted",
			ErrorMessage: "stopped", IsError: true,
		},
	}

	items := buildTurnTranscriptItems(messages, nil)
	if len(items) != 3 {
		t.Fatalf("item count = %d, want 3: %+v", len(items), items)
	}
	if result, ok := items[1].ToolResults["call_1"]; !ok || result.ID != "result_1" {
		t.Fatalf("paired result = %+v, present %v", result, ok)
	}
	for index, item := range items {
		if !item.Aborted {
			t.Errorf("item %d was not marked aborted", index)
		}
	}
}

func TestGroupTranscriptDisplayItemsKeepsProseAndConsolidatesTurnWork(t *testing.T) {
	t.Parallel()

	messages := []protocol.TranscriptMessage{
		transcriptMessageWithContent("user_1", "turn_1", "user", textBlock("inspect")),
		transcriptMessageWithContent("assistant_1", "turn_1", "assistant",
			textBlock("I will read it."), toolCallBlock("call_1", "read", `{"path":"README.md"}`)),
		transcriptMessageWithContent("assistant_2", "turn_1", "assistant",
			toolCallBlock("call_2", "grep", `{"pattern":"TODO"}`)),
		transcriptMessageWithContent("assistant_3", "turn_1", "assistant", textBlock("Done.")),
	}

	display := groupTranscriptDisplayItems(buildTurnTranscriptItems(messages, nil))
	gotKinds := make([]transcriptDisplayKind, len(display))
	for index := range display {
		gotKinds[index] = display[index].Kind
	}
	wantKinds := []transcriptDisplayKind{
		transcriptDisplaySingle,
		transcriptDisplayAssistantProse,
		transcriptDisplayTurnWork,
		transcriptDisplayAssistantProse,
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("display kinds = %v, want %v", gotKinds, wantKinds)
	}
	work := display[2]
	if work.ID != "turn-work:turn_1:assistant_1" || len(work.Items) != 2 {
		t.Fatalf("turn work = %+v", work)
	}
	calls := displayItemToolCalls(work)
	if len(calls) != 2 || calls[0].Name != "read" || calls[1].Name != "grep" {
		t.Fatalf("turn-work calls = %+v", calls)
	}
}

func TestGroupTranscriptDisplayItemsSplitsToolBatchesAroundProse(t *testing.T) {
	t.Parallel()

	messages := []protocol.TranscriptMessage{
		transcriptMessageWithContent("assistant_1", "turn_1", "assistant",
			toolCallBlock("call_1", "read", `{"path":"README.md"}`)),
		transcriptMessageWithContent("assistant_2", "turn_1", "assistant",
			textBlock("That path is stale; searching instead."),
			toolCallBlock("call_2", "grep", `{"pattern":"TODO","path":"docs"}`)),
		transcriptMessageWithContent("assistant_3", "turn_1", "assistant", textBlock("Done.")),
	}

	display := groupTranscriptDisplayItems(buildTurnTranscriptItems(messages, nil))
	if len(display) != 4 {
		t.Fatalf("display = %+v, want work, prose, work, prose", display)
	}
	if display[0].Kind != transcriptDisplayTurnWork || display[0].ID != "turn-work:turn_1:assistant_1" ||
		display[1].Kind != transcriptDisplayAssistantProse || assistantProse(display[1].Item.Message) != "That path is stale; searching instead." ||
		display[2].Kind != transcriptDisplayTurnWork || display[2].ID != "turn-work:turn_1:assistant_2" ||
		display[3].Kind != transcriptDisplayAssistantProse || assistantProse(display[3].Item.Message) != "Done." {
		t.Fatalf("chronological display = %+v", display)
	}
}

func TestGroupTranscriptDisplayItemsKeepsPendingProseToolBatchIdentityStable(t *testing.T) {
	t.Parallel()

	first := transcriptMessageWithContent("assistant_1", "turn_1", "assistant",
		toolCallBlock("call_1", "read", `{"path":"README.md"}`))
	second := transcriptMessageWithContent("assistant_2", "turn_1", "assistant",
		textBlock("I need to search next."), toolCallBlock("call_2", "grep", `{"pattern":"TODO"}`))
	items := buildTurnTranscriptItems([]protocol.TranscriptMessage{first, second}, nil)
	items[1].Pending = true
	pending := groupTranscriptDisplayItems(items)
	items[1].Pending = false
	completed := groupTranscriptDisplayItems(items)

	if len(pending) != 2 || pending[1].ID != "turn-work:turn_1:assistant_2" {
		t.Fatalf("pending display = %+v", pending)
	}
	if len(completed) != 3 || completed[2].ID != pending[1].ID {
		t.Fatalf("completed display changed batch identity: %+v", completed)
	}
}

func TestPresentTranscriptBuffersPendingAssistantTextUntilCompletion(t *testing.T) {
	t.Parallel()

	messages := []transcriptMessage{
		{ID: "user_1", TurnID: "turn_1", Role: "user", Text: "inspect"},
		{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Text: "partial response", Pending: true},
	}
	pending := presentTranscript(messages)
	if len(pending.Items) != 1 || pending.Items[0].Kind != transcriptDisplaySingle || pending.Items[0].Item.Message.TextContent() != "inspect" {
		t.Fatalf("pending transcript = %+v, want only the submitted user message", pending.Items)
	}

	messages[1].Text = "final response"
	messages[1].Pending = false
	completed := presentTranscript(messages)
	if len(completed.Items) != 2 || completed.Items[1].Kind != transcriptDisplayAssistantProse || assistantProse(completed.Items[1].Item.Message) != "final response" {
		t.Fatalf("completed transcript = %+v, want atomic final assistant prose", completed.Items)
	}
}

func TestPresentTranscriptKeepsPendingThinkingAndToolsVisible(t *testing.T) {
	t.Parallel()

	presentation := presentTranscript([]transcriptMessage{{
		ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Text: "partial response",
		Thinking: "Inspecting", Pending: true,
		ToolCalls: []transcriptToolCall{{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}},
	}})
	if len(presentation.Items) != 1 || presentation.Items[0].Kind != transcriptDisplayTurnWork {
		t.Fatalf("pending activity = %+v, want one work item", presentation.Items)
	}
	sections := buildActivitySections(presentation.Items[0])
	if len(sections) != 1 || sections[0].Thinking != "Inspecting" || sections[0].Prose != "" || len(sections[0].Calls) != 1 {
		t.Fatalf("pending activity sections = %+v", sections)
	}
}

func TestHistoricalProjectionPreservesInterleavedContentOrder(t *testing.T) {
	t.Parallel()

	messages := projectTranscript([]protocol.TranscriptMessage{{
		ID: "assistant_1", TurnID: "turn_1", Role: "assistant",
		Content: []protocol.TranscriptContent{
			thinkingBlock("inspect first"),
			toolCallBlock("call_1", "read", `{"path":"one.md"}`),
			textBlock("First result."),
			thinkingBlock("inspect next"),
			toolCallBlock("call_2", "read", `{"path":"two.md"}`),
			textBlock("Second result."),
		},
	}})
	presentation := presentTranscript(messages)
	if len(presentation.Items) != 4 {
		t.Fatalf("interleaved display = %+v, want work, prose, work, prose", presentation.Items)
	}
	wantKinds := []transcriptDisplayKind{
		transcriptDisplayTurnWork, transcriptDisplayAssistantProse,
		transcriptDisplayTurnWork, transcriptDisplayAssistantProse,
	}
	for index, want := range wantKinds {
		if presentation.Items[index].Kind != want {
			t.Fatalf("display[%d].kind = %q, want %q", index, presentation.Items[index].Kind, want)
		}
	}
	if got := assistantProse(presentation.Items[1].Item.Message); got != "First result." {
		t.Fatalf("first prose = %q", got)
	}
	if got := assistantProse(presentation.Items[3].Item.Message); got != "Second result." {
		t.Fatalf("second prose = %q", got)
	}
	first := buildActivitySections(presentation.Items[0])
	second := buildActivitySections(presentation.Items[2])
	if len(first) != 1 || first[0].Thinking != "inspect first" || len(first[0].Calls) != 1 || first[0].Calls[0].ID != "call_1" {
		t.Fatalf("first activity = %+v", first)
	}
	if len(second) != 1 || second[0].Thinking != "inspect next" || len(second[0].Calls) != 1 || second[0].Calls[0].ID != "call_2" {
		t.Fatalf("second activity = %+v", second)
	}
}

func TestHistoricalProjectionKeepsThinkingWithTheFollowingToolCall(t *testing.T) {
	t.Parallel()

	messages := projectTranscript([]protocol.TranscriptMessage{{
		ID: "assistant_1", TurnID: "turn_1", Role: "assistant",
		Content: []protocol.TranscriptContent{
			toolCallBlock("call_1", "read", `{"path":"one.md"}`),
			thinkingBlock("next step"),
			toolCallBlock("call_2", "read", `{"path":"two.md"}`),
		},
	}})
	presentation := presentTranscript(messages)
	if len(presentation.Items) != 1 || presentation.Items[0].Kind != transcriptDisplayTurnWork {
		t.Fatalf("ordered work = %+v, want one contiguous batch", presentation.Items)
	}
	sections := buildActivitySections(presentation.Items[0])
	if len(sections) != 2 || len(sections[0].Calls) != 1 || sections[0].Calls[0].ID != "call_1" || sections[0].Thinking != "" {
		t.Fatalf("first work section = %+v", sections)
	}
	if len(sections[1].Calls) != 1 || sections[1].Calls[0].ID != "call_2" || sections[1].Thinking != "next step" {
		t.Fatalf("second work section = %+v", sections)
	}
}

func TestHistoricalProjectionAppendsTerminalErrorAfterOrderedContent(t *testing.T) {
	t.Parallel()

	messages := projectTranscript([]protocol.TranscriptMessage{{
		ID: "assistant_1", TurnID: "turn_1", Role: "assistant",
		Content: []protocol.TranscriptContent{
			toolCallBlock("call_1", "read", `{"path":"README.md"}`),
			textBlock("Partial answer."),
		},
		StopReason: "error", ErrorMessage: "provider disconnected", IsError: true,
	}})
	presentation := presentTranscript(messages)
	if len(presentation.Items) != 3 || presentation.Items[0].Kind != transcriptDisplayTurnWork {
		t.Fatalf("terminal error display = %+v, want work, prose, error", presentation.Items)
	}
	if got := assistantProse(presentation.Items[1].Item.Message); got != "Partial answer." {
		t.Fatalf("completed prose = %q", got)
	}
	errorItem := presentation.Items[2].Item.Message
	if got := assistantProse(errorItem); got != "provider disconnected" || !errorItem.IsError || errorItem.StopReason != "error" {
		t.Fatalf("terminal error = %+v, prose %q", errorItem, got)
	}
	state, exists := presentation.ToolStates[transcriptToolStateKey{TurnID: "turn_1", ToolCallID: "call_1"}]
	resolved := resolveActivityToolState(state, exists, false)
	if !exists || resolved != activityToolFailed {
		t.Fatalf("unresolved terminal tool state = %+v, exists %v, resolved %v, want failed", state, exists, resolved)
	}
}

func TestThinkingWithoutToolsDoesNotCreateActivityWork(t *testing.T) {
	t.Parallel()

	message := transcriptMessageWithContent(
		"assistant_1", "turn_1", "assistant",
		thinkingBlock("## Plan\n\n- inspect"), textBlock("Done."),
	)
	display := groupTranscriptDisplayItems(buildTurnTranscriptItems([]protocol.TranscriptMessage{message}, nil))
	if len(display) != 1 || display[0].Kind != transcriptDisplayAssistantProse || assistantProse(display[0].Item.Message) != "Done." {
		t.Fatalf("thinking-only display = %+v, want assistant prose without a work drawer", display)
	}
}

func TestPendingThinkingWithoutToolsHasNoTranscriptItem(t *testing.T) {
	t.Parallel()

	presentation := presentTranscript([]transcriptMessage{{
		ID: "assistant_1", TurnID: "turn_1", Role: "assistant",
		Text: "partial response", Thinking: "considering", Pending: true,
	}})
	if len(presentation.Items) != 0 {
		t.Fatalf("pending thinking transcript = %+v, want no tool drawer", presentation.Items)
	}
}

func TestToolArrivalCreatesActivityWorkWithThinkingEvidence(t *testing.T) {
	t.Parallel()

	message := transcriptMessageWithContent(
		"assistant_1", "turn_1", "assistant",
		thinkingBlock("considering"), toolCallBlock("call_1", "read", `{"path":"README.md"}`),
	)
	display := groupTranscriptDisplayItems(buildTurnTranscriptItems([]protocol.TranscriptMessage{message}, nil))
	if len(display) != 1 || display[0].Kind != transcriptDisplayTurnWork || display[0].ID != "turn-work:turn_1:assistant_1" {
		t.Fatalf("tool-backed work = %+v", display)
	}
	sections := buildActivitySections(display[0])
	if len(sections) != 1 || sections[0].Thinking != "considering" || len(sections[0].Calls) != 1 {
		t.Fatalf("tool-backed thinking evidence = %+v", sections)
	}
}

func TestGroupTranscriptDisplayItemsKeepsWorkIdentityStableAcrossCompletion(t *testing.T) {
	t.Parallel()

	assistant := transcriptMessageWithContent(
		"assistant_1", "turn_1", "assistant", toolCallBlock("call_1", "read", `{"path":"README.md"}`),
	)
	before := groupTranscriptDisplayItems(buildTurnTranscriptItems([]protocol.TranscriptMessage{assistant}, nil))
	result := protocol.TranscriptMessage{
		ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read",
		Content: []protocol.TranscriptContent{textBlock("contents")},
	}
	after := groupTranscriptDisplayItems(buildTurnTranscriptItems([]protocol.TranscriptMessage{assistant, result}, nil))
	if len(before) != 1 || len(after) != 1 || before[0].ID != after[0].ID {
		t.Fatalf("display identity changed: before=%+v after=%+v", before, after)
	}
	if _, ok := after[0].Items[0].ToolResults["call_1"]; !ok {
		t.Fatalf("completed result was not paired: %+v", after[0])
	}
}

func TestBuildActivitySectionsPreservesAssistantBoundariesAndStableIDs(t *testing.T) {
	t.Parallel()

	source := transcriptDisplayItem{Kind: transcriptDisplayTurnWork, TurnID: "turn_1", Items: []turnTranscriptItem{
		{Kind: transcriptItemAssistant, ID: "assistant_1", TurnID: "turn_1", Message: transcriptMessageWithContent(
			"assistant_1", "turn_1", "assistant", textBlock("First step"), toolCallBlock("call_1", "read", `{"path":"README.md"}`),
		)},
		{Kind: transcriptItemAssistant, ID: "assistant_2", TurnID: "turn_1", Message: transcriptMessageWithContent(
			"assistant_2", "turn_1", "assistant", toolCallBlock("call_2", "grep", `{"pattern":"TODO"}`),
		)},
	}}
	sections := buildActivitySections(source)
	if len(sections) != 2 {
		t.Fatalf("activity sections = %+v, want 2", sections)
	}
	if sections[0].ID != "activity-section:turn_1:assistant_1" || sections[0].Prose != "" || len(sections[0].Calls) != 1 {
		t.Fatalf("first activity section = %+v", sections[0])
	}
	if sections[1].ID != "activity-section:turn_1:assistant_2" || sections[1].Calls[0].Name != "grep" {
		t.Fatalf("second activity section = %+v", sections[1])
	}
}

func TestMoveActivityToolCursorClampsAtListEdges(t *testing.T) {
	t.Parallel()

	keys := []activityToolKey{{ToolCallID: "one"}, {ToolCallID: "two"}}
	if got := moveActivityToolCursor(keys, keys[0], -1); got != keys[0] {
		t.Fatalf("cursor before first = %+v", got)
	}
	if got := moveActivityToolCursor(keys, keys[1], 1); got != keys[1] {
		t.Fatalf("cursor after last = %+v", got)
	}
}

func TestResolveActivityToolState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		state   transcriptMessage
		exists  bool
		aborted bool
		want    activityToolState
	}{
		{name: "pending", want: activityToolPending},
		{name: "planned", state: transcriptMessage{Pending: true, ToolStatus: "Planned"}, exists: true, want: activityToolPending},
		{name: "running", state: transcriptMessage{Pending: true, ToolStatus: "Running…"}, exists: true, want: activityToolRunning},
		{name: "success", state: transcriptMessage{ToolStatus: "Completed"}, exists: true, want: activityToolSucceeded},
		{name: "failed", state: transcriptMessage{ToolStatus: "Failed", IsError: true}, exists: true, want: activityToolFailed},
		{name: "completed before abort", state: transcriptMessage{ToolStatus: "Completed"}, exists: true, aborted: true, want: activityToolSucceeded},
		{name: "missing after abort", aborted: true, want: activityToolAborted},
		{name: "not run", state: transcriptMessage{ToolStatus: "Not run"}, exists: true, want: activityToolAborted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveActivityToolState(test.state, test.exists, test.aborted); got != test.want {
				t.Fatalf("activity state = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPresentationScopesRepeatedToolCallIDsByTurn(t *testing.T) {
	t.Parallel()

	messages := []transcriptMessage{
		{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", ToolCalls: []transcriptToolCall{{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		{ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read", ToolStatus: "Completed"},
		{ID: "assistant_2", TurnID: "turn_2", Role: "assistant", ToolCalls: []transcriptToolCall{{ID: "call_1", Name: "write", Arguments: json.RawMessage(`{}`)}}},
		{ID: "result_2", TurnID: "turn_2", Role: "tool", ToolCallID: "call_1", ToolName: "write", ToolStatus: "Failed", IsError: true},
	}
	presentation := presentTranscript(messages)
	first := presentation.ToolStates[transcriptToolStateKey{TurnID: "turn_1", ToolCallID: "call_1"}]
	second := presentation.ToolStates[transcriptToolStateKey{TurnID: "turn_2", ToolCallID: "call_1"}]
	if first.ToolStatus != "Completed" || first.IsError || second.ToolStatus != "Failed" || !second.IsError {
		t.Fatalf("turn-scoped tool states = first %+v second %+v", first, second)
	}
}

func TestLocalActivitySourceIdentitySurvivesSnapshotReconciliation(t *testing.T) {
	t.Parallel()

	live := []transcriptMessage{
		{ID: "live-assistant", TurnID: "turn_1", Role: "assistant", ToolCalls: []transcriptToolCall{{
			ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`),
		}}},
		{ID: "live-tool:call_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read", Pending: true},
	}
	historical := []transcriptMessage{
		{ID: "live-assistant", TurnID: "turn_1", Role: "assistant", ToolCalls: []transcriptToolCall{{
			ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`),
		}}},
		{ID: "persisted-result", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read", ToolStatus: "Completed"},
	}
	livePresentation := presentTranscript(live)
	historicalPresentation := presentTranscript(historical)
	liveSource := livePresentation.Items[0].ID
	historicalSource := historicalPresentation.Items[0].ID
	if liveSource != "turn-work:turn_1:live-assistant" || historicalSource != liveSource {
		t.Fatalf("activity source changed from %q to %q", liveSource, historicalSource)
	}
	liveSections := buildActivitySections(livePresentation.Items[0])
	historicalSections := buildActivitySections(historicalPresentation.Items[0])
	if len(liveSections) != 1 || len(historicalSections) != 1 || liveSections[0].ID != historicalSections[0].ID {
		t.Fatalf("activity section identity changed: live %+v historical %+v", liveSections, historicalSections)
	}
}

func TestPresentBashCommandMatchesMainPresentation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		command    string
		text       string
		summarized bool
		count      int
	}{
		{`printf '%s' 'a  b'`, `printf '%s' 'a  b'`, false, 1},
		{`grep -R 'TerminalColors' app | head -10; grep DEFAULT app | head`, "grep → head · grep → head", true, 4},
		{`printf '%s' 'a|b;c'`, `printf '%s' 'a|b;c'`, false, 1},
		{`echo $(printf 'a|b') | sed 's/a/b/'`, "echo → sed", true, 2},
		{"pwd\nbun test", "shell script · 2 commands", true, 2},
		{"sleep 1 & echo done", "sleep · echo", true, 2},
		{"echo ok # note | sed x", "echo ok # note | sed x", false, 1},
		{"  for x in a b; do echo $x; done", "shell command · 3 steps", true, 3},
		{"cat <<EOF\na | b\nEOF", "shell script · 3 lines", true, 4},
		{"echo foo \\\n  bar", "shell script · 2 lines", true, 1},
		{"sudo -u root grep x | head", "sudo → head", true, 2},
		{`FOO="a b" grep x | head`, "grep → head", true, 2},
		{`my\ command | head`, `my\ command → head`, true, 2},
		{"cat <&0 | wc", "cat → wc", true, 2},
		{"echo hi;# note | sed x", "echo hi;# note | sed x", false, 1},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			got := presentBashCommand(test.command)
			if got.Text != test.text || got.Summarized != test.summarized || got.CommandCount != test.count {
				t.Fatalf("presentBashCommand() = %+v, want text %q summarized %v count %d", got, test.text, test.summarized, test.count)
			}
		})
	}

	long := presentBashCommand("grep " + strings.Repeat("x", 100))
	if !long.Summarized || len([]rune(long.Text)) > maxBashCommandSummaryLength || !strings.HasSuffix(long.Text, "…") {
		t.Fatalf("long command presentation = %+v", long)
	}
}

func TestPresentToolCallUsesTypedTitlesAndSummaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		call   transcriptToolCall
		state  transcriptMessage
		exists bool
		want   toolCallPresentation
	}{
		{name: "read range", call: transcriptToolCall{Name: "read", Arguments: json.RawMessage(`{"path":"README.md","offset":4}`)}, state: transcriptMessage{Text: "four\nfive", ToolStatus: "Completed"}, exists: true, want: toolCallPresentation{Title: "Read file", Summary: "README.md:4–5"}},
		{name: "write lines", call: transcriptToolCall{Name: "write", Arguments: json.RawMessage(`{"path":"notes.md","content":"one\ntwo\n"}`)}, want: toolCallPresentation{Title: "Write 2 lines", Summary: "notes.md"}},
		{name: "edits", call: transcriptToolCall{Name: "edit", Arguments: json.RawMessage(`{"path":"main.go","edits":[{"oldText":"a","newText":"b"},{"oldText":"c","newText":"d"}]}`)}, want: toolCallPresentation{Title: "Edit 2 sections", Summary: "main.go"}},
		{name: "search", call: transcriptToolCall{Name: "grep", Arguments: json.RawMessage(`{"pattern":"TODO","path":"docs"}`)}, want: toolCallPresentation{Title: "Search", Summary: "TODO in docs"}},
		{name: "agent", call: transcriptToolCall{Name: "subagent", Arguments: json.RawMessage(`{"action":"message","agent":"reviewer"}`)}, want: toolCallPresentation{Title: "Message agent", Summary: "reviewer"}},
		{name: "discover sessions", call: transcriptToolCall{Name: "peer_session", Arguments: json.RawMessage(`{"action":"discover"}`)}, want: toolCallPresentation{Title: "Discover sessions", Summary: "available sessions"}},
		{name: "ask session", call: transcriptToolCall{Name: "peer_session", Arguments: json.RawMessage(`{"action":"send","sessionId":"session_peer","message":"Review the scheduler"}`)}, want: toolCallPresentation{Title: "Ask session", Summary: "Review the scheduler"}},
		{name: "inspect peer query", call: transcriptToolCall{Name: "peer_session", Arguments: json.RawMessage(`{"action":"inspect","requestId":"peer_request"}`)}, want: toolCallPresentation{Title: "Inspect peer query", Summary: "peer_request"}},
		{name: "wait for peer query", call: transcriptToolCall{Name: "peer_session", Arguments: json.RawMessage(`{"action":"wait","requestId":"peer_request","timeoutSeconds":30}`)}, want: toolCallPresentation{Title: "Wait for peer query", Summary: "peer_request"}},
		{name: "create session", call: transcriptToolCall{Name: "create_session", Arguments: json.RawMessage(`{"cwd":"/tmp/project","name":"Investigate API","prompt":"Inspect it"}`)}, want: toolCallPresentation{Title: "Create session", Summary: "Investigate API · /tmp/project"}},
		{name: "create session missing name", call: transcriptToolCall{Name: "create_session", Arguments: json.RawMessage(`{"cwd":"/tmp/project"}`)}, want: toolCallPresentation{Title: "Create session", Summary: "/tmp/project"}},
		{name: "create session malformed", call: transcriptToolCall{Name: "create_session", Arguments: json.RawMessage(`{`)}, want: toolCallPresentation{Title: "Create session", Summary: `{`}},
		{name: "aborted read", call: transcriptToolCall{Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}, state: transcriptMessage{ToolStatus: "Not run"}, exists: true, want: toolCallPresentation{Title: "Read file", Summary: "README.md"}},
		{name: "fallback", call: transcriptToolCall{Name: "custom_tool", Arguments: json.RawMessage(`{"path":"tmp"}`)}, want: toolCallPresentation{Title: "Custom Tool", Summary: "tmp"}},
		{name: "fallback non-string arguments", call: transcriptToolCall{Name: "custom_tool", Arguments: json.RawMessage(`{"limit":20}`)}, want: toolCallPresentation{Title: "Custom Tool", Summary: `{"limit":20}`}},
		{name: "known malformed arguments", call: transcriptToolCall{Name: "read", Arguments: json.RawMessage(`{`)}, want: toolCallPresentation{Title: "Read file", Summary: `{`}},
		{name: "malformed search arguments", call: transcriptToolCall{Name: "grep", Arguments: json.RawMessage(`{`)}, want: toolCallPresentation{Title: "Search", Summary: `{`}},
		{name: "malformed find arguments", call: transcriptToolCall{Name: "find", Arguments: json.RawMessage(`{`)}, want: toolCallPresentation{Title: "Find files", Summary: `{`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := presentToolCall(test.call, test.state, test.exists); got != test.want {
				t.Fatalf("presentation = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestTruncateToolPathTailPreservesInformativeEnd(t *testing.T) {
	t.Parallel()
	if got := truncateToolPathTail("docs/design/0012-native-macos-client.md", 24); got != "⋯/native-macos-client.md" {
		t.Fatalf("tail path = %q", got)
	}
}

func TestToolPresentationHelpersMatchMainRules(t *testing.T) {
	t.Parallel()

	read := transcriptToolCall{ID: "read_1", Name: "read", Arguments: json.RawMessage(`{"path":"/tmp/file"}`)}
	if got := formatToolArguments(read, false); got != "/tmp/file" {
		t.Fatalf("read argument = %q", got)
	}
	longPath := "/tmp/" + strings.Repeat("nested/", 20) + "file.go"
	longRead := transcriptToolCall{ID: "read_2", Name: "read", Arguments: json.RawMessage(`{"path":"` + longPath + `"}`)}
	if got := activityToolArgument(longRead); got != longPath {
		t.Fatalf("full Activity argument = %q, want %q", got, longPath)
	}

	subagent := transcriptToolCall{ID: "agent_1", Name: "subagent", Arguments: json.RawMessage(`{"action":"run","agent":"reviewer","message":"inspect changes"}`)}
	if got := formatToolArguments(subagent, false); got != "inspect changes" {
		t.Fatalf("subagent argument = %q", got)
	}

	peer := transcriptToolCall{ID: "peer_1", Name: "peer_session", Arguments: json.RawMessage(`{"action":"send","sessionId":"session_peer","message":"inspect peer changes"}`)}
	if got := formatToolArguments(peer, false); got != "inspect peer changes" {
		t.Fatalf("peer session argument = %q", got)
	}

	createSession := transcriptToolCall{ID: "session_1", Name: "create_session", Arguments: json.RawMessage(`{"cwd":"/tmp/project","name":"Investigate API","prompt":"inspect"}`)}
	if got := formatToolArguments(createSession, true); got != "Investigate API" {
		t.Fatalf("create session argument = %q", got)
	}

	skill := transcriptToolCall{ID: "skill_1", Name: "activate_skill", Arguments: json.RawMessage(`{"name":"vaxis-ui"}`)}
	if got := formatToolArguments(skill, false); got != "vaxis-ui" {
		t.Fatalf("skill argument = %q", got)
	}
}

// TestPresentTranscriptCarriesBashExecutionOutsideWireMessage pins that a bash
// row keeps its execution through presentation. Direct shell work is session
// history rather than transcript content, so the execution rides on the
// presentation item instead of the wire message.
func TestPresentTranscriptCarriesBashExecutionOutsideWireMessage(t *testing.T) {
	execution := bashExecution("bash_1", "git status", false, "clean")
	presentation := presentTranscript([]transcriptMessage{
		{ID: "user_1", TurnID: "turn_1", Role: "user", Text: "run it"},
		{ID: "bash_1", Role: "bash", Bash: execution},
	})
	rows := presentation.Items
	if len(rows) != 2 {
		t.Fatalf("display rows = %d, want 2: %+v", len(rows), rows)
	}
	bash := rows[1]
	if bash.Kind != transcriptDisplaySingle || bash.Item.Kind != transcriptItemBash {
		t.Fatalf("bash row = %+v", bash)
	}
	if bash.Item.Bash == nil || bash.Item.Bash.ID != "bash_1" || bash.Item.Bash.Command != "git status" {
		t.Fatalf("bash row execution = %+v", bash.Item.Bash)
	}
}
