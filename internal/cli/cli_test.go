package cli

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/auth"
	kitserver "github.com/akonwi/kit/internal/server"
)

func TestRunVersion(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "kit ") {
		t.Fatalf("stdout = %q, want version", stdout.String())
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	const expected = `A native terminal coding agent

Usage:
  kit [flags]
  kit [command]

Available Commands:
  auth        Manage provider credentials
  help        Help about any command
  new         Create a persisted session and launch the TUI
  print       Run one headless turn
  server      Manage the local Kit server
  sessions    Manage and open saved sessions
  version     Print version information

Flags:
      --cwd string        working directory used for session lookup or creation
  -h, --help              help for kit
      --model string      override the startup provider/model
  -s, --session string    open a long or unambiguous short session ID
      --temp              use a temporary session discarded on exit
      --thinking string   override the startup reasoning level
  -v, --version           print version information

Use "kit [command] --help" for more information about a command.
`
	if stdout.String() != expected || stderr.Len() != 0 {
		t.Fatalf("help output:\nstdout=%q\nstderr=%q", stdout.String(), stderr.String())
	}
}

func TestPrintHelpIsExactAndDoesNotRunPrint(t *testing.T) {
	t.Parallel()
	deps := commandDependencies{print: func(context.Context, printOptions, io.Writer, io.Writer) int {
		t.Fatal("print ran while rendering help")
		return 1
	}}
	var stdout, stderr bytes.Buffer
	code := executeCommand(context.Background(), []string{"print", "--help"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	const expected = `Run one headless turn

Usage:
  kit print [--] PROMPT... [flags]

Flags:
      --cwd string        working directory used for session lookup or creation
  -h, --help              help for print
      --model string      provider/model to use
      --name string       initial name when --new creates a session
      --new               create a persisted session instead of resuming
  -s, --session string    continue a long or unambiguous short session ID
      --temp              use a temporary session discarded on exit
      --thinking string   reasoning level to use
`
	if stdout.String() != expected || stderr.Len() != 0 {
		t.Fatalf("print help:\nstdout=%q\nstderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = executeCommand(context.Background(), []string{"help", "print"}, &stdout, &stderr, deps)
	if code != 0 || stdout.String() != expected || stderr.Len() != 0 {
		t.Fatalf("help print output: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestPrintCommandBuildsPromptFromStdinAndPositionals(t *testing.T) {
	t.Parallel()
	var captured printOptions
	deps := commandDependencies{
		stdin: strings.NewReader("piped context"), stdinIsTerminal: func() bool { return false },
		print: func(_ context.Context, options printOptions, _, _ io.Writer) int {
			captured = options
			return 0
		},
	}
	var stdout, stderr bytes.Buffer
	code := executeCommand(context.Background(), []string{"print", "--temp", "review", "this"}, &stdout, &stderr, deps)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if captured.Prompt != "piped context\nreview this" || !captured.Temporary {
		t.Fatalf("print options = %+v", captured)
	}
}

func TestInteractiveCommandsProjectValidatedOptions(t *testing.T) {
	t.Parallel()
	var captured []interactiveOptions
	deps := commandDependencies{interactive: func(_ context.Context, options interactiveOptions, _, _ io.Writer) int {
		captured = append(captured, options)
		return 0
	}}
	var stdout, stderr bytes.Buffer
	if code := executeCommand(context.Background(), []string{"--session", "01234567"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("session startup exit = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCommand(context.Background(), []string{"new", "--name", "Focused work", "--cwd", ".", "--model", "test/echo", "--thinking", "high"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("new startup exit = %d, stderr = %q", code, stderr.String())
	}
	if len(captured) != 2 || captured[0].SessionID != "01234567" {
		t.Fatalf("captured startup options = %+v", captured)
	}
	created := captured[1]
	if created.NewSessionID == "" || created.Name != "Focused work" || created.CWD != "." || created.Model != "test/echo" || created.Thinking != "high" {
		t.Fatalf("new options = %+v", created)
	}
}

func TestPrintCommandAcceptsItsOptionsBeforeSubcommand(t *testing.T) {
	t.Parallel()
	var captured printOptions
	deps := commandDependencies{
		stdinIsTerminal: func() bool { return true },
		print: func(_ context.Context, options printOptions, _, _ io.Writer) int {
			captured = options
			return 0
		},
	}
	var stdout, stderr bytes.Buffer
	if code := executeCommand(context.Background(), []string{"--temp", "print", "prompt"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !captured.Temporary {
		t.Fatalf("print options = %+v", captured)
	}
}

func TestPrintCommandRejectsOversizedPipedInput(t *testing.T) {
	t.Parallel()
	called := false
	deps := commandDependencies{
		stdin: strings.NewReader(strings.Repeat("x", maxPrintPromptBytes+1)), stdinIsTerminal: func() bool { return false },
		print: func(context.Context, printOptions, io.Writer, io.Writer) int {
			called = true
			return 0
		},
	}
	var stdout, stderr bytes.Buffer
	if code := executeCommand(context.Background(), []string{"print"}, &stdout, &stderr, deps); code != 2 {
		t.Fatalf("oversized input exit = %d, stderr = %q", code, stderr.String())
	}
	if called || !strings.Contains(stderr.String(), "exceeds 128 KiB") {
		t.Fatalf("called = %t stderr = %q", called, stderr.String())
	}
}

func TestPrintCommandAcceptsOptionLikePromptAfterSeparator(t *testing.T) {
	t.Parallel()
	var prompt string
	deps := commandDependencies{
		stdinIsTerminal: func() bool { return true },
		print: func(_ context.Context, options printOptions, _, _ io.Writer) int {
			prompt = options.Prompt
			return 0
		},
	}
	var stdout, stderr bytes.Buffer
	if code := executeCommand(context.Background(), []string{"print", "--", "--summarize", "this"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if prompt != "--summarize this" {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestCommandSelectionValidationPrecedesExecution(t *testing.T) {
	t.Parallel()
	called := false
	deps := commandDependencies{
		stdinIsTerminal: func() bool { return true },
		interactive: func(context.Context, interactiveOptions, io.Writer, io.Writer) int {
			called = true
			return 0
		},
		print: func(context.Context, printOptions, io.Writer, io.Writer) int {
			called = true
			return 0
		},
	}
	for _, arguments := range [][]string{
		{"--session", "abc", "--temp"},
		{"print", "--session", "abc", "--new", "prompt"},
		{"print", "--name", "named", "prompt"},
		{"-p", "prompt"},
		{"--print", "prompt"},
		{"--no-session"},
		{"--model", "missing-slash"},
		{"--session", "abc", "new"},
		{"--temp", "sessions"},
	} {
		called = false
		var stdout, stderr bytes.Buffer
		if code := executeCommand(context.Background(), arguments, &stdout, &stderr, deps); code != 2 {
			t.Fatalf("executeCommand(%q) exit = %d, stderr = %q", arguments, code, stderr.String())
		}
		if called {
			t.Fatalf("executeCommand(%q) invoked application dependency", arguments)
		}
	}
}

func TestCanceledRuntimeFailureUsesInterruptExitCode(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := commandDependencies{interactive: func(context.Context, interactiveOptions, io.Writer, io.Writer) int { return 1 }}
	var stdout, stderr bytes.Buffer
	if code := executeCommand(ctx, nil, &stdout, &stderr, deps); code != 130 {
		t.Fatalf("canceled exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestDeadlineRuntimeFailureIsNotReportedAsSignal(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	deps := commandDependencies{interactive: func(context.Context, interactiveOptions, io.Writer, io.Writer) int { return 1 }}
	var stdout, stderr bytes.Buffer
	if code := executeCommand(ctx, nil, &stdout, &stderr, deps); code != 1 {
		t.Fatalf("deadline exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestThreadsAliasRunsSessionPicker(t *testing.T) {
	t.Parallel()
	called := false
	deps := commandDependencies{sessions: func(context.Context, interactiveOptions, io.Writer, io.Writer) int {
		called = true
		return 0
	}}
	var stdout, stderr bytes.Buffer
	if code := executeCommand(context.Background(), []string{"threads"}, &stdout, &stderr, deps); code != 0 || !called {
		t.Fatalf("threads exit = %d called = %t stderr = %q", code, called, stderr.String())
	}
}

func TestServerCommandsDispatchLifecycleActions(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"start", "status", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			var got []string
			deps := commandDependencies{server: func(_ context.Context, args []string, _, _ io.Writer) int {
				got = args
				return 0
			}}
			var stdout, stderr bytes.Buffer
			code := executeCommand(context.Background(), []string{"server", action}, &stdout, &stderr, deps)
			if code != 0 || len(got) != 1 || got[0] != action {
				t.Fatalf("server %s: exit = %d, dispatched = %q, stderr = %q", action, code, got, stderr.String())
			}
		})
	}
}

func TestAuthAndServerCommandTreesExposeHelp(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		arguments []string
		contains  []string
	}{
		{arguments: []string{"auth", "--help"}, contains: []string{"login", "logout", "status"}},
		{arguments: []string{"auth", "login", "--help"}, contains: []string{"openai-codex"}},
		{arguments: []string{"server", "--help"}, contains: []string{"start", "status", "stop", "restart"}},
		{arguments: []string{"server", "restart", "--help"}, contains: []string{"Restart the server"}},
	} {
		var stdout, stderr bytes.Buffer
		if code := executeCommand(context.Background(), test.arguments, &stdout, &stderr, commandDependencies{}); code != 0 {
			t.Fatalf("help %q exit = %d, stderr = %q", test.arguments, code, stderr.String())
		}
		for _, expected := range test.contains {
			if !strings.Contains(stdout.String(), expected) {
				t.Fatalf("help %q missing %q:\n%s", test.arguments, expected, stdout.String())
			}
		}
	}
}

func TestHiddenServerHasNoPublicHelpTopic(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"help", "__server"}, &stdout, &stderr); code != 2 {
		t.Fatalf("hidden help exit = %d, stdout = %q stderr = %q", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("hidden help leaked command surface: stdout = %q stderr = %q", stdout.String(), stderr.String())
	}
}

func TestRunNewHelpAndArgumentValidation(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"new", "--help"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "kit new") || stderr.Len() != 0 {
		t.Fatalf("new help exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"new", "unexpected"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unexpected") {
		t.Fatalf("new argument exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestSupportsInteractiveAPIKeyLoginRequiresBothProviderSources(t *testing.T) {
	t.Parallel()
	registry := kitserver.Registry{CredentialSources: map[string]kitserver.CredentialSource{
		auth.OpenAIProviderID: kitserver.CredentialSourceStore,
	}}
	if supportsInteractiveAPIKeyLogin(registry) {
		t.Fatal("partial API-key credential metadata was accepted")
	}
	registry.CredentialSources[auth.AnthropicProviderID] = kitserver.CredentialSourceEnvironment
	if !supportsInteractiveAPIKeyLogin(registry) {
		t.Fatal("complete API-key credential metadata was rejected")
	}
}

func TestInteractiveProvidersPreferCodexCredentials(t *testing.T) {
	t.Parallel()
	providers, model := interactiveProviders([]string{"anthropic", "openai", "openai-codex"})
	if !providers["openai-codex"] || !providers["openai"] || !providers["anthropic"] {
		t.Fatalf("providers = %#v", providers)
	}
	if model != "openai-codex/gpt-5.6-sol" {
		t.Fatalf("default model = %q", model)
	}
}

func TestInteractiveLocationLeavesGitPresentationToSessionStatus(t *testing.T) {
	directory := t.TempDir()
	location := interactiveLocation(context.Background(), directory)
	if location != directory {
		t.Fatalf("location = %q, want %q", location, directory)
	}
}

func TestServerStatusWhenUnavailable(t *testing.T) {
	t.Setenv(apphome.EnvHome, filepath.Join(t.TempDir(), "kit"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"server", "status"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "server unavailable") {
		t.Fatalf("stderr = %q, want unavailable message", stderr.String())
	}
}
