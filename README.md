# Kit

Kit is a fast, native coding-agent CLI with a terminal UI, semantic web client,
remote sessions, concurrent subagents, and language-neutral process plugins.

The `kit-v2` branch is a ground-up Go rewrite for macOS and Linux. Windows is
not a supported target. Its architecture is recorded in
[`docs/adrs/0001-native-go-architecture.md`](docs/adrs/0001-native-go-architecture.md),
and replacement parity is tracked in [`docs/parity.md`](docs/parity.md).

## Architecture

- one Go executable for the CLI, daemon/server, agent orchestration, and TUI
- a Kit-private [`internal/droids`](./internal/droids) agent core, seeded from
  [`github.com/akonwi/droids`](https://github.com/akonwi/droids)
- `vaxis/ui` for the native terminal client
- Solid and Mica for build-time browser assets embedded in the executable
- SQLite for authoritative session/runtime state
- JSON-RPC child processes for custom plugins written in any language

During rewrite development, Kit stores data under `~/.kit-v2`. Set `KIT_HOME`
to use another isolated location.

## CLI

```sh
# Start the native TUI, resuming this directory's latest usable session:
go run ./cmd/kit

# Choose an exact session, force a new saved session, or work temporarily:
go run ./cmd/kit --session <long-or-short-id>
go run ./cmd/kit new --name "Focused work"
go run ./cmd/kit --temp
go run ./cmd/kit sessions

# Run one headless turn. Piped stdin is prepended to the prompt:
go run ./cmd/kit print "Continue the latest session for this directory"
go run ./cmd/kit print --model openai/gpt-4o-mini "Say hello"
cat changes.diff | go run ./cmd/kit print --temp "Review this diff"

# Inspect the complete command tree and operate the local daemon:
go run ./cmd/kit --help
go run ./cmd/kit daemon status
go run ./cmd/kit daemon restart

# Persist OpenAI Codex OAuth credentials with a headless device flow:
go run ./cmd/kit auth login openai-codex
go run ./cmd/kit auth status
go run ./cmd/kit auth logout openai-codex
```

A normal `go run ./cmd/kit` invocation starts the viewport-native vaxis TUI. It
starts or discovers the daemon, offers provider login when credentials are
missing, resumes the latest usable session for the selected working directory,
and restores its persisted transcript snapshot. `kit sessions` opens a bounded
primary-screen session manager before transitioning to the normal TUI for an
opened session. `--temp` uses an in-memory session that is disposed when its
foreground command exits.

Print mode creates and resumes droids sessions through the local session-client
boundary, writes only final assistant prose to stdout, and keeps diagnostics on
stderr. Codex can derive account and expiry metadata from
its access token; `OPENAI_CODEX_ACCOUNT_ID`, `OPENAI_CODEX_ID_TOKEN`,
`OPENAI_CODEX_FEDRAMP`, and Unix-millisecond `OPENAI_CODEX_EXPIRES_AT` are
available when explicit metadata is needed. Without explicit Codex environment
credentials, the daemon uses the locked, atomic `~/.kit-v2/auth.json` store and
persists refresh-token rotations. Provider environment is read only at daemon
startup, takes precedence over the file store, and refreshes only in memory, so
restart the daemon after changing it. Stored login/logout generations are
observed without restarting the daemon.

## Development

Kit requires Go 1.26 or newer. Browser development will additionally require
Bun, but released users will not need a JavaScript runtime.

```sh
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Historical TypeScript application code remains temporarily in `app/` and
`packages/` as a parity reference while native replacements are built.
