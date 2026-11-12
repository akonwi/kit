package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

func TestKitAndAttachParseAView(t *testing.T) {
	isolatedHome(t)
	serverHome := t.TempDir()
	var stdout, stderr bytes.Buffer
	for _, test := range []struct {
		args []string
		want View
	}{
		{args: []string{"--view", "diff"}, want: View{Kind: ViewDiff}},
		{args: []string{"--view", "diff", "--annotation", "7"}, want: View{Kind: ViewDiff, Annotation: 7}},
		{args: []string{"--view", "file", "--path", "internal/x.go", "--line", "40"}, want: View{Kind: ViewFile, Path: "internal/x.go", Line: 40}},
		{args: []string{"--view", "host-file", "--path", "~/notes.md"}, want: View{Kind: ViewHostFile, Path: "~/notes.md"}},
		{args: []string{"--view", "subagent", "--conversation", "conversation_1"}, want: View{Kind: ViewSubagent, Conversation: "conversation_1"}},
		{args: []string{"--view", "scratchpad"}, want: View{Kind: ViewScratchpad}},
		{args: []string{"--view", "diff", "--view-id", testViewID}, want: View{Kind: ViewDiff, ID: testViewID}},
	} {
		for _, prefix := range [][]string{{"--session", "session_1"}, {"attach", serverHome, "--session", "session_1"}} {
			args := append(append([]string{}, prefix...), test.args...)
			invocation := Parse(context.Background(), args, &stdout, &stderr)
			if invocation.Kind != InvocationInteractive || invocation.options.View != test.want {
				t.Errorf("Parse(%q) = %s %+v, want %+v; stderr = %q", args, invocation.Kind, invocation.options.View, test.want, stderr.String())
			}
		}
	}
}

func TestAttachRejectsInvalidViews(t *testing.T) {
	isolatedHome(t)
	serverHome := t.TempDir()
	for _, test := range []struct {
		args   []string
		stderr string
	}{
		{args: []string{"--session", "s", "--view", "transcript"}, stderr: "--view must be diff, file, host-file, subagent, or scratchpad"},
		{args: []string{"--view", "diff"}, stderr: "--view requires --session"},
		{args: []string{"--session", "s", "--path", "x.go"}, stderr: "--path requires --view"},
		{args: []string{"--session", "s", "--view", "file"}, stderr: "--view file requires --path"},
		{args: []string{"--session", "s", "--view", "diff", "--path", "x.go"}, stderr: "--path is only valid with --view file or host-file"},
		{args: []string{"--session", "s", "--view", "scratchpad", "--line", "3"}, stderr: "--line is only valid with --view file or host-file"},
		{args: []string{"--session", "s", "--view", "file", "--path", "x.go", "--line", "-1"}, stderr: "--line must not be negative"},
		{args: []string{"--session", "s", "--view", "subagent"}, stderr: "--view subagent requires --conversation"},
		{args: []string{"--session", "s", "--view", "diff", "--conversation", "c"}, stderr: "--conversation is only valid with --view subagent"},
		{args: []string{"--session", "s", "--view", "host-file", "--path", "/x", "--annotation", "1"}, stderr: "--annotation is only valid with --view diff or file"},
		{args: []string{"--session", "s", "--view-id", testViewID}, stderr: "--view-id requires --view"},
		{args: []string{"--session", "s", "--view", "diff", "--view-id", "view_1"}, stderr: "--view-id must be view_ and 32 lowercase hex digits"},
	} {
		for _, prefix := range [][]string{nil, {"attach", serverHome}} {
			var stdout, stderr bytes.Buffer
			args := append(append([]string{}, prefix...), test.args...)
			invocation := Parse(context.Background(), args, &stdout, &stderr)
			if invocation.Kind != InvocationExit || invocation.ExitCode != 2 || !strings.Contains(stderr.String(), test.stderr) {
				t.Errorf("Parse(%q) = %s %d, stderr = %q; want usage error %q", args, invocation.Kind, invocation.ExitCode, stderr.String(), test.stderr)
			}
		}
	}
}

var testViewID = "view_" + strings.Repeat("0a", 16)

