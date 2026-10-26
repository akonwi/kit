# 0049: Let plugins ask attached clients to open web pages

## Status

Accepted. Supersedes the "Plugin footer actions and URL opening" section of
[ADR 0026](0026-scope-plugin-processes-and-route-plugin-ui.md) for
`kit/system/open-url`. Footer click callbacks and declarative URL actions
remain outside the native scope.

## Context

The model can already ask the user's Kit app to open a web page with the
`open_url` tool. The server validates the address and records it in the tool
result; the macOS app opens it only for a call it saw complete live, at most
once, in its browser beside the conversation.

Plugins need the same capability without a model tool call: a command that
opens a pull request, an event handler that opens a preview server once it is
ready, a tool that shows documentation. Plugins run on the server, which may
not be the machine the user sits at, so the plugin cannot open the page
itself.

A plugin instance belongs to exactly one session (ADR 0026). Several clients,
or none, may be attached to that session, and they attach, detach, and
reconnect independently of the plugin.

## Decision

### Plugin request

A plugin asks with the plugin-to-Kit request `kit/system/open-url`. Params are
an object with one required `url` string. Unknown parameters are rejected, and
encoded params are bounded to 8 KiB. There is no session parameter: the target
is always the session that owns the calling instance. This holds for user and
project plugins alike; a user plugin's instance in one session cannot reach
another session.

Kit validates the address with the same rules as the `open_url` tool: an
absolute `http` or `https` URL with a host and no credentials, without
whitespace or control characters, at most 2 KiB before and after
canonicalization. An invalid address fails with `-32602` and a message naming
the rule it broke. Kit publishes the canonical form.

The request is fire-and-forget. Kit returns `null` once it has validated and
published the request. It does not wait for, or report on, any client: the
result does not mean that a page opened, and it is the same when no client is
attached or no attached client opens pages.

Only the current ready generation of an instance may ask. A revoked, stopped,
failed, or superseded generation fails with `-32002` and publishes nothing.

The session's plugins together may make five requests in any ten seconds.
Further requests fail with `-32006`, `data.reason` `rate_limited`, and publish
nothing; Kit does not queue or retry them.

There is no user confirmation before a page opens and no label naming the
requesting plugin. Opening a project with plugin manifests already authorizes
its plugins as trusted local code (ADR 0026), and the address is limited to web
pages, which open in the app's browser rather than the system's handlers.

### Session event

A request is published as the session-scoped live event `url.open.requested`,
which carries no turn. It is ephemeral: it lives only in the runtime's live
event log, is never part of a snapshot or the transcript, and does not survive
the runtime, a reload that rotates the stream, or a daemon restart.

The event carries `url` only on an event stream that was already open when the
session accepted the request. Event pages, replays, and the catch-up an event
stream sends when it opens (everything up to the last sequence the runtime had
published at that moment) carry the event without `url`. The server, which
alone knows when each stream opened, enforces that a request reaches a client
live or not at all. Each stream receives each event once, so a client acting on
it opens each page at most once per stream; loading a transcript, attaching
after the request, or reconnecting never opens a page.

Adding the event changes the closed session event union, so it is a wire break
and takes session protocol 49.

### Clients

Clients that do not open pages ignore the event. The TUI ignores it. The
public Go client delivers it in session attachments (`Session.Watch`) and, as
for other events without a turn, not in turn-oriented streams.

The macOS app re-validates the address on its side of the boundary with the
rules the server used and ignores any address it does not accept. It opens a
page only for a request it received live, while the session is shown, at most
once per request. The page opens with the placement `open_url` uses: as a tab
in the tile holding the most recently opened browser tab, unless that tile also
holds the conversation, and otherwise in a new tile beside the conversation.

## Consequences

- Plugins can open pages for the user from any code path without a model turn.
- A request made while no client is attached, or that arrives while a client is
  reconnecting, is lost; plugins that need the user to see a page must not rely
  on it opening.
- A plugin can open pages without asking, within the rate bound. The bound
  limits flooding the user or the session's live event log, not intent.
- Each macOS window showing the session is its own client and opens the page.
