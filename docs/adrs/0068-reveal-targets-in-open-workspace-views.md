# 0068: Reveal targets in open workspace views

## Status

Accepted. Extends [ADR 0067](0067-place-workspace-views-in-rex-panes.md), and
follows the stream rules of
[ADR 0035](0035-describe-server-push-streams-in-the-session-contract.md).

## Context

A terminal client running in Rex opens workspace views in panes, each one a
single-view client (`kit --view`) attached to the same session. When an
opening targets a view whose pane is still open, at another line or
annotation, the view has to show the new target. Restarting it throws away the
view's own state: a Diff view's chosen target, its collapsed files and
selection, and a Markdown file's preview or source mode.

The opening client and the view are separate processes, possibly on a machine
other than the server. Each already holds an authenticated attachment to the
session, so the server can relay a message between them.

Session protocol 50 is released, and session routes require an exact protocol
match. The session event union is closed for a protocol version, so a new
event kind would be a breaking change for every client. A new operation that
only the terminal client uses isn't.

## Decision

### View identifiers

The client that opens a view in a pane gives it a view identifier,
`view_` and 32 lowercase hex digits from a random source, on its command line
(`--view-id`). It keeps one identifier per pane for its lifetime, including
across a pane's restart. The identifier only addresses the view: it is not a
credential, and every route that takes one authenticates the caller like any
other session route and applies only to the session in its path.

### Operations

Two additive operations relay a reveal from the opening client to the view:

- `POST /v1/sessions/{sessionID}/views/{viewID}/reveal` (`revealView`) takes
  exactly one of a positive `line` or an `annotation`, and answers
  `{ "delivered": bool }`: whether any view of the session was watching that
  identifier. `delivered` doesn't mean the view showed the target.
- `GET /v1/sessions/{sessionID}/views/{viewID}/events` (`streamViewReveals`)
  is a server-sent event stream with one record, `view.reveal`, carrying the
  session ID, the view identifier, and the target. It sends nothing on
  connect, never resumes or replays, and heartbeats like other streams.

The server validates the identifier and the target on both routes, and the
Go client validates them before sending.

### Relay

Each session runtime keeps the watchers of its view identifiers in memory.
Nothing is stored: a reveal reaches only the streams open when it is sent.
Each watcher holds at most one unread reveal, and a newer reveal replaces it,
since a view shows one target at a time. A session has at most 64 open view
streams; another one fails with `capacity_exceeded`. A stream ends, and its
watcher is dropped, when its client disconnects or when the runtime closes:
the session is deleted, unloaded, or the server shuts down.

### Clients

When an opening targets a view whose pane is open, the terminal client posts a
reveal, bounded to five seconds, and then focuses the pane. If the reveal
isn't delivered, it replaces the pane in place, as ADR 0067 describes. That
happens when no view is watching, the request fails, or the server predates
the operations. On a server without the operations, which answers with an
unrouted 404, the Go client reports `UnsupportedError`. A missing session
stays a declared `not_found` error.

A single view started with `--view-id` watches its identifier for as long as
it is attached. It shows a line, or an annotation it has, as a new opening of
its own tab, as the annotation's chip would, and keeps the rest of the tab's
state.

The macOS and browser clients don't open views in panes and don't use these
operations. The generated Swift and TypeScript types describe them.

## Consequences

- An open view keeps its state when it is asked to show another target.
- The session protocol stays at 50. Released clients are unaffected. A newer
  terminal client attached to an older server restarts panes as before.
- A reveal sent while a view is reconnecting reaches no watcher, so the pane
  restarts. A reveal queued for a stream that drops before sending it is
  reported as delivered but lost; the view keeps its old target until the
  next opening.
- The server carries client-to-client navigation it doesn't otherwise need.
  The relay is in memory, bounded, and live-only, so it has no persistence or
  replay cost.
