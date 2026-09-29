# 0035: Describe server-push streams in the session contract

## Status

Proposed

## Context

ADR 0011 made Server-Sent Events (SSE) the push transport for live session
events. Other server-push responses, such as repository status and plugin
notifications, were built separately and use newline-delimited JSON with their
own framing, heartbeats, and bounds. Every client therefore implements two
stream readers.

ADR 0031 publishes the session API as an OpenAPI contract. OpenAPI 3.1 has no
first-class model for event streams, so the contract needs a Kit convention
that states, for each stream operation, which records it emits, their payload
schemas, and how failures are reported. Generated clients must be able to
decode stream payloads with the same schemas they use for ordinary responses.

## Decision

### One stream format

Every server-push response in the session API is an SSE stream as defined by
ADR 0011. Newline-delimited JSON is not used.

- The response has `Content-Type: text/event-stream; charset=utf-8`,
  `Cache-Control: no-cache`, and `X-Accel-Buffering: no`.
- The server sends an initial comment when the stream opens and a comment
  heartbeat at least every 15 seconds while no record is due. Comments carry
  no meaning and clients ignore them.
- Each record has an `event` field naming the record and a single `data` line
  holding one JSON object. Records never span multiple `data` lines.
- Each stream declares a maximum encoded record size. The server ends the
  stream rather than send a larger record, and clients reject larger records.
- A stream sets the `id` field only when it supports resumption. Its cursor
  semantics are defined by the stream, as ADR 0011 does for session events.
  Streams without resumption start fresh on every connection and do not replay.

### Records and payloads

- Record names follow the ADR 0033 grammar and are namespaced by the stream's
  subject, for example `vcs.status` or `session.events`. They are transport
  record names, not session event kinds.
- Every record of one stream carries the same payload schema. When a stream
  needs payloads of different shapes, its payload is a discriminated union per
  ADR 0032. The record name distinguishes the meaning of payloads that share a
  shape, such as `session.events` and `session.resync`.
- Payloads are ordinary protocol records. They are validated with the same
  `Validate` rules as the equivalent non-stream response.

### Failures

- Failures before the stream opens use the ADR 0034 error body with the
  statuses and codes the operation declares. Capacity limits use 429
  `capacity_exceeded`; an unavailable source uses 503 `unavailable` or a domain
  code.
- After the stream opens, the server reports no errors inside it. A stream
  ends when the client disconnects, the source closes or fails, or a record
  would violate its bounds. A stream may define a terminal record, such as
  `session.resync`, that tells the client how to recover before the server
  closes the response.
- Clients treat an unexpected end, malformed framing, an oversized record, a
  payload that fails decoding or validation, or an unknown record name as a
  transport failure. They reconnect according to their own backoff policy.

### Contract representation

- A stream operation declares a `200` response with `text/event-stream`
  content. The content schema is the stream's payload schema, published as a
  component so generated clients can decode `data` with it.
- The operation carries an `x-kit-stream` extension listing its record names,
  whether it supports resumption, and its maximum record size. The emitter
  generates it from the operation catalog.
- In Go, `internal/httpapi` owns a stream operation type, the server-side
  writer that applies framing, heartbeats, and bounds, and the client-side
  reader that parses records, enforces bounds, and decodes and validates
  payloads. Streams do not implement framing by hand.
- Generated clients receive the raw body and decode it with their runtime's
  SSE support using the published payload schema. Client transports bound each
  record and the time between records rather than the total response length,
  because a stream has no natural end.

## Consequences

### Positive

- Every client uses one stream reader for every push response.
- The contract states each stream's records, payload, bounds, and failure
  behavior, and generated clients decode payloads with generated types.
- Heartbeats, framing, and bounds are enforced in one Go implementation
  instead of per stream.

### Negative

- Converting existing newline-delimited JSON streams is a protocol change for
  each affected stream.
- Record names and resumption rules live in a vendor extension that generic
  OpenAPI tooling does not interpret.
- Client transports need a streaming path separate from the bounded,
  fully buffered path used for ordinary responses.
