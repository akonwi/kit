# 0013: Native macOS client

## Status

Accepted

## Context

Kit's architecture ([ADR 0001](./0001-native-go-architecture.md)) makes the Go
server authoritative for sessions and defines clients as viewports that speak
the canonical server and session protocol over authenticated HTTP with SSE event
delivery ([ADR 0011](./0011-use-sse-for-session-events.md)). The native TUI and
the semantic browser client are the first two viewports. The same protocol is
intended to serve local daemons discovered through the run directory and
explicitly exposed remote servers started with `kit serve`.

A desktop application is a third viewport. Candidate approaches were evaluated:
an Electron or webview shell around the browser client, a Rust/GPUI application
forked from an existing agent controller, React-over-GPUI bindings, and a
native Swift application. Webview shells inherit browser rendering and
integration limits and were rejected as not native. GPUI options require a Rust
workspace, private renderer forks, and inheriting a large foreign UI whose state
model is not Kit's protocol. React-over-GPUI bindings are single-platform and
maintained by one person.

Linux desktop coverage is not required; the browser client serves Linux users.

## Decision

Kit provides a fully native macOS application written in Swift using SwiftUI
and AppKit. It contains no embedded web content and no agent logic. It is a
protocol client only.

### Viewport over any Kit server

The application is a server client as defined in ADR 0001. It connects to a
Kit server endpoint and then opens one session client per attached session.
The local daemon is the default endpoint, not a special code path:

```text
Kit.app ── server client ──▶ local daemon   (discovered or started)
        ── server client ──▶ kit serve host (explicit remote endpoint)
```

- The application discovers or starts the local daemon using the same run
  directory metadata and startup lock that the TUI uses. It ships the `kit`
  executable inside its bundle so it can start the daemon in the internal
  daemon role without a separate installation.
- The application accepts explicit remote endpoints and stores them as named
  servers. Remote authentication uses the token contract defined for CLI
  clients; the application never places credentials in URLs and stores tokens
  in the macOS Keychain.
- Kit does not terminate TLS. Remote endpoints are reached through user-managed
  HTTPS reverse proxies or private tunnels, and the application treats plain
  HTTP to non-loopback hosts as an explicit insecure choice.
- The application may hold connections to several servers and several sessions
  concurrently. Each session view is bound to exactly one session client for
  its lifetime; switching sessions replaces or adds a view.
- Server capability discovery drives feature availability. Workspace operations
  execute on the server host, never on the client machine. Local-only
  affordances such as opening a path in Finder are offered only when the server
  is the local daemon.

### Protocol boundary

Swift wire types are generated from the canonical protocol definitions, not
hand-maintained. The application validates every inbound payload at the
boundary and rejects unknown canonical wire values rather than guessing.
Snapshot, cursor, bounded replay, and resynchronization semantics follow
ADR 0011 exactly; the client owns reconnect backoff and snapshot fallback.

### Native presentation

- Transcript: a virtualized, streaming transcript with stick-to-bottom
  behavior, rendering the same renderer-neutral events as the TUI and browser
  client. Markdown renders through native attributed text; code blocks are
  syntax highlighted.
- Composer: native text editing with slash commands, references, attachments,
  queued follow-ups, model and thinking selection, and abort.
- Shell: native session tabs or windows, session discovery through pickers, native
  menus, keyboard shortcuts, notifications for interactions that need the
  user, and standard macOS accessibility.
- Mica's semantic surfaces and colors provide the application identity, with
  softened geometry and native macOS window chrome, controls, and text editing.
  The [macOS design reference](../design/macos-design-language.md) defines the
  presentation direction alongside `.agents/skills/design/SKILL.md`.
- Session navigation uses native macOS window tabs. Retained workspace tabs
  and optional two-group splits remain distinct from window-level session navigation.
- Appearance selects independently configured light and dark themes; System
  follows macOS. Theme imports use Kit's `tokens` and `syntaxPalette` structure,
  with explicit native projections and readable matching window chrome.

### Repository placement

The application lives under `apps/macos/` as an Xcode-buildable Swift package.
It has its own build, test, signing, notarization, and update pipeline and
does not affect the Go build. Bun, Node, and Xcode remain development-time
dependencies only.

### Non-goals

- Linux or Windows desktop builds.
- Embedding a browser, web view, or the Solid client.
- Running droids, tools, plugins, or SQLite inside the application process.
- A separate desktop protocol; the application uses only the shared contracts.

## Sequencing

1. The app consumes the canonical server and session contracts, with generated
   Swift wire types checked against the Go definitions.
2. A first milestone connects to a running local daemon, lists sessions, and renders a
   live transcript read-only over SSE.
3. Prompting, interactions, attachments, and session management follow.
4. Named remote servers and Keychain-backed tokens complete the viewport.

## Consequences

### Positive

- Kit gains a first-class macOS experience without adding a second runtime,
  renderer fork, or Rust workspace.
- Protocol conformance improves because a third independent client exercises
  the same contracts as the TUI and browser client.
- Remote attachment is native from the start rather than retrofitted.

### Negative

- Transcript, composer, and markdown rendering are implemented a third time.
- Adds Xcode, Swift, signing, and notarization to the release surface.
- Feature parity across three clients must be tracked explicitly.

### References

- `zeronsh/comet` `apps/ios`: virtualized SwiftUI transcript and composer
  design reference.
- `cristicretu/diri` `ios` and `diri/PACKAGING.md`, `UPDATING.md`: HTTP/SSE
  client pattern, notarization, and self-update playbook.
- `anomalyco/opencode` `packages/app`: UX reference shared with the browser
  client.
