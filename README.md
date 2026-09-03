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

## Bootstrap commands

```sh
go run ./cmd/kit daemon start
go run ./cmd/kit daemon status
go run ./cmd/kit daemon stop
go run ./cmd/kit version

# Persist OpenAI Codex OAuth credentials with a headless device flow:
go run ./cmd/kit login openai-codex
go run ./cmd/kit auth status
# Remove them later with: go run ./cmd/kit logout openai-codex

# The daemon captures API-key provider credentials when it starts:
OPENAI_API_KEY=... go run ./cmd/kit daemon restart
go run ./cmd/kit -p --model openai/gpt-4o-mini "Say hello"
go run ./cmd/kit -p "Continue the latest session for this directory"

# OpenAI Codex accepts OAuth credentials and refreshes them in memory:
OPENAI_CODEX_ACCESS_TOKEN=... OPENAI_CODEX_REFRESH_TOKEN=... \
  go run ./cmd/kit daemon restart
go run ./cmd/kit -p --model openai-codex/gpt-5.6-sol "Say hello"
```

A normal `go run ./cmd/kit` invocation currently starts or discovers the daemon.
Print mode now creates and resumes SQLite-backed droids sessions through the
local session-client boundary. Codex can derive account and expiry metadata from
its access token; `OPENAI_CODEX_ACCOUNT_ID`, `OPENAI_CODEX_ID_TOKEN`,
`OPENAI_CODEX_FEDRAMP`, and Unix-millisecond `OPENAI_CODEX_EXPIRES_AT` are
available when explicit metadata is needed. Without explicit Codex environment
credentials, the daemon uses the locked, atomic `~/.kit-v2/auth.json` store and
persists refresh-token rotations. Provider environment is read only at daemon
startup, takes precedence over the file store, and refreshes only in memory, so
restart the daemon after changing it. Stored login/logout generations are
observed without restarting the daemon. Streaming protocol projection and the
native TUI remain subsequent slices.

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
