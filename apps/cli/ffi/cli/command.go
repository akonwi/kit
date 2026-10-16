package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/identifier"
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
	internalServer  func(context.Context, []string, io.Writer) int
	auth            func(context.Context, []string, io.Writer, io.Writer) int
	server          func(context.Context, []string, io.Writer, io.Writer) int
	servers         func(context.Context, []string, io.Writer, io.Writer) int
	// pair prints pairing links for a server, "" for this home's, with the
	// given extra server URLs.
	pair    func(context.Context, string, []string, io.Writer, io.Writer) int
	devices func(context.Context, []string, io.Writer, io.Writer) int
	// expose serves this home's server on the tailnet, or stops with off.
	expose func(context.Context, exposeOptions, bool, io.Writer, io.Writer) int
	// attach runs the terminal client for the named server, or picks one when
	// the name is empty.
	attach func(context.Context, interactiveOptions, string, io.Writer, io.Writer) int
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{
		stdin:           os.Stdin,
		stdinIsTerminal: stdinIsTerminal,
		print:           runPrintOptions,
		internalServer:  runInternalServer,
		auth:            runAuthCommand,
		server:          runServerCommand,
		servers:         runServersCommand,
		pair: func(ctx context.Context, server string, urls []string, stdout, stderr io.Writer) int {
			return runPair(ctx, server, urls, stdout, stderr, stdoutIsTerminal())
		},
		devices: runServerDevices,
		expose:  runExpose,
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
		newServerCommand(deps),
		newServersCommand(deps),
		newAttachCommand(deps),
		newPairCommand(deps),
		newVersionCommand(),
		newInternalServerCommand(deps),
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
			Use: "status", Short: "List providers and whether the server is logged in", Args: cobra.NoArgs,
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
			Use: "logout PROVIDER", Short: "Remove the server's saved credentials for a provider", Args: cobra.ExactArgs(1),
			RunE: func(command *cobra.Command, arguments []string) error {
				return resultForCode(deps.auth(command.Context(), append([]string{"logout"}, arguments...), command.OutOrStdout(), command.ErrOrStderr()))
			},
		},
	)
	return command
}

func newServerCommand(deps commandDependencies) *cobra.Command {
	command := &cobra.Command{
		Use: "server", Short: "Manage the local Kit server", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	for _, action := range []struct {
		name, description string
		listen            bool
	}{
		{name: "start", description: "Start or discover the server", listen: true},
		{name: "status", description: "Inspect server status"},
		{name: "stop", description: "Stop the server"},
		{name: "restart", description: "Restart the server", listen: true},
		{name: "run", description: "Run the server in the foreground, logging to stderr", listen: true},
	} {
		action := action
		var listen string
		subcommand := &cobra.Command{
			Use: action.name, Short: action.description, Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				arguments := []string{action.name}
				if listen != "" {
					arguments = append(arguments, "--listen", listen)
				}
				return resultForCode(deps.server(command.Context(), arguments, command.OutOrStdout(), command.ErrOrStderr()))
			},
		}
		if action.listen {
			subcommand.Flags().StringVar(&listen, "listen", "", "listen on host:port instead of the server.listen setting")
		}
		command.AddCommand(subcommand)
	}
	var urls []string
	pair := &cobra.Command{
		Use:   "pair",
		Short: "Print a pairing link for another device",
		Long: "Print a single-use pairing link for each URL that reaches the running server: each\n" +
			"server.allowedHosts entry, each --url, and the loopback URL. The code expires after\n" +
			"5 minutes. Redeeming it gives the other device a token for this server.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return resultForCode(deps.pair(command.Context(), "", urls, command.OutOrStdout(), command.ErrOrStderr()))
		},
	}
	pair.Flags().StringArrayVar(&urls, "url", nil, "also print a link for this server URL, such as a reverse proxy's")
	devices := &cobra.Command{
		Use: "devices", Short: "Manage paired devices", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return resultForCode(deps.devices(command.Context(), []string{"list"}, command.OutOrStdout(), command.ErrOrStderr()))
		},
	}
	devices.AddCommand(
		&cobra.Command{
			Use: "list", Aliases: []string{"ls"}, Short: "List paired devices", Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return resultForCode(deps.devices(command.Context(), []string{"list"}, command.OutOrStdout(), command.ErrOrStderr()))
			},
		},
		&cobra.Command{
			Use: "revoke ID-OR-NAME", Short: "Revoke a device's access", Args: cobra.ExactArgs(1),
			RunE: func(command *cobra.Command, arguments []string) error {
				return resultForCode(deps.devices(command.Context(), append([]string{"revoke"}, arguments...), command.OutOrStdout(), command.ErrOrStderr()))
			},
		},
	)
	var tailscale, off, force bool
	var httpsPort int
	expose := &cobra.Command{
		Use:   "expose --tailscale",
		Short: "Serve this server on your tailnet with Tailscale Serve",
		Long: "Serve this server on your tailnet over HTTPS with Tailscale Serve, then print a\n" +
			"pairing link. It fixes server.listen to a loopback port when none is set, adds\n" +
			"the machine's tailnet name to server.allowedHosts, and restarts the server when\n" +
			"those change. It never uses Tailscale Funnel, so only your tailnet can reach it.\n" +
			"--off removes the Serve handler and the settings it added.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !tailscale {
				return usagef("kit server expose requires --tailscale")
			}
			if httpsPort < 1 || httpsPort > 65535 {
				return usagef("--https-port must be between 1 and 65535")
			}
			if force && !off {
				return usagef("--force applies only with --off")
			}
			options := defaultExposeOptions(httpsPort, force, stdoutIsTerminal())
			return resultForCode(deps.expose(command.Context(), options, off, command.OutOrStdout(), command.ErrOrStderr()))
		},
	}
	expose.Flags().BoolVar(&tailscale, "tailscale", false, "serve with Tailscale Serve")
	expose.Flags().BoolVar(&off, "off", false, "stop serving and remove the settings expose added")
	expose.Flags().BoolVar(&force, "force", false, "with --off, remove the settings even if the Serve handler cannot be confirmed removed")
	expose.Flags().IntVar(&httpsPort, "https-port", 443, "tailnet HTTPS port to serve on")
	command.AddCommand(pair, devices, expose)
	return command
}

