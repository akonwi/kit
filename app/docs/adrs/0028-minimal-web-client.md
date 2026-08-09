# 0028: Minimal web client architecture

## Status

Proposed

## Context

ADR 0027 establishes `kit --mode web` as a remote session server and chooses an
HTML-first browser client built on Mica. The server now exposes the protocol
surface needed for an initial client: snapshots, ordered event replay, live
runtime events, correlated commands, attachments, and remote interactions.
The first browser client now exercises that surface and has exposed constraints
that are easy to miss when considering only the server transport.

The client needs enough structure to remain reliable as transcript, tool,
interaction, and multi-client behavior grows. The initial framework-free view
demonstrated that direct DOM callbacks require substantial manual identity
caches, reconciliation, focus preservation, and render scheduling. Those
mechanics obscured the otherwise clean protocol reducer and would become more
expensive as session, model, and plugin surfaces are added.

The client is also a privileged remote-control surface. Session content, tool
output, plugin-provided labels, and model text are untrusted display data. Its
asset and rendering architecture must not depend on third-party origins or
interpret remote content as markup.

## Decision

Kit will ship a small, same-origin browser application with `kit --mode web`.
It will use semantic HTML, Mica, app-owned CSS, and SolidJS for the browser view
layer. It will not use React, a virtual DOM, or a separate browser application
server. Transport, protocol reduction, commands, and uploads remain plain
TypeScript and do not depend on Solid.

The browser client is layered behind a stable `WebClientController` facade:

- `rpc-transport.ts` owns WebSocket lifecycle, reconnect cursors, ordered event
  draining, and command correlation without importing protocol state.
- `remote-services.ts` owns capability parsing, attachment HTTP operations,
  pagination, and bounded reference recovery. It returns validated values and
  never mutates client state.
- `view-state.ts` owns local observable UI state and immutable snapshots.
- `controller.ts` coordinates those layers, applies the pure protocol reducer,
  and performs domain-specific stale-result checks before committing async work.

Solid remains confined to the context/provider and view components.

### Asset delivery

Mica will be a pinned package dependency used at build time. Kit will bundle
Mica's CSS and only the opt-in enhancement modules it uses into first-party web
assets. Solid JSX will compile to a browser-targeted bundle during Kit's build;
the resulting JavaScript will be embedded in the compiled binary. Source-mode
development and tests may build that same entry point lazily. Browser assets
will be served by the same `kit --mode web` process.

The production client will not load scripts, styles, fonts, or libraries from a
CDN. The document will avoid inline script and style requirements so the server
can apply a restrictive same-origin Content Security Policy. Development may
use source maps and unminified assets, but it will exercise the same asset and
protocol boundaries as production.

### Client layers

The client will keep four explicit layers:

1. A WebSocket transport owns connection lifecycle, command correlation, and
   reconnect attempts.
2. A protocol reducer applies snapshots and ordered events to an in-memory
   client state model.
3. Command and upload services translate user intent into RPC commands and HTTP
   attachment requests.
4. Solid view modules render semantic HTML from client state and bind native
   browser interactions back to those services. One provider bridges immutable
   reducer snapshots into a Solid signal; components do not mutate protocol
   state directly.

Protocol reduction will not depend on DOM APIs. Transport code will not mutate
rendered elements directly. The reducer consumes transport-safe projections of
Kit's semantic `AgentRuntime` events; it does not model raw Pi lifecycle events
or provider payloads. This keeps replay and snapshot behavior testable
without a browser and leaves the state model reusable by a future remote TUI or
another browser renderer.

The server remains authoritative. The client may track local pending-command
and upload state, but it will not speculatively add shared transcript, session,
model, or interaction state before the corresponding response or event arrives.

### Synchronization and recovery

The client will model connection state explicitly as disconnected, connecting,
synchronizing, or live. It will not enable mutating controls until initial
synchronization completes.

A snapshot replaces authoritative client state. Replay applies only to the
existing in-memory state from the same stream. The client advances its resume
cursor only after applying each sequenced event and treats duplicate events as
idempotent. A sequence gap, `resync_required`, changed stream, or reducer failure
causes a reconnect without a cursor so the server returns a fresh snapshot.

Resume state will remain in memory for ordinary network interruption. A page
reload or newly opened tab will not send a cursor because it does not possess
the reducer state that precedes that cursor; it will request a snapshot instead.
Persisting a resumable reducer state may be considered separately later.

Connection-scoped command responses remain outside the sequenced reducer and
resolve a pending-command table by command id. Disconnecting rejects those
local pending commands even though accepted server work may continue and later
appear through replay or snapshot recovery.

### Information architecture

The initial document will follow Kit's existing shell hierarchy while adapting
to browser semantics:

- a compact header for session, workspace, model, and connection state
- a primary transcript region for user and assistant messages
- inline tool and run activity associated with the current turn
- a fixed composer with submit, attachment, queue, and abort affordances
- native dialogs for remote confirmation, input, selection, and guided
  questions
- a restrained status/toast region for command and connection errors

The first milestone is a functional protocol client, not a complete visual port
of OpenTUI. Advanced session exploration, settings, plugin chrome, rich code
review, and other renderer-owned surfaces remain follow-up work unless required
to complete a core remote interaction.

