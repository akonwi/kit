# 0051: Let attached clients host session plugins

## Status

Proposed. Extends the plugin model of
[ADR 0026](0026-scope-plugin-processes-and-route-plugin-ui.md) with a second
kind of plugin instance and the session event stream of
[ADR 0036](0036-publish-turns-events-and-transcript-contract.md). Builds on the
browser placement of
[ADR 0049](0049-let-plugins-ask-attached-clients-to-open-web-pages.md).

## Context

Some capabilities exist only where the user is, not where the agent runs. The
macOS app has an in-app browser whose tabs hold the user's pages, sign-ins, and
access to the Mac's own network. The agent should be able to read and act on
those tabs, as a browser-use extension does in a desktop browser, so it can
fill in a form on a site the user is signed into, test a sign-in flow end to
end, check a development server on the Mac after a change, or pull data out of
a page the user has open.

The agent loop runs on the server, which may be on another machine
(ADR 0045). The server decides which tools the model sees and executes each
call, so it must offer a client's tool to the model and carry each call to that
client and its result back. A server can reach a client only over a connection
the client opened, and every attached client already holds one: its session
event stream.

Plugins already contribute tools to one session. A session's plugin host owns
their declarations, validates each call against the declared schema, routes it
to the instance that registered the tool, fences it by generation so that a
revoked or superseded instance cannot answer, and updates the model's tool
catalog between requests without reloading. A tool provided by a client needs
all of this; only where the instance runs and how calls reach it differ.

## Decision

### Client-hosted plugins

A plugin instance is either a *process instance*, a child process started from
a manifest (ADR 0026), or a *client instance*, hosted by a client attached to
the session. Both are plugins of that session: they share one plugin ID
namespace, the model-facing tool names `<plugin ID>__<tool ID>`, the tool
declaration rules, schema validation, generation fencing, and the session's
tool catalog.

A client instance provides tools and nothing else. It does not use the plugin
JSON-RPC protocol and has none of its other capabilities (commands, footer
items, interactions, notifications, messages, URL opening, event
subscriptions). Each further capability for client instances is a separate
decision with its own typed operations.

A client instance has a plugin ID matching the plugin ID rules
(`^[a-z][a-z0-9-]{0,31}$`), such as `browser`, and a client-generated instance
identifier (`clientplugin_` followed by 32 lowercase hex digits). The
identifier names one attachment of one client view of a session, such as a
macOS session tab. A client reuses it when it
reconnects and generates a new one when it attaches afresh. Each client
instance belongs to exactly one session; a client attaches one for each session
it shows.

### Attaching

`attachClientPlugin`
(`PUT /v1/sessions/{sessionID}/client-plugins/{instanceID}`) declares or
replaces an instance's plugin ID and its complete tool list. Each tool uses the
`kit/tools/register` fields and limits: an `id`, an optional `label`, a
required `description`, an `inputSchema` in the plugin tool schema subset, an
`executionMode`, and optional `promptSnippet` and `promptGuidelines`. A client
instance declares at most 32 tools, and the encoded declaration is bounded to
256 KiB. Unknown fields are rejected, and so is a plugin ID reserved for Kit
command domains.

`detachClientPlugin`
(`DELETE /v1/sessions/{sessionID}/client-plugins/{instanceID}`) removes it. A
session holds at most eight client instances.

Client instances are not persisted. They do not survive the session runtime, a
reload that rotates the event stream, or a daemon restart. A client attaches
again whenever it opens a stream.

### Lifecycle

A client opens its session event stream with `clientPlugin={instanceID}`.
Opening a stream for an unknown instance fails with `not_found`, and a second
stream for the same instance replaces the first.

A client instance is *ready* while a stream bound to it is open. When its
stream closes, it stays ready for a 10-second grace period so that reconnecting
does not churn the model's tools. After the grace period, or on detach, it
stops.

For each plugin ID, the ready client instance whose stream opened most recently
is the current generation. A newer one supersedes the previous one; when the
current one stops, the most recent remaining ready instance with that ID becomes
current. A superseded instance's tools leave the catalog, but calls already
sent to it may still complete there; a stopped instance's pending calls end as
`unavailable`.

A process plugin takes precedence over a client instance with the same plugin
ID. The client instance's tools are withheld while the conflict lasts, and the
session reports a plugin diagnostic.

