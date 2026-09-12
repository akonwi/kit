# 0011: Use SSE for session event delivery

## Status

Accepted

## Context

Kit clients issue discrete session operations and consume an ordered stream of
renderer-neutral runtime events. The initial native client obtains those events
by repeatedly requesting bounded pages over HTTP. Polling duplicates timing and
retry policy in every client, creates requests while a run is idle, and delays
delivery by the polling interval.

The event path is unidirectional: clients do not need to send frames on the live
connection. Prompt submission, abort, configuration, interaction responses,
attachments, and other mutations are naturally correlated HTTP requests with
normal status codes and bounded response bodies. A bidirectional WebSocket
would combine these distinct concerns and introduces an upgrade protocol that
is more likely to be restricted or handled specially by proxies, tunnels, and
network policy.

Kit already gives live session events a runtime-local stream identity and an
ordered sequence. It retains a bounded contiguous suffix and requires snapshot
resynchronization when the stream changes or a cursor falls outside that suffix.
Those semantics do not require a bidirectional transport.

## Decision

Kit uses HTTP Server-Sent Events (SSE) as the push transport for live semantic
session events. All client-to-server operations continue to use ordinary HTTP
requests and responses.

Each attached session uses one session-bound SSE response. The server publishes
bounded `SessionEventBatch` values as `session.events` SSE records and flushes
each record as it becomes available. It sends periodic comment heartbeats so intermediaries can
keep an otherwise idle response open. The stream uses `Cache-Control: no-cache`
and disables intermediary buffering where supported.

The SSE connection is a projection of the authoritative runtime event stream,
not an additional durable queue. A slow or disconnected client resumes with the
runtime stream identity and its last applied sequence. The server first emits
the retained contiguous suffix. If the stream identity changed, the cursor is
invalid, or retained history was lost, it emits a `session.resync` record carrying a
resynchronization-required batch and closes the response. The client obtains an authoritative snapshot and
opens a new stream from the snapshot's stream identity and replay cursor.

SSE record IDs encode both stream identity and the last event sequence carried
by that record. Native clients may supply the same values explicitly as query
parameters. Browser clients may additionally use `Last-Event-ID` for automatic
reconnection, but correctness does not depend on browser-managed retry timing.
Clients own reconnect backoff and snapshot fallback.

The server's in-memory event log wakes all waiting response handlers when events
arrive. It does not allocate an unbounded queue per connection. Each handler
reads bounded pages from the shared retained log, so a client that cannot keep
up eventually receives the normal resynchronization signal.

Authentication credentials are never placed in an SSE URL. Native clients send
the daemon bearer header. A same-origin browser deployment must use credentials
that the browser can attach to `EventSource`, such as an authenticated cookie or
HTTP authentication, and must retain origin and CSRF protections for mutation
requests.

WebSocket is not part of the session protocol. A future feature that genuinely
requires bidirectional framed communication must justify that transport
separately rather than routing ordinary commands through the event stream.

## Consequences

### Positive

- Events are delivered immediately without repeated client polling.
- Commands retain ordinary HTTP cancellation, status, validation, and
  observability semantics.
- The event connection works through conventional HTTP infrastructure without a
  WebSocket upgrade.
- Stream replay, gap detection, and snapshot fallback remain transport-neutral.
- One runtime append wakes every attached client, making broadcasting explicit.

### Trade-offs

- SSE is UTF-8 text framing, so protocol batches are JSON rather than binary.
- Long-lived HTTP responses still require heartbeat, timeout, buffering, and
  backpressure care in servers and intermediaries.
- Browser `EventSource` cannot set an arbitrary bearer header.
- HTTP/1 connection limits make one stream per attached session preferable to
  several independent live endpoints; HTTP/2 avoids most of that pressure.
- Command responses and resulting events travel on separate HTTP exchanges, so
  protocol identities must continue to correlate intent with authoritative
  state.

## Related

- [0001: Native Go architecture](./0001-native-go-architecture.md)
- [0006: Make droids authoritative for session conversation data](./0006-droids-as-session-data-authority.md)
- [`../roadmap.md`](../roadmap.md)