func newServersCommand(deps commandDependencies) *cobra.Command {
	run := func(command *cobra.Command, arguments []string) error {
		return resultForCode(deps.servers(command.Context(), arguments, command.OutOrStdout(), command.ErrOrStderr()))
	}
	command := &cobra.Command{
		Use: "servers", Short: "Manage the servers kit attach connects to", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return run(command, []string{"list"}) },
	}
	command.AddCommand(
		&cobra.Command{
			Use: "list", Aliases: []string{"ls"}, Short: "List saved servers and their status", Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error { return run(command, []string{"list"}) },
		},
		&cobra.Command{
			Use: "add NAME HOME-OR-LINK", Short: "Save a Kit home's server, or pair with a link and save its server", Args: cobra.ExactArgs(2),
			RunE: func(command *cobra.Command, arguments []string) error {
				return run(command, append([]string{"add"}, arguments...))
			},
		},
		&cobra.Command{
			Use: "remove NAME", Aliases: []string{"rm"}, Short: "Forget a saved server", Args: cobra.ExactArgs(1),
			RunE: func(command *cobra.Command, arguments []string) error {
				return run(command, append([]string{"remove"}, arguments...))
			},
		},
	)
	return command
}

func newPairCommand(deps commandDependencies) *cobra.Command {
	var urls []string
	command := &cobra.Command{
		Use:   "pair [SERVER]",
		Short: "Print a pairing link for another device",
		Long: "Print a single-use pairing link that pairs another device with SERVER: a saved server,\n" +
			"a URL or Kit home path, or this home's server by default. The code expires after 5 minutes.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			server := ""
			if len(arguments) == 1 {
				server = arguments[0]
			}
			return resultForCode(deps.pair(command.Context(), server, urls, command.OutOrStdout(), command.ErrOrStderr()))
		},
	}
	command.Flags().StringArrayVar(&urls, "url", nil, "also print a link for this server URL, such as a reverse proxy's")
	return command
}

func newAttachCommand(deps commandDependencies) *cobra.Command {
	var options interactiveOptions
	command := &cobra.Command{
		Use:   "attach [SERVER]",
		Short: "Launch the TUI for another Kit server",
		Long: "Launch the TUI for a saved server, \"local\" for this home's server, a pairing link or URL\n" +
			"of a saved server, or a Kit home path. A pairing link re-pairs this device first. Without\n" +
			"SERVER, pick from the saved servers. Attaching never starts or stops a server.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			if err := validateInteractiveOptions(options, false); err != nil {
				return err
			}
			server := ""
			if len(arguments) == 1 {
				server = arguments[0]
			}
			return resultForCode(deps.attach(command.Context(), options, server, command.OutOrStdout(), command.ErrOrStderr()))
		},
	}
	bindInteractiveFlags(command, &options)
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
			// A home that is not the familiar default is worth naming, so nobody
			// has to guess which state a binary owns.
			if line, isolated := homeLine(); isolated {
				fmt.Fprintf(command.OutOrStdout(), "home: %s\n", line)
			}
		},
	}
}

// homeLine names the resolved home when it is not the shared default one.
// A failure is reported rather than guessed at, and never claimed to be an
// override.
func homeLine() (string, bool) {
	paths, err := apphome.Resolve("")
	if err != nil {
		return "unknown: " + err.Error(), true
	}
	sharedHome := filepath.Join(defaultUserHome(), apphome.DefaultDirectory)
	return paths.Home, paths.Home != sharedHome
}

// defaultUserHome is the user's home without its trailing separator.
func defaultUserHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func newInternalServerCommand(deps commandDependencies) *cobra.Command {
	return &cobra.Command{
		Use:                "__server",
		Hidden:             true,
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return resultForCode(deps.internalServer(command.Context(), arguments, command.ErrOrStderr()))
		},
	}
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