Client instance tools reach the session's primary agent and its subagents.
They are the one kind of plugin tool subagents receive, because they reach the
user's environment rather than extend the server's. Because they enter each
agent's catalog through the plugin host, changes take
effect at the next model request; a model request keeps the catalog it was sent
with.

### Calls and results

The plugin host executes a client instance's tool as it does any plugin tool:
it checks the generation, validates the input against the declared schema, and
bounds it to 64 KiB. It then publishes the session event
`client_plugin.call.requested`, carrying the calling agent (the turn, or the
subagent conversation), the model's tool call ID, a server-generated call ID (`clientcall_` followed by 32 hex digits), the
instance ID, the tool ID, and the input.

The event is ephemeral, like `url.open.requested`: it lives only in the live
event log, never in a snapshot or the transcript. It carries its payload only
on the stream bound to the target instance, and only while the call is pending.
Every other stream, event page, and replay carries the event without its
payload.

The live event log rotates when a turn starts and whenever clients must
resynchronize, and a client may be reconnecting when a call is published. So
whenever a stream binds to an instance, the session publishes the instance's
pending calls again, and the new stream receives each one. A client therefore
sees a call at least once and runs each call ID at most once.

A change to client instance tools updates the model's catalog but does not make
clients resynchronize: snapshots do not list tools.

The client answers with `completeClientPluginCall`
(`POST /v1/sessions/{sessionID}/client-plugins/{instanceID}/calls/{callID}/result`).
A result is either the plugin tool result shape, `content` with `text` and
`image` parts bounded to 4 MiB of decoded content in total plus optional
`details`, or an `error` message. Client instances cannot end the turn the way
a plugin result's `terminate` does. Answering does not need an open stream.

A call ends exactly once, and `client_plugin.call.resolved` reports its
outcome: `completed`, `failed`, `timed_out`, `withdrawn` (the calling turn or
subagent work was aborted), or `unavailable` (the instance stopped). A call
times out after two minutes. A client instance has at most 16 pending calls.
A result for an ended, unknown, or foreign call fails with `conflict` and
settles nothing. The model sees a failed tool result for every outcome except
`completed`.

The model's view of the call is the ordinary tool call in the transcript.
`client_plugin.call.*` events are coordination only.

Images in a plugin tool's result, from a client or a process instance, are
kept as session attachments, as `inspect_image`'s are, and the result's image
blocks in the transcript keep their attachment ID so every client can show the
image with the call. The model still receives the image inline, followed by a
line naming its attachment so it can copy the image to a file with
`save_attachment` ([ADR 0055](0055-let-the-model-save-session-attachments-to-files.md)).
An image the attachment store refuses stays inline for the model alone, and
the line says it was not kept.

### Trust

A process plugin is trusted local code on the server (ADR 0026). A client
instance's declarations come from the user's own client over the session
connection's authorization (ADR 0045) and are as trusted as the user's input.
The server validates their shape and text, not their intent.

A client instance's results are tool output from an environment the user, not
the server, controls. The server does not interpret them. A client instance
that returns third-party content, such as a web page, must mark it as untrusted
for the model.

### Protocol

The new operations, the stream's `clientPlugin` parameter, and the two event
kinds change the closed session event union and the session API. They take
session protocol 49. A client hosts plugins only on servers that speak
protocol 49 or later. The public Go client exposes attaching, detaching,
receiving calls, and completing them. The TUI hosts no plugins and ignores the
events. Plugin result images keeping their attachment ID use the existing
optional `attachmentId` of transcript content and are compatible.

### The macOS browser plugin

Each macOS session tab showing a session on a protocol 49 or later server hosts
a `browser` client instance over the browser tabs in its workspace. It is
always hosted; there is no setting. A session tab's workspace holds one
session, so the agent sees exactly the tabs beside its conversation. Tabs waiting for an authorization
redirect are excluded.

The app re-validates every call on its side: the input matches the schema it
declared, and the tab exists in the workspace. It offers these tools:

- `browser__tabs` lists the workspace's tabs: tab ID, title, address, whether the
  user or the agent opened it, and whether it is visible.
- `browser__snapshot` returns one tab's page as a role/name outline whose
  elements carry stable `ref`s. It has `interactive` and `full` modes and is
  bounded to 40,000 characters.
