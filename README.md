# Kit

Kit is a native coding agent for macOS and Linux with a terminal UI, headless
commands, durable sessions, concurrent subagents, and language-neutral process
plugins. Windows is not supported.

Kit is implemented in Go. Its architecture is recorded in
[`docs/adrs/0001-native-go-architecture.md`](docs/adrs/0001-native-go-architecture.md),
and release scope is tracked in [`backlog/README.md`](backlog/README.md).

## Architecture

- one Go executable for the CLI, daemon/server, agent orchestration, and TUI
- a Kit-private [`internal/droids`](./internal/droids) agent core, seeded from
  [`github.com/akonwi/droids`](https://github.com/akonwi/droids)
- `vaxis/ui` for the native terminal client
- SQLite for authoritative session/runtime state
- JSON-RPC child processes for custom plugins written in any language

Kit stores data under `~/.kit` by default. Set `KIT_HOME` to an explicit
isolated location when developing or testing.

## CLI

```sh
# Start the native TUI, resuming this directory's latest usable session:
kit

# Choose an exact session, force a new saved session, or work temporarily:
kit --session <long-or-short-id>
kit new --name "Focused work"
kit --temp
kit sessions

# Run one headless turn. Piped stdin is prepended to the prompt:
kit print "Continue the latest session for this directory"
kit print --model openai/gpt-4o-mini "Say hello"
cat changes.diff | kit print --temp "Review this diff"

# Inspect the complete command tree and operate the local daemon:
kit --help
kit server status
kit server restart

# Persist OpenAI Codex OAuth credentials with a headless device flow:
kit auth login openai-codex
kit auth status
kit auth logout openai-codex
```

A normal `kit` invocation starts the viewport-native vaxis TUI. It
starts or discovers the daemon, offers OpenAI, Anthropic, and OpenCode Go API-key
login plus OpenAI Codex and Claude Pro/Max subscription login when credentials are
missing, resumes the latest usable session
for the selected working directory, and restores its persisted transcript snapshot. `kit sessions` opens a bounded
primary-screen session manager before transitioning to the normal TUI for an
opened session. `--temp` uses an in-memory session that is disposed when its
foreground command exits. Claude subscription login uses a localhost callback;
if that callback cannot complete, the login dialog accepts the final redirect
URL or authorization code.

Print mode creates and resumes droids sessions through the local session-client
boundary, writes only final assistant prose to stdout, and keeps diagnostics on
stderr. Codex can derive account and expiry metadata from
its access token; `OPENAI_CODEX_ACCOUNT_ID`, `OPENAI_CODEX_ID_TOKEN`,
`OPENAI_CODEX_FEDRAMP`, and Unix-millisecond `OPENAI_CODEX_EXPIRES_AT` are
available when explicit metadata is needed. `ANTHROPIC_OAUTH_TOKEN` supplies a
managed Claude subscription access token. `OPENCODE_API_KEY` enables the
`opencode-go/*` model catalog through `https://opencode.ai/zen/go/v1`; models are
routed to Responses, Chat Completions, or Anthropic Messages according to their
models.dev metadata. Without explicit provider environment credentials, the
daemon uses the locked, atomic `~/.kit/auth.json` store and
persists refresh-token rotations. Provider environment is read only at daemon
startup, takes precedence over the file store, and refreshes only in memory, so
restart the daemon after changing it. Stored login/logout generations are
observed without restarting the daemon.

## Development

Kit requires Go 1.26 or newer. The Go CLI and daemon do not require a JavaScript
runtime.

```sh
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

The macOS client lives in [`apps/macos/`](apps/macos/). TypeScript application
code in `apps/web/` and shared packages in `packages/` use Bun for development.
The web client is not yet integrated with the Go daemon.
