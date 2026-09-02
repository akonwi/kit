# Kit

Kit is a fast, native coding-agent CLI with a terminal UI, semantic web client,
remote sessions, concurrent subagents, and language-neutral process plugins.

The `kit-v2` branch is a ground-up Go rewrite for macOS and Linux. Windows is
not a supported target. Its architecture is recorded in
[`docs/adrs/0001-native-go-architecture.md`](docs/adrs/0001-native-go-architecture.md),
and replacement parity is tracked in [`docs/parity.md`](docs/parity.md).

## Architecture

- one Go executable for the CLI, daemon/server, agent orchestration, and TUI
- [`github.com/akonwi/droids`](https://github.com/akonwi/droids) for the agent core
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
```

A normal `go run ./cmd/kit` invocation currently starts or discovers the daemon.
The native TUI is the next vertical slice.

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
