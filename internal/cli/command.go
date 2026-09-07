package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/version"
	"github.com/spf13/cobra"
)

const maxPrintPromptBytes = 128 << 10

type commandDependencies struct {
	stdin           io.Reader
	stdinIsTerminal func() bool
	interactive     func(context.Context, interactiveOptions, io.Writer, io.Writer) int
	print           func(context.Context, printOptions, io.Writer, io.Writer) int
	sessions        func(context.Context, interactiveOptions, io.Writer, io.Writer) int
	internalDaemon  func(context.Context, []string, io.Writer) int
	auth            func(context.Context, []string, io.Writer, io.Writer) int
	daemon          func(context.Context, []string, io.Writer, io.Writer) int
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{
		stdin:           os.Stdin,
		stdinIsTerminal: stdinIsTerminal,
		interactive:     runInteractive,
		print:           runPrintOptions,
		sessions:        runSessions,
		internalDaemon:  runInternalDaemon,
		auth:            runAuthCommand,
		daemon:          runDaemonCommand,
	}
}

type reportedExit struct{ code int }

func (e reportedExit) Error() string { return fmt.Sprintf("exit status %d", e.code) }

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func usagef(format string, arguments ...any) error {
	return usageError{message: fmt.Sprintf(format, arguments...)}
}

func resultForCode(code int) error {
	if code == 0 {
		return nil
	}
	return reportedExit{code: code}
}

// Run constructs and executes Kit's public command tree and returns a stable
// process exit code. The executable boundary remains responsible for os.Exit.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return executeCommand(ctx, args, stdout, stderr, defaultCommandDependencies())
}

func executeCommand(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	deps commandDependencies,
) int {
	command := newRootCommand(deps)
	command.SetArgs(args)
	command.SetOut(stdout)
	command.SetErr(stderr)
	err := command.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var reported reportedExit
	if errors.As(err, &reported) {
		if reported.code == 1 && errors.Is(ctx.Err(), context.Canceled) {
			return 130
		}
		return reported.code
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "kit: interrupted")
		return 130
	}
	var usage usageError
	if errors.As(err, &usage) {
		fmt.Fprintf(stderr, "kit: %s\n", usage.message)
		return 2
	}
	// Cobra parse, unknown-command, and argument errors are usage failures.
	fmt.Fprintf(stderr, "kit: %v\n", err)
	return 2
}

func newRootCommand(deps commandDependencies) *cobra.Command {
	var options interactiveOptions
	root := &cobra.Command{
		Use:           "kit",
		Short:         "A native terminal coding agent",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := validateInteractiveOptions(options, false); err != nil {
				return err
			}
			return resultForCode(deps.interactive(command.Context(), options, command.OutOrStdout(), command.ErrOrStderr()))
		},
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			if command.Parent() == nil {
				return nil
			}
			name := ""
			switch {
			case options.SessionID != "":
				name = "session"
			case options.Temporary:
				name = "temp"
			case options.Model != "":
				name = "model"
			case options.Thinking != "":
				name = "thinking"
			case options.CWD != "":
				name = "cwd"
			}
			if name != "" {
				return usagef("--%s is not valid for %s", name, command.CommandPath())
			}
			return nil
		},
		Version: version.Version + " (" + version.Commit + ")",
	}
	root.SetVersionTemplate("kit {{.Version}}\n")
	root.InitDefaultVersionFlag()
	root.Flags().Lookup("version").Usage = "print version information"
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{message: err.Error()}
	})
	bindInteractiveFlags(root, &options)

	root.AddCommand(
		newNewCommand(deps),
		newSessionsCommand(deps),
		newPrintCommand(deps),
		newAuthCommand(deps),
		newDaemonCommand(deps),
		newVersionCommand(),
		newInternalDaemonCommand(deps),
	)
	root.SetHelpCommand(newHelpCommand(root))
	root.InitDefaultHelpFlag()
	return root
}

func bindInteractiveFlags(command *cobra.Command, options *interactiveOptions) {
	flags := command.Flags()
	flags.StringVarP(&options.SessionID, "session", "s", "", "open a long or unambiguous short session ID")
	flags.BoolVar(&options.Temporary, "temp", false, "use a temporary session discarded on exit")
	flags.StringVar(&options.Model, "model", "", "override the startup provider/model")
	flags.StringVar(&options.Thinking, "thinking", "", "override the startup reasoning level")
	flags.StringVar(&options.CWD, "cwd", "", "working directory used for session lookup or creation")
}

