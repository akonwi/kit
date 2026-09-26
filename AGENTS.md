# AGENTS.md

## Project identity

Kit is a standalone, single-user, terminal-first coding agent distributed as one
Go executable for macOS and Linux. Windows is intentionally unsupported. The
current rewrite architecture is canonical in
[`docs/adrs/0001-native-go-architecture.md`](docs/adrs/0001-native-go-architecture.md),
and release scope is tracked in [`backlog/README.md`](backlog/README.md).

Kit uses `~/.kit` by default. Set `KIT_HOME` to an explicit isolated directory
for development and tests when production state must not be read or mutated.

When working from a development worktree, do not launch a development daemon,
replace or restart the existing daemon, or open/migrate the production `~/.kit`
database without explicit user authorization. A normal `kit` invocation may
start or replace a daemon. Follow `.agents/skills/worktree-development/SKILL.md`
before running a development build against a Kit home; keep ordinary access to
existing sessions on their current server when compatible.

## Design language

Kit's UI design language is documented in `.agents/skills/design/SKILL.md`. All
TUI and web UI work must preserve its surface hierarchy, palette, layout,
interaction, and component conventions unless a new decision explicitly changes
it.

The native TUI uses `go.rockorager.dev/vaxis/ui`. The semantic browser client
uses Solid and Mica at build time and is embedded in the Go executable.

## Architecture rules

- The Go server is authoritative for sessions and shared state.
- Clients consume server/session-client contracts; they do not import
  `internal/droids`, SQLite implementations, or concrete server internals.
- `internal/droids` is Kit's private agent core, seeded from the standalone
  repository at the commit recorded in its README. There is no implicit
  upstream sync; reconcile changes deliberately.
- `internal/auth` owns machine-managed provider credentials. Keep auth files
  private, locked, atomically replaced, and generation-checked.
- Runtime, persistence, protocol, client, and renderer types have distinct
  owners and are projected explicitly.
- Do not introduce process-global cwd or active-session state.
- Multiple sessions may run concurrently; serialize mutation within a session.
- Subagents are supervised concurrent executions with durable state and mailbox
  delivery.
- Built-ins are compiled Go packages. Custom plugins are child processes using
  the public RPC protocol.
- Preserve canonical wire values and validate both sides of every process or
  network boundary.
- Create packages to enforce meaningful ownership/dependency boundaries, not
  merely to hold shared types.
- Once an interface is accepted, implement through it rather than bypassing it
  for expediency.
- Record architectural decisions under `docs/adrs/`. Record all outstanding and
  deferred work in `backlog/`; do not maintain work lists under `docs/`.
- Write ADRs as descriptions of the proposed or accepted target state and its
  rationale. Do not narrate previous implementations or historical state in an
  ADR; put necessary historical comparisons in migration documents, the backlog,
  or commit history instead.

## Go conventions

- Follow standard Go style and keep exported identifiers documented.
- Thread `context.Context` through blocking work.
- Make goroutine ownership, cancellation, and channel closure explicit.
- Prefer bounded queues and concurrency over unbounded goroutine creation.
- Keep SQLite transactions short and preserve foreign-key enforcement.
- Keep the release build CGO-free unless an ADR explicitly changes that goal.

## Testing conventions

- UI presentation tests must assert the visible content, style, geometry, focus,
  or interaction that is expected. Prefer exact expected rows, cells, snapshots,
  and state transitions so each test documents the intended experience.
- Do not define presentation behavior by asserting that unwanted text, glyphs,
  or styles are absent. Use negative assertions only when a test explicitly
  captures a discovered regression.

## Commit conventions

Use Conventional Commits, preferably `type(scope): summary`. Common types are
`feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `build`, and `ci`.

## Validation

For Go changes, run:

```sh
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

`gofmt -l .` must print nothing. Also run `go test -race ./...` for changes to
daemon, session, subagent, plugin, or other concurrency-sensitive code when the
platform supports it.

For browser-client changes, run its formatting, lint, typecheck, unit, and
browser suites defined by the web workspace. Bun is a development/build-time
dependency only and must not become a user runtime requirement.

Before releasing the rewrite, satisfy the R1 verification gates in
`backlog/README.md` and `backlog/core.md`.
