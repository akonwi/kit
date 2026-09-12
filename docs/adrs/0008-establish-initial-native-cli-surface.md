# 0008: Establish the initial native CLI surface

## Status

Accepted

## Context

Kit's native executable needs a stable public command contract for its first
complete local workflow. That contract must make interactive startup, session
selection, headless execution, authentication, and daemon management
discoverable without exposing unfinished remote entry points.

The command layer is an adapter over application services. It must parse and
validate user intent, route cancellation, and present consistent help and exit
status, while session lifecycle, execution, persistence, daemon behavior, and
terminal state remain owned by their existing packages.

The initial CLI also needs a standalone session-management experience. It
should preserve shell scrollback, permit management before any session is
attached, and hand an exact selection to the normal native TUI.

## Decision

### Command framework and ownership

Kit uses [Cobra](https://github.com/spf13/cobra) for its public command tree,
flags, aliases, argument validation, and generated help.

The initial public tree is:

```text
kit
├── (no command)            launch the native TUI
├── new                     create a persisted session and launch the TUI
├── sessions                launch the session-management mini-TUI
├── print                   run one headless turn
├── auth
│   ├── status
│   ├── login
│   └── logout
├── daemon
│   ├── start
│   ├── status
│   ├── stop
│   └── restart
├── version
└── help
```

Cobra remains a thin adapter. A fresh command tree is constructed for each
invocation, receives its dependencies and output streams explicitly, and calls
application services through accepted interfaces. Command handlers do not own
session, daemon, runtime, or TUI state and do not call `os.Exit`.

The executable invokes the tree with `ExecuteContext`. Its signal-aware outer
boundary maps returned typed errors to process exit status. Shared execution
flags are registered through command-local helpers rather than inherited by
unrelated commands such as `auth` and `daemon`.

The internal `__daemon` process role remains hidden and undocumented. Cobra's
generated completion command is disabled until shell completion enters scope.

### Interactive startup

The interactive forms are:

```sh
kit
kit --session <id>
kit --temp
kit new
```

`kit` resumes the latest usable session for the selected working directory and
creates a persisted session when none is available. `kit --session <id>` opens
an exact long or unambiguous short session ID. `kit new` creates a new
persisted session without consulting resumable sessions.

`--temp` means “use a temporary session that is discarded on exit.” A temporary
session is not resumable and leaves no durable session artifacts after its
owning foreground command exits. Temporary metadata and droid history remain
process-local. Creation uses a client-selected identity, and a distinct server
disposal operation atomically blocks new admission, cancels active work, and
rejects persisted session IDs. Foreground commands install cleanup before the
creation request so ambiguous responses remain disposable. `--temp` has no
short alias. The previous `--no-session` spelling is not part of the native CLI
because a temporary conversation is still semantically a session.

Interactive execution accepts:

```text
-s, --session ID
    --temp
    --model PROVIDER/MODEL
    --thinking LEVEL
    --cwd PATH
```

`kit new` accepts `--name`, `--model`, `--thinking`, and `--cwd`. For implicit
resume, an explicit model or thinking level selects the newest matching session
and becomes the creation default when no session matches. Exact sessions retain
their saved runtime configuration, so `--model`, `--thinking`, and `--cwd` are
rejected with `--session` rather than ignored. Session-selection options are
validated before daemon discovery or TUI initialization.

### Print mode

Headless single-turn execution is an explicit subcommand:

```sh
kit print [options] [--] PROMPT...
```

Neither `-p` nor `--print` is supported. Print execution should read as a
command rather than a boolean mode flag, and `-p` remains available for a future
unrelated option.

Print mode joins positional arguments into the prompt and prepends piped stdin.
It supports `--` before prompt text that begins with a dash and rejects an empty
combined prompt.

Its session-selection options are mutually exclusive:

```text
--session ID
--new
--temp
```

Without one of those options, print mode resumes the latest usable session for
the selected working directory and creates a persisted session when necessary.
An explicit model or thinking level filters implicit resume and becomes the
creation default when no session matches. `--new` always creates a persisted
session, while `--temp` uses a disposable session. Print also accepts
`--model`, `--thinking`, and `--cwd`; `--name` is accepted only with `--new`.
Exact sessions retain their saved model and thinking level, so those overrides
are rejected with `--session`.

Print mode writes only final assistant prose to stdout. Diagnostics and failures
go to stderr. Interrupting the foreground wait requests cancellation of that
run before the command exits.

### Session-management mini-TUI

The canonical session-management command is:

```sh
kit sessions
```

`kit threads` remains a compatibility alias. The command lists the global saved
session directory and supports opening, renaming, confirmed deletion, and exit
without selection.

The picker is a standalone vaxis application rendered in a bounded terminal
primary-screen live region:

```go
ui.Run(sessionPicker, ui.WithDynamicPrimaryScreen())
```

This preserves shell scrollback and occupies only the rows preferred by the
picker. Enter returns the exact selected session ID and closes the live region;
the command then launches the normal alternate-screen TUI attached to that
session. Escape or Ctrl+C closes the picker without launching a session.

The embedded and standalone session explorers share controller and presentation
behavior. The embedded explorer switches an already attached client and cannot
delete its attached session. The standalone picker has no attached session, so
any listed session may be deleted and selection returns to its command runner.

### Authentication and daemon operations

The initial supporting commands are:

```sh
kit auth status
kit auth login openai-codex
kit auth logout openai-codex

kit daemon start
kit daemon status
kit daemon stop
kit daemon restart
```

They preserve the credential-store and daemon-manager ownership boundaries and
existing operation deadlines.

### Help, validation, and process behavior

The public help and version forms are:

```sh
kit --help
kit help
kit help <command>
kit <command> --help
kit --version
kit version
```

Help and version handling do not initialize the daemon or TUI. Every public
command has command-specific help. For executable command paths, unknown
commands, unknown flags, unexpected positional arguments, and invalid flag
combinations fail as usage errors. Cobra's help and version flags retain their
conventional short-circuit behavior even when followed by otherwise unused
arguments. Runtime failures produce concise diagnostics without automatically
printing the entire usage block.

The executable uses these stable exit codes:

```text
0     success
1     runtime failure
2     usage error
130   interrupted by SIGINT
143   terminated by SIGTERM
```

### Deferred command surfaces

This initial command tree does not expose:

- stdio RPC mode;
- semantic web serving or remote session transports;
- `kit attach`;
- shell completion generation or installation; or
- noninteractive `kit sessions list`, `rename`, and `delete` commands.

These are deferred rather than rejected and may be added as explicit commands
when their application services are ready. Browser-hosted `web-tui` remains
removed by the native architecture decision.

## Required properties

The implementation must demonstrate:

- root invocation launches the native TUI while every help/version form exits
  without TUI or daemon initialization;
- default, exact, new, and temporary session selection have distinct validated
  behavior;
- temporary sessions leave no durable session artifacts after exit;
- print mode preserves stdout for final assistant prose and stderr for
  diagnostics;
- print interruption aborts its foreground run;
- invalid syntax and combinations return exit code 2, while runtime failures
  return exit code 1;
- the standalone session picker preserves shell scrollback, supports list,
  open, rename, and delete, and cleans up before the main TUI starts;
- command tests can construct isolated trees with injected dependencies and
  streams; and
- the internal daemon role remains callable by the process manager but absent
  from public help.

## Consequences

### Positive

- Kit has a conventional, discoverable, and testable CLI contract.
- Explicit commands replace positional special cases and boolean execution
  modes.
- Help and validation remain consistent as the command tree grows.
- `--temp` describes lifecycle more accurately than saying no session exists.
- The standalone picker reuses native session-management behavior while
  preserving shell history.
- Application behavior remains reusable by future local and remote adapters.

### Trade-offs

- Cobra and its flag package become dependencies of the released executable.
- Generated help becomes a user-visible contract that requires presentation
  tests.
- Existing invocations using `-p`, `--print`, or `--no-session` must adopt
  `print` and `--temp`.
- Moving from the primary-screen picker into the alternate-screen TUI requires
  careful terminal cleanup and manual verification across supported terminals.
- Temporary-session disposal is deterministic for orderly return and handled
  signals; an owner killed without cleanup can leave process-local state in the
  daemon until that daemon exits, but leaves no durable registry or droid data.
- Deferred production modes will extend this tree later and must preserve the
  ownership and error conventions established here.

## Related

This ADR supersedes the initial local-client command spellings shown in ADR
0001 while preserving its executable-role and ownership architecture.

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [0006: Make droids authoritative for session conversation data](./0006-droids-as-session-data-authority.md)
- [0009: Keep temporary sessions process-local and defer owner leases](./0009-keep-temporary-sessions-process-local.md)
- [`../roadmap.md`](../roadmap.md)
