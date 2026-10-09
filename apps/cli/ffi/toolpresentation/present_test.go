package toolpresentation

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPresentUsesTypedTitlesAndSummaries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		call   Call
		result Result
		want   Presentation
	}{
		{name: "read range", call: Call{Name: "read", Arguments: `{"path":"README.md","offset":4}`}, result: Result{Succeeded: true, Text: "four\nfive"}, want: Presentation{Title: "Read file", Summary: "README.md:4–5"}},
		{name: "read empty", call: Call{Name: "read", Arguments: `{"path":"empty.txt"}`}, result: Result{Succeeded: true}, want: Presentation{Title: "Read file", Summary: "empty.txt · empty"}},
		{name: "read truncated", call: Call{Name: "read", Arguments: `{"path":"big.go"}`}, result: Result{Succeeded: true, Text: "a\nb\n[truncated]", Details: `{"lines":2000,"truncated":true}`}, want: Presentation{Title: "Read file", Summary: "big.go:1–2000 · truncated"}},
		{name: "write lines", call: Call{Name: "write", Arguments: `{"path":"notes.md","content":"one\ntwo\n"}`}, want: Presentation{Title: "Write 2 lines", Summary: "notes.md"}},
		{name: "edits", call: Call{Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"a","newText":"b"},{"oldText":"c","newText":"d"}]}`}, want: Presentation{Title: "Edit 2 sections", Summary: "main.go"}},
		{name: "one edit", call: Call{Name: "edit", Arguments: `{"path":"main.go","oldText":"a","newText":"b"}`}, want: Presentation{Title: "Edit 1 section", Summary: "main.go"}},
		{name: "scratchpad", call: Call{Name: "edit_scratchpad", Arguments: `{"edits":[{"oldText":"a","newText":"b"}]}`}, want: Presentation{Title: "Update scratchpad", Summary: "1 edit"}},
		{name: "search", call: Call{Name: "grep", Arguments: `{"pattern":"TODO","path":"docs"}`}, want: Presentation{Title: "Search", Summary: "TODO in docs"}},
		{name: "find", call: Call{Name: "find", Arguments: `{"pattern":"*_test.go"}`}, want: Presentation{Title: "Find files", Summary: "*_test.go in ."}},
		{name: "list", call: Call{Name: "ls", Arguments: `{"path":"internal"}`}, want: Presentation{Title: "List directory", Summary: "internal"}},
		{name: "change directory", call: Call{Name: "change_cwd", Arguments: `{"path":"../app"}`}, want: Presentation{Title: "Change directory", Summary: "../app"}},
		{name: "command", call: Call{Name: "bash", Arguments: `{"command":"go test ./... | tail"}`}, want: Presentation{Title: "Run command", Summary: "go → tail"}},
		{name: "skill", call: Call{Name: "activate_skill", Arguments: `{"name":"vaxis-ui"}`}, want: Presentation{Title: "Load skill", Summary: "vaxis-ui"}},
		{name: "agent", call: Call{Name: "subagent", Arguments: `{"action":"message","agent":"reviewer"}`}, want: Presentation{Title: "Message agent", Summary: "reviewer"}},
		{name: "discover sessions", call: Call{Name: "peer_session", Arguments: `{"action":"discover"}`}, want: Presentation{Title: "Discover sessions", Summary: "available sessions"}},
		{name: "ask session", call: Call{Name: "peer_session", Arguments: `{"action":"send","sessionId":"session_peer","message":"Review the scheduler"}`}, want: Presentation{Title: "Ask session", Summary: "Review the scheduler"}},
		{name: "inspect peer query", call: Call{Name: "peer_session", Arguments: `{"action":"inspect","requestId":"peer_request"}`}, want: Presentation{Title: "Inspect peer query", Summary: "peer_request"}},
		{name: "wait for peer query", call: Call{Name: "peer_session", Arguments: `{"action":"wait","requestId":"peer_request","timeoutSeconds":30}`}, want: Presentation{Title: "Wait for peer query", Summary: "peer_request"}},
		{name: "create session", call: Call{Name: "create_session", Arguments: `{"cwd":"/tmp/project","name":"Investigate API","prompt":"Inspect it"}`}, want: Presentation{Title: "Create session", Summary: "Investigate API · /tmp/project"}},
		{name: "create session missing name", call: Call{Name: "create_session", Arguments: `{"cwd":"/tmp/project"}`}, want: Presentation{Title: "Create session", Summary: "/tmp/project"}},
		{name: "create session malformed", call: Call{Name: "create_session", Arguments: `{`}, want: Presentation{Title: "Create session", Summary: `{`}},
		{name: "unfinished read", call: Call{Name: "read", Arguments: `{"path":"README.md"}`}, want: Presentation{Title: "Read file", Summary: "README.md"}},
		{name: "fallback", call: Call{Name: "custom_tool", Arguments: `{"path":"tmp"}`}, want: Presentation{Title: "Custom Tool", Summary: "tmp"}},
		{name: "fallback non-string arguments", call: Call{Name: "custom_tool", Arguments: `{"limit":20}`}, want: Presentation{Title: "Custom Tool", Summary: `{"limit":20}`}},
		{name: "fallback without arguments", call: Call{Name: "custom-tool"}, want: Presentation{Title: "Custom Tool", Summary: "no arguments"}},
		{name: "fallback truncated arguments", call: Call{Name: "custom_tool", ArgumentsTruncated: true}, want: Presentation{Title: "Custom Tool", Summary: "arguments truncated"}},
		{name: "known malformed arguments", call: Call{Name: "read", Arguments: `{`}, want: Presentation{Title: "Read file", Summary: `{`}},
		{name: "malformed search arguments", call: Call{Name: "grep", Arguments: `{`}, want: Presentation{Title: "Search", Summary: `{`}},
		{name: "malformed find arguments", call: Call{Name: "find", Arguments: `{`}, want: Presentation{Title: "Find files", Summary: `{`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := Present(test.call, test.result)
			if got.Title != test.want.Title || got.Summary != test.want.Summary {
				t.Fatalf("Present() = %q · %q, want %q · %q", got.Title, got.Summary, test.want.Title, test.want.Summary)
			}
		})
	}
}

func TestPathLedSummariesReportTheirPath(t *testing.T) {
	t.Parallel()
	read := Present(Call{Name: "read", Arguments: `{"path":"docs/a.md"}`}, Result{})
	if !read.PathFirst || read.Path != "docs/a.md" {
		t.Fatalf("read = %+v, want a path-led summary of docs/a.md", read)
	}
	if search := Present(Call{Name: "grep", Arguments: `{"pattern":"x","path":"docs"}`}, Result{}); search.PathFirst {
		t.Fatalf("search = %+v, want a pattern-led summary", search)
	}
}

func TestBashCommandSummarizesLikeTheVaxisClient(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
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
	} {
		t.Run(test.command, func(t *testing.T) {
			got := BashCommand(test.command)
			if got.Text != test.text || got.Summarized != test.summarized || got.CommandCount != test.count {
				t.Fatalf("BashCommand() = %+v, want text %q summarized %v count %d", got, test.text, test.summarized, test.count)
			}
		})
	}
	long := BashCommand("grep " + strings.Repeat("x", 100))
	if !long.Summarized || utf8.RuneCountInString(long.Text) > maxBashCommandSummaryLength || !strings.HasSuffix(long.Text, "…") {
		t.Fatalf("long command presentation = %+v", long)
	}
}
