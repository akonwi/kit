# 0038: Expose a typed Go client API

## Status

Accepted

## Context

Kit's server is authoritative for sessions, and every maintained client reaches
that authority through the versioned session protocol. Go callers currently
reach it through packages under `internal/`: `internal/server` implements the
HTTP transport, `internal/client` composes bound-session behavior, and
`internal/sessionclient` defines renderer-neutral interfaces. Those packages
let Kit's own clients remain separated from server internals, but they do not
form an API that another Go program can import.

A caller should not need to know session routes, headers, JSON encoding, SSE
framing, or which transport carries an operation. It should invoke typed
operations using the same validated request and response records that define
the session contract. The same client implementation should carry requests for
Kit's TUI so that the public path is the production path rather than a parallel
wrapper used only by external consumers.

The published OpenAPI document remains the HTTP contract. It describes wire
operations and enables non-Go clients, but it does not provide Go callers with
an ergonomic, transport-independent way to invoke those operations. The Go
client is a typed façade over that contract, not a second semantic model of the
server.

## Decision

Kit publishes an importable Go client at `github.com/akonwi/kit/api`. The
package name is `kit`, allowing callers to write:

```go
import kit "github.com/akonwi/kit/api"

client, err := kit.Connect(ctx, kit.Local())
if err != nil {
    return err
}

sessions, err := client.ListSessions(ctx, kit.ListSessionsOptions{})
if err != nil {
    return err
}

session, err := client.Attach(ctx, sessions[0].ID)
if err != nil {
    return err
}

submission, err := session.SendMessage(ctx, kit.Message{Text: "Explain this repository"})
```

`api/` owns both the language-neutral OpenAPI artifact and the public Go client
surface. A public `api/contract` subpackage owns the validated Go request and
response records from which OpenAPI is emitted. The parent `api` package
re-exports the records needed in client signatures as type aliases, so ordinary
callers need only import `github.com/akonwi/kit/api`. It does not expose routes,
headers, codecs, or the internal operation catalog.

### Client ownership and lifetime

- `Client` is an explicit value. Kit does not provide package-global connection,
  authentication, current-directory, or active-session state.
- A client is safe for concurrent use. Its close operation is explicit and
  idempotent.
- A bound `Session` has one immutable session identity. Operations cannot
  accidentally target a different session through an input field.
- Bound sessions own their synchronized client-side snapshot, mutation
  serialization, reconciliation, replay cursor, and retry state. These are
  client guarantees rather than renderer responsibilities.
- Blocking operations take `context.Context`. Canceling a wait or event read
  detaches that caller; it does not abort server work unless the caller invokes
  an explicit abort operation.
- Request types use option structs so adding an optional input does not require
  changing positional method signatures.

Package-level constructors and value helpers are allowed, but server operations
are methods on a `Client`, `Session`, or operation handle. Functions such as
`kit.ListSessions` that rely on hidden process-global client state are not
provided.

### Connection targets and transports

`Connect` accepts an explicit target. The initial targets are local daemon
discovery and an explicitly configured server endpoint. Target construction
contains transport-specific configuration; operations performed after
connection do not.

The package hides:

- daemon registry discovery and authentication material;
- protocol and instance headers;
- route, method, query, and body construction;
- JSON encoding and strict decoding;
- HTTP status interpretation;
- SSE framing, record bounds, payload decoding, cursor recovery, reconnects,
  and resynchronization.

The client implementation depends on an internal transport boundary. HTTP is
the first implementation, not part of the semantic method contract. A future
first-party transport can implement that boundary without changing callers.
Kit does not initially publish a transport-provider interface: doing so would
freeze a lower-level service-provider contract before a second implementation
establishes its requirements.

Connecting is separate from managing a server process. The public client does
not silently install, start, replace, stop, or migrate a daemon. CLI bootstrap
may perform those lifecycle operations before constructing the client.

### Contract model

The public `api/contract` package owns the validated Go records that define
requests, responses, errors, and stream payloads in the OpenAPI contract. There
is one Go type for a contract concept, not a public semantic copy projected
from an internal wire type. Existing contract records move out of
`internal/protocol`; `api` aliases rather than duplicates the records used by
its client surface. Keeping contract ownership in a dependency package below
the client and operation catalog avoids an import cycle between them.

For example:

- `Message{Text, AttachmentIDs, AnnotationIDs}` is the request record encoded
  for message admission; there is not a second `PromptInput` with the same
  fields behind the client;