func TestViewCommandAttachesWithoutStartingAServer(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	file := View{Kind: ViewFile, Path: "internal/x.go", Line: 40, Annotation: 3, ID: testViewID}
	own := viewCommand("/opt/kit/bin/kit", home, attachTarget{}, "session_1", file)
	if want := []string{
		"/usr/bin/env", "KIT_HOME=" + home, "/opt/kit/bin/kit", "--session", "session_1",
		"--view", "file", "--path", "internal/x.go", "--line", "40", "--annotation", "3", "--view-id", testViewID,
	}; !slices.Equal(own, want) {
		t.Errorf("viewCommand(this home's server) = %q, want %q", own, want)
	}
	for _, test := range []struct {
		name   string
		target attachTarget
		server string
	}{
		{name: "another home", target: attachTarget{Name: "dev", Home: "/srv/dev"}, server: "/srv/dev"},
		{name: "a URL server", target: attachTarget{Name: "box", URL: "https://box.example"}, server: "https://box.example"},
		{name: "a saved SSH server", target: attachTarget{Name: "devbox", SSH: "me@devbox"}, server: "devbox"},
		{name: "an SSH destination", target: attachTarget{Name: "me@devbox", SSH: "me@devbox"}, server: "me@devbox"},
	} {
		got := viewCommand("/opt/kit/bin/kit", home, test.target, "session_1", file)
		want := []string{
			"/usr/bin/env", "KIT_HOME=" + home, "/opt/kit/bin/kit", "attach", test.server, "--session", "session_1",
			"--view", "file", "--path", "internal/x.go", "--line", "40", "--annotation", "3", "--view-id", testViewID,
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: viewCommand() = %q, want %q", test.name, got, want)
		}
	}
	subagent := viewCommand("/kit", home, attachTarget{}, "session_1", View{Kind: ViewSubagent, Conversation: "conversation_1"})
	if want := []string{"/usr/bin/env", "KIT_HOME=" + home, "/kit", "--session", "session_1", "--view", "subagent", "--conversation", "conversation_1"}; !slices.Equal(subagent, want) {
		t.Errorf("viewCommand(subagent) = %q, want %q", subagent, want)
	}
}

func TestAViewOfThisHomesServerNeverStartsIt(t *testing.T) {
	home := isolatedHome(t)
	for _, args := range [][]string{
		{"--session", "session_1", "--view", "diff"},
		{"attach", "local", "--session", "session_1", "--view", "diff"},
	} {
		var stdout, stderr bytes.Buffer
		invocation := Parse(context.Background(), args, &stdout, &stderr)
		environment := OpenEnvironment(context.Background(), invocation, &stderr)
		environment.Close()
		want := "kit: server \"local\" is not running; start it with `KIT_HOME=" + home + " kit server start`\n"
		if environment.ExitCode != 1 || stderr.String() != want {
			t.Fatalf("%q: exit = %d, stderr = %q, want %q", args, environment.ExitCode, stderr.String(), want)
		}
		if _, err := os.Stat(filepath.Join(home, "run")); !os.IsNotExist(err) {
			t.Fatalf("%q: the home's run directory exists (%v); a view started its server", args, err)
		}
	}
}

func TestViewLauncherBuildsTheViewCommandLine(t *testing.T) {
	launcher := &ViewLauncher{executable: "/kit", home: "/home/.kit", target: attachTarget{Name: "dev", Home: "/srv/dev"}}
	got := launcher.Command("session_1", View{Kind: ViewScratchpad})
	want := []string{"/usr/bin/env", "KIT_HOME=/home/.kit", "/kit", "attach", "/srv/dev", "--session", "session_1", "--view", "scratchpad"}
	if !slices.Equal(got, want) {
		t.Errorf("Command() = %q, want %q", got, want)
	}
	var none *ViewLauncher
	if command := none.Command("session_1", View{Kind: ViewDiff}); command != nil {
		t.Errorf("nil ViewLauncher Command() = %q, want nil", command)
	}
}

func TestNewViewIDReturnsDistinctValidIdentifiers(t *testing.T) {
	first, second := NewViewID(), NewViewID()
	if !protocol.ValidViewID(first) || !protocol.ValidViewID(second) || first == second {
		t.Fatalf("NewViewID() = %q, %q; want two distinct valid identifiers", first, second)
	}
}