### Mica and Kit styling

Mica will provide layout primitives, native-element styling, and accessible
interaction patterns. The initial client will use Mica's default tokens without
a Kit-specific theme override. App-owned CSS will be limited to layout and
content behavior that Mica does not provide. A distinct web theme can be added
later if product needs justify diverging from Mica's defaults.

Native elements remain the source of interaction semantics: forms for composer
submission, buttons for actions, `<dialog>` for modal interactions,
`<details>` for disclosure, and live regions only for concise status changes.
Streaming token updates will not be announced individually. Optional Mica
JavaScript enhancements will be imported only when the accessible behavior
cannot be provided natively.

The layout will support narrow mobile viewports from the first milestone.
Secondary information will collapse or move below the primary transcript rather
than requiring a desktop-width shell.

### Safe rendering

Remote and persisted content will be inserted as text, not HTML. Rendering code
will use DOM text nodes or `textContent` for model output, tool output, paths,
plugin labels, and errors. The initial client will prefer plain text and code
presentation over unsanitized rich rendering. Any future Markdown or rich HTML
support requires an explicit sanitization boundary.

View updates will be targeted and identity-aware so streaming updates replace
the affected message or activity record without rebuilding the whole document.
The reducer, not DOM shape, owns message and turn identity. Stable Kit
`messageId` and `turnId` values key live events, snapshots, references, and
paginated transcript results.

### Protocol and rendering scheduling

WebSocket delivery order is not sufficient once the browser batches work.
Sequenced protocol records must be reduced before a later unsequenced command
response is allowed to resolve client work that depends on those records.
Protocol reduction must also use a bounded, promptly drained queue; it must not
wait for `requestAnimationFrame`, which browsers throttle or pause in background
tabs. Animation frames are reserved for DOM painting.

### Mutable collection recovery

A mutable collection cannot safely use the number of currently rendered items
as its pagination cursor. A `ui_request` or `ui_resolved` event can change the
pending-interaction collection while pages are in flight. The interaction
broker owns a monotonically increasing generation exposed in snapshots, pages,
and mutation events. Clients page from zero against one generation, reject stale
pages, and reconcile requests by opaque ID.

### Active message recovery

A bounded snapshot may represent the currently streaming assistant message with
a `message_reference`. The reference carries the same `messageId` and `turnId`
as the full message. The reducer retains that identified message as active and
buffers continuation deltas until its immutable snapshot bytes are hydrated. If the
reference token has expired, a fresh `get_messages` result is a rebased current
message; buffered deltas must not be applied to it again.

Session changes from another client and `session.transcript.replaced` events
from compaction or retry recovery invalidate transcript projection state. A
client requests a fresh snapshot rather than trying to merge a replacement into
its existing transcript.

### Capability-driven limits

Capability discovery advertises attachment counts, sizes, aggregate budgets,
upload concurrency, pagination sizes, snapshot bounds, event retention, and
chunk-recovery limits. These are protocol behavior, not presentation choices.
The browser uses the advertised attachment, page, and interaction-recovery
limits with conservative defaults only until capability discovery completes.

### Verification boundary

The protocol reducer will have deterministic tests for snapshot replacement,
ordered replay, duplicate and gap handling, streaming message updates, tool
activity, pending interactions, and reconnect fallback. Browser tests will
cover the served asset boundary, keyboard and form behavior, narrow layouts,
native dialog focus behavior, and a complete prompt-to-stream-to-reconnect
flow. Accessibility checks will be part of browser verification rather than
being deferred to visual polish.

## Consequences

### Positive

- The browser client stays aligned with Mica's progressive, native-first model
  while Solid handles keyed streaming updates and component lifecycle.
- Protocol correctness is isolated from DOM rendering and can be tested
  deterministically.
- Same-origin assets and text-only remote-content rendering provide a strong
  default security boundary.
- Snapshot and replay behavior are explicit rather than emerging from view
  callbacks.
- The client state model can inform a future remote TUI without making browser
  DOM assumptions part of the protocol.
- Using Mica's default tokens avoids a premature web-theme maintenance surface.

### Trade-offs

- Kit must maintain a small browser build and static-asset lifecycle alongside
  its compiled terminal binary.
- Solid adds a browser runtime and JSX compilation step even though transport
  and reducer logic remain framework-independent.
- Snapshot recovery on reload favors correctness over minimizing initial data
  transfer.
- The minimal client will initially have less presentation richness and fewer
  management surfaces than the OpenTUI application.
- Mica remains the styling and native-component convention layer; Solid owns
  composition, reactivity, and DOM lifecycle rather than introducing a second
  visual design system.

## Related

- `docs/adrs/0003-custom-shell.md`
- `docs/adrs/0023-keymap-driven-keybindings.md`
- `docs/adrs/0026-headless-rpc-mode.md`
- `docs/adrs/0027-remote-session-server.md`
- `docs/features/rpc-mode.md`
- `backlog/remote-session-server.md`
- [Mica vision](https://github.com/akonwi/mica/blob/main/VISION.md)
- [Mica progressive component model](https://github.com/akonwi/mica/blob/main/PROGRESSIVE.md)