func validateInteractiveOptions(options interactiveOptions, creating bool) error {
	if options.SessionID != "" && options.Temporary {
		return usagef("--session and --temp are mutually exclusive")
	}
	if options.SessionID != "" && options.CWD != "" {
		return usagef("--cwd cannot be combined with --session")
	}
	if options.SessionID != "" && (options.Model != "" || options.Thinking != "") {
		return usagef("--model and --thinking cannot be combined with --session")
	}
	if options.Model != "" && !validModelSelector(options.Model) {
		return usagef("--model expects PROVIDER/MODEL")
	}
	if creating && options.SessionID != "" {
		return usagef("kit new cannot be combined with --session")
	}
	if creating && options.Temporary {
		return usagef("kit new cannot be combined with --temp")
	}
	return nil
}

func newNewCommand(deps commandDependencies) *cobra.Command {
	var options interactiveOptions
	command := &cobra.Command{
		Use:   "new",
		Short: "Create a persisted session and launch the TUI",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := validateInteractiveOptions(options, true); err != nil {
				return err
			}
			sessionID, err := identifier.New("session_")
			if err != nil {
				fmt.Fprintf(command.ErrOrStderr(), "kit: prepare new session: %v\n", err)
				return reportedExit{code: 1}
			}
			options.NewSessionID = sessionID
			return resultForCode(deps.interactive(command.Context(), options, command.OutOrStdout(), command.ErrOrStderr()))
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.Name, "name", "", "initial session name")
	flags.StringVar(&options.Model, "model", "", "startup provider/model")
	flags.StringVar(&options.Thinking, "thinking", "", "startup reasoning level")
	flags.StringVar(&options.CWD, "cwd", "", "working directory for the new session")
	return command
}

func newSessionsCommand(deps commandDependencies) *cobra.Command {
	return &cobra.Command{
		Use:     "sessions",
		Aliases: []string{"threads"},
		Short:   "Manage and open saved sessions",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return resultForCode(deps.sessions(command.Context(), interactiveOptions{}, command.OutOrStdout(), command.ErrOrStderr()))
		},
	}
}

func newPrintCommand(deps commandDependencies) *cobra.Command {
	var options printOptions
	command := &cobra.Command{
		Use:   "print [--] PROMPT...",
		Short: "Run one headless turn",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			if err := validatePrintOptions(options); err != nil {
				return err
			}
			stdin := ""
			if deps.stdin != nil && (deps.stdinIsTerminal == nil || !deps.stdinIsTerminal()) {
				content, err := io.ReadAll(io.LimitReader(deps.stdin, maxPrintPromptBytes+1))
				if err != nil {
					fmt.Fprintf(command.ErrOrStderr(), "kit: read stdin: %v\n", err)
					return reportedExit{code: 1}
				}
				if len(content) > maxPrintPromptBytes {
					return usagef("print input exceeds 128 KiB")
				}
				stdin = string(content)
			}
			positional := strings.Join(arguments, " ")
			separator := ""
			if stdin != "" && positional != "" && !strings.HasSuffix(stdin, "\n") {
				separator = "\n"
			}
			options.Prompt = stdin + separator + positional
			if len(options.Prompt) > maxPrintPromptBytes {
				return usagef("print input exceeds 128 KiB")
			}
			if strings.TrimSpace(options.Prompt) == "" {
				return usagef("print requires a prompt from arguments or stdin")
			}
			if err := (protocol.PromptInput{Text: options.Prompt}).Validate(); err != nil {
				return usagef("invalid print prompt: %v", err)
			}
			return resultForCode(deps.print(command.Context(), options, command.OutOrStdout(), command.ErrOrStderr()))
		},
	}
	flags := command.Flags()
	flags.StringVarP(&options.SessionID, "session", "s", "", "continue a long or unambiguous short session ID")
	flags.BoolVar(&options.NewSession, "new", false, "create a persisted session instead of resuming")
	flags.BoolVar(&options.Temporary, "temp", false, "use a temporary session discarded on exit")
	flags.StringVar(&options.Model, "model", "", "provider/model to use")
	flags.StringVar(&options.ThinkingLevel, "thinking", "", "reasoning level to use")
	flags.StringVar(&options.CWD, "cwd", "", "working directory used for session lookup or creation")
	flags.StringVar(&options.Name, "name", "", "initial name when --new creates a session")
	return command
}