- `SessionInfo` is the response record containing identity, working directory,
  model configuration, revisions, and timestamps; the client validates and
  returns it directly;
- `SessionSnapshot` and its nested transcript, usage, follow-up, interaction,
  and annotation records are decoded directly into their contract types;
- `SessionEvent` remains a closed discriminated union. The client hides SSE
  framing but returns the validated event variants defined by the contract;
- page records expose the contract's typed cursors while attachment methods
  expose streaming content without exposing multipart construction.

Generated OpenAPI types are not introduced into Go as a second set of records.
The Go contract definitions remain the source from which OpenAPI is emitted.
Renderer and runtime models remain separate and project from these contract
records where their meanings differ.

Names favor user operations such as `SendMessage` while preserving distinctions
that exist in the contract, including queued follow-ups, turn identity,
explicit abort, and replay resynchronization. Convenience methods may cover
common text messages, but they delegate to the complete typed operation rather
than define a second behavior.

### Errors

Transport failures and server-declared failures remain distinguishable.
Server-declared failures are exposed as typed errors with stable codes and,
where declared, typed details. Callers can use `errors.Is` and `errors.As` for
recovery decisions and never need to inspect error strings or HTTP statuses.

Protocol incompatibility, authentication failure, missing resources, stale
revisions, capacity limits, cancellation, and stream protocol violations retain
distinct identities. Error messages are suitable for display but are not API
identifiers. Undeclared statuses, malformed records, unknown union variants,
and invalid payloads are protocol errors rather than partially decoded
successes.

### Layering and adoption

The target dependency direction is:

```text
TUI / print / RPC / external Go callers
                  │
              api.Client
                  │
 bound session state and recovery behavior
                  │
      validated public contract records
                  │
       internal transport implementation
                  │
        HTTP + OpenAPI wire contract
                  │
              Kit daemon

CLI bootstrap ── manages local daemon lifecycle, then constructs api.Client
```

The public client absorbs both roles currently divided between
`internal/server.Client` and `internal/client`. It owns typed operation
construction as well as snapshot caching, mutation serialization, optimistic
reconciliation, turn waiting, event replay, resynchronization, and retry
policy. These behaviors are part of interacting correctly with a Kit server and
must not be reimplemented by each caller.

The TUI, print mode, and RPC bridge migrate to `api.Client` and its bound
sessions. A consumer may retain a narrow, consumer-owned interface for test
doubles, but the production implementation is the public client and the
interface uses public contract types. `internal/client` and
`internal/sessionclient` are retired once their callers migrate; they do not
remain as a second client stack or model.

### Verification and compatibility

Kit does not maintain a separate client conformance suite or run the public
client against a production Kit server in integration tests. Shared operation
definitions, validated contract types, server conformance tests, and the
generated-document staleness check verify the common contract boundary.

Client-owned state and recovery behavior is covered by focused unit tests
against a fake typed transport. Transport mechanics not guaranteed by the
shared contract infrastructure, such as SSE framing and cancellation, receive
targeted adapter tests. These tests verify client behavior rather than duplicate
server functionality or exhaustively replay every request and response shape.

The public Go API follows Kit's module release compatibility policy. A session
protocol version change does not by itself require a source-incompatible Go API
change, and an additive Go convenience API does not require a protocol change.
Source-breaking public API changes require an explicit compatibility decision.

## Consequences

### Positive

- Go programs receive one discoverable, typed API instead of assembling wire
  operations or importing Kit internals.
- Callers are insulated from HTTP, OpenAPI generation details, and stream
  framing.
- Kit's maintained Go callers and external programs use the same stateful
  client implementation.
- Contract records retain one Go definition and one validation implementation.
- Focused unit tests cover client-owned state and recovery behavior without
  duplicating server contract or integration coverage.

### Negative

- Contract records become a supported public Go surface in addition to their
  wire-compatibility role.
- Kit assumes a Go source-compatibility obligation in addition to its session
  protocol compatibility obligation.
- Moving contract ownership from `internal/protocol` to `api/contract` affects
  imports across the server and existing clients even when record shapes do not
  change.
- The public package assumes responsibility for caching, retries, and recovery,
  making those behavioral guarantees part of its compatibility surface.
- Migrating and retiring both existing internal client layers is a substantial
  change even where their behavior is preserved.
- The `api` directory contains both the OpenAPI artifact and a Go package, so
  file organization and generated-artifact ownership must remain clear.
