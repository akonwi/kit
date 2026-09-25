# 0027: Treat print as an ordinary session client

## Status

Accepted. Supersedes the explicitly noninteractive execution policy in
[ADR 0026](0026-scope-plugin-processes-and-route-plugin-ui.md).

## Context

Kit sessions and their plugin processes are owned by the server. The TUI,
macOS app, and print client can operate on the same session. A client's ability
to present a dialog is not a property of that session or its plugin runtime.

Treating print as a separate plugin execution mode would make it an exceptional
client. It would require separate session classes, session-wide policy changes,
or propagation of client-specific execution policy through shared plugin work.
Those distinctions conflict with a uniform server-owned session model.

## Decision

Print is an ordinary session client. It submits work and consumes results through
the same session contracts as other clients. Kit does not introduce a special
noninteractive plugin policy for print, persist a headless session mode, or
change plugin interactivity according to which client starts a turn.

Plugin discovery, background initialization, process ownership, and contribution
availability follow normal session-runtime rules. Print may reuse a session that
is also open in another client without changing that client's plugin behavior.

Plugin confirm, input, and select requests use the shared session interaction
broker regardless of the initiating client. A capable client attached to the
session may answer them; the first valid response wins. Kit does not reject,
auto-answer, or fabricate cancellation for a dialog merely because print is
running or no client is currently presenting it.

Requests remain subject to their normal ownership, cancellation, capacity, and
runtime lifecycle rules. Actual absence of a host interaction adapter may still
produce an interactivity-unavailable error; print mode itself is not evidence
that the adapter is unavailable.

Print need not implement an interactive dialog interface. Its output contract
remains unchanged: diagnostics belong on stderr, not in result stdout. Plugin
protocol stdout remains reserved for JSON-RPC.

## Consequences

- Print does not require a separate class of sessions or mutable runtime mode.
- Starting print work cannot disable plugin dialogs for an attached TUI or app.
- An operation waiting for user input may remain pending until a capable client
  answers or normal cancellation/lifecycle handling ends the wait. Print is not
  a guarantee that plugin work will complete unattended.
- Client detachment and lack of a dialog renderer do not revoke shared requests.
- A dedicated noninteractive plugin execution policy is not planned work.