func validatePrintOptions(options printOptions) error {
	selections := 0
	if options.SessionID != "" {
		selections++
	}
	if options.NewSession {
		selections++
	}
	if options.Temporary {
		selections++
	}
	if selections > 1 {
		return usagef("--session, --new, and --temp are mutually exclusive")
	}
	if options.SessionID != "" && options.CWD != "" {
		return usagef("--cwd cannot be combined with --session")
	}
	if options.Name != "" && !options.NewSession {
		return usagef("--name requires --new")
	}
	if options.SessionID != "" && (options.Model != "" || options.ThinkingLevel != "") {
		return usagef("--model and --thinking cannot be combined with --session")
	}
	if options.Model != "" && !validModelSelector(options.Model) {
		return usagef("--model expects PROVIDER/MODEL")
	}
	return nil
}

func validModelSelector(selector string) bool {
	provider, model, ok := strings.Cut(selector, "/")
	return ok && provider != "" && model != ""
}

func newAuthCommand(deps commandDependencies) *cobra.Command {
	command := &cobra.Command{
		Use: "auth", Short: "Manage provider credentials", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(
		&cobra.Command{
			Use: "status", Short: "List credential metadata", Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return resultForCode(deps.auth(command.Context(), []string{"status"}, command.OutOrStdout(), command.ErrOrStderr()))
			},
		},
		&cobra.Command{
			Use: "login openai-codex", Short: "Log in to OpenAI Codex", Args: cobra.ExactArgs(1),
			RunE: func(command *cobra.Command, arguments []string) error {
				if arguments[0] != "openai-codex" {
					return usagef("unsupported authentication provider %q", arguments[0])
				}
				return resultForCode(deps.auth(command.Context(), append([]string{"login"}, arguments...), command.OutOrStdout(), command.ErrOrStderr()))
			},
		},
		&cobra.Command{
			Use: "logout openai-codex", Short: "Remove saved OpenAI Codex credentials", Args: cobra.ExactArgs(1),
			RunE: func(command *cobra.Command, arguments []string) error {
				if arguments[0] != "openai-codex" {
					return usagef("unsupported authentication provider %q", arguments[0])
				}
				return resultForCode(deps.auth(command.Context(), append([]string{"logout"}, arguments...), command.OutOrStdout(), command.ErrOrStderr()))
			},
		},
	)
	return command
}

func newDaemonCommand(deps commandDependencies) *cobra.Command {
	command := &cobra.Command{
		Use: "daemon", Short: "Manage the local Kit daemon", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	for _, action := range []struct {
		name, description string
	}{
		{name: "start", description: "Start or discover the daemon"},
		{name: "status", description: "Inspect daemon status"},
		{name: "stop", description: "Stop the daemon"},
		{name: "restart", description: "Restart the daemon"},
	} {
		action := action
		command.AddCommand(&cobra.Command{
			Use: action.name, Short: action.description, Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return resultForCode(deps.daemon(command.Context(), []string{action.name}, command.OutOrStdout(), command.ErrOrStderr()))
			},
		})
	}
	return command
}

func newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use: "help [command]", Short: "Help about any command", Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, arguments []string) error {
			target := root
			if len(arguments) > 0 {
				resolved, remaining, err := root.Find(arguments)
				if err != nil || resolved == nil || resolved.Hidden || len(remaining) != 0 {
					return usagef("unknown help topic %q", strings.Join(arguments, " "))
				}
				target = resolved
			}
			target.InitDefaultHelpFlag()
			return target.Help()
		},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use: "version", Short: "Print version information", Args: cobra.NoArgs,
		Run: func(command *cobra.Command, _ []string) {
			fmt.Fprintf(command.OutOrStdout(), "kit %s (%s)\n", version.Version, version.Commit)
		},
	}
}

func newInternalDaemonCommand(deps commandDependencies) *cobra.Command {
	return &cobra.Command{
		Use:                "__daemon",
		Hidden:             true,
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return resultForCode(deps.internalDaemon(command.Context(), arguments, command.ErrOrStderr()))
		},
	}
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