- `browser__act` performs `click`, `fill`, `select`, `check`, `uncheck`,
  `focus`, or `scroll` on a `ref`; `type` (text as key presses) or `press`
  (one key, with modifiers) into a `ref` or the focused element; `hover` or
  `click` on a `ref` or a screenshot point; `drag` between refs or points;
  `scroll` of the page; `accept` or `dismiss` on the tab's open page dialog,
  with text for a `prompt`; or `choose_files` for an open file chooser. It
  then waits for the page to settle and returns a fresh outline that includes
  text, so page changes are visible.
- `browser__screenshot` returns an image of a tab's visible viewport, at one
  pixel per point of the page's view, scaled down to fit 1600 pixels. Its
  pixels are the coordinates `browser__act` takes, so the agent can work
  canvases and other content without refs. The user sees it with the call.
- `browser__navigate` loads an address in a tab, goes back or forward, or
  reloads it. Without a tab it opens one, with the placement `open_url` uses.
  Addresses follow the `open_url` rules.

Page scripts run in an isolated content world that the page cannot see, so a
page cannot read, forge, or remove refs. Page content, including titles, is
wrapped in an `UNTRUSTED_PAGE_CONTENT` fence with a fresh nonce on every
result; only the tab ID and origin sit outside the fence.

Field values are reported and can be filled on every tab, including password,
one-time-code, and payment-card fields, so the agent can run sign-in flows end
to end. The outline marks those fields `secret` so the model knows what they
hold.

Actions on one tab run one at a time, in the order the app receives them, so
the primary agent and subagents never interleave actions on a page. Actions on
different tabs may run concurrently.

The agent acts without asking. Acting on a tab brings it to the front of its
tile, so a person looking at the session sees the work; keyboard focus stays
where it is. A page that is in no window, as when its session tab isn't shown,
is laid out in an off-screen window for the operation. Every action on a
visible tab shows the agent pointer above the web view, where the page can
neither hide nor imitate it. The pointer glides to the element, plays a press,
and fades two seconds after the last action.

Clicks, checks, fills, selects, and scrolls on a ref are DOM operations from
the isolated world. Typing, key presses, hovering, dragging, and clicks at a
point are native input delivered to that tab's web view alone, which the page
receives as a person's. They are not computer use: they never move the
system pointer, reach another app or tab, or need macOS permissions, and
typing takes keyboard focus only for the keystroke. The agent's input gives a
page no activation of its own: a download or link to another app that it
starts is blocked as one a page starts by itself. A drag carries the pointer
but not files or dragged page elements, and the page sees no held buttons
while it moves.

A page dialog (`alert`, `confirm`, `prompt`) or file chooser waits for an
answer; the page waits with it. While the page is shown, the dialog is a sheet
on its window; while it isn't, the tab carries a mark until the dialog is
answered, and the sheet appears once the page is shown. An action that opens
one returns at once and reports the dialog's kind and fenced message, and the
tab's outline and listing report it while it is open. The agent or the user
may answer it; the first answer wins.

`choose_files` gives a file chooser readable files from the session's working
directory on the Mac, after symbolic links resolve, and no more than the
chooser allows. Files the agent can already read are the only ones it can
upload; a page cannot steer it to the rest of the Mac.

## Consequences

- The agent can act on the user's own tabs, with their sign-ins and the Mac's
  network, even when the server is remote.
- There is one plugin model. Client-hosted tools get the same naming,
  validation, fencing, and catalog behavior as process plugin tools, and the
  server holds no browser-specific code.
- Adding or changing a browser action changes the app, not the server.
- The tools exist only while a capable client shows the session. A session with
  no such client, such as one driven from the TUI or by a plugin with nobody
  watching, has no browser tools, and the catalog changes when such a client
  arrives or leaves, which costs a prompt-cache miss.
- With several session tabs or Macs showing one session, the most recently
  attached is the current generation, and calls never move to another client.
  Only the browser tabs in the current one's workspace are visible to the agent;
  when another takes over, tab IDs from the previous one fail as unknown and the
  agent lists tabs again.
- A process plugin can claim a plugin ID and hide a client's tools; the
  diagnostic names the conflict.
- Calls do not survive a daemon restart or a runtime reload; the model sees them
  fail.
- Acting on user tabs uses the user's cookies without per-action approval. The
  visible pointer and the transcript are the record of what the agent did.
- Secrets the agent reads or enters, such as test-account passwords, are in
  tool calls and results. They are stored in the transcript and sent to the
  model provider like any other tool content.
- Subagents can drive the browser alongside the primary agent; per-tab ordering
  keeps their actions from interleaving, but not from working against each
  other on one page.
