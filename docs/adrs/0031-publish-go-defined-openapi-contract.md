# 0031: Publish the session HTTP API as a Go-defined OpenAPI contract

## Status

Proposed

## Context

Kit's session server is consumed by the native TUI, the native macOS client,
and a future semantic browser client. Each client must agree with the server on
routes, methods, path and query parameters, request and response bodies,
status codes, typed error bodies, and SSE payloads. The canonical record types
live in `internal/protocol` and carry `Validate` methods that enforce semantic
invariants at every process or network boundary.

A contract that is expressed only as Go types leaves each non-Go client to
reconstruct operations, parameters, and error shapes by hand, and leaves the Go
client and server to agree through convention rather than a shared definition.
A language-neutral description of the contract lets clients generate their
transport layer and lets tooling detect breaking changes mechanically.

## Decision

Kit publishes its session HTTP API as an OpenAPI 3.1 document. The document is
**derived from Go definitions**; Go remains the single source of truth for the
contract.

### Ownership

- `internal/protocol` owns wire record types, enum values, and semantic
  validation. It does not depend on OpenAPI tooling. Each enum type declares
  its permitted values beside its constants with a plain Go method; validation
  and the emitter both use that list, and a test verifies it against the
  declared constants.
- A contract package (`internal/httpapi`) owns the operation catalog: for each
  operation, its identifier, method, path, path/query/header parameters,
  request body type and media type, success status and response type, declared
  error responses, and whether the response is an SSE stream with its event
  names and payload types.
- The server registers every HTTP handler through the operation catalog. An
  operation without a handler, or a handler without an operation, is a startup
  error in tests. Mux patterns are derived from catalog entries rather than
  written separately.
- The Go session client calls operations through typed catalog values and
  shared request/response helpers. Operation-specific cross-checks that relate a
  response to its request remain hand-written beside the call.
- No HTTP framework owns request decoding, validation, or error rendering.
  Strict JSON decoding, request bounds, and `Validate` calls remain explicit in
  the server and client.

### The published document

- An emitter in `internal/httpapi/openapi`, used only by generation and tests
  and never imported by the server or clients at runtime, reflects protocol
  types into JSON Schema 2020-12 component schemas and combines them with the
  operation catalog into one OpenAPI 3.1 document committed at
  `api/kit-session.openapi.json`.
  Reflection uses `invopop/jsonschema`; Kit-owned post-processing adds nullable
  collections, integer formats and bounds, enum values, union variants, and
  component references. The document avoids schema constructs that the
  supported client generators cannot represent, such as `not`.
- `info.version` equals `version.SessionProtocolVersion`.
- A Go test fails when the committed document differs from freshly emitted
  output. Regeneration is an explicit developer action.
- The document describes the wire exactly as the server produces it for the
  current protocol. Where the current protocol permits `null` collections,
  both error-body shapes, or omitted zero values, the schema says so. Tightening
  those shapes is a protocol change decided separately, not a side effect of
  schema emission.
- Objects are closed (`additionalProperties: false`), matching strict decoding
  on both sides of the boundary.
- Security requirements describe the daemon bearer token; required instance and
  protocol headers are described as parameters on every operation.
- SSE operations declare `text/event-stream` responses and document each named
  event and its JSON payload schema. SSE framing, reconnection, and cursor
  semantics remain as defined in ADR 0011.

### Generated clients

- The macOS client generates types and its operation client with Apple's
  `swift-openapi-generator`, run ahead of time with committed output and a
  staleness check. A Kit-owned transport preserves the client's loopback-only,
  no-redirect, bearer-token, and instance-header policy.
- The browser client generates TypeScript types and a typed fetch client from
  the same document at build time.
- Generated code is a transport and type layer only. Client-side semantic
  validation that the schema cannot express remains hand-written in each client.

### Verification and evolution

- Server tests run with a test-only middleware that validates every request and
  response exercised by the suite against the committed document.
- CI compares the document with the one published by the most recent release.
  A breaking difference requires a `SessionProtocolVersion` greater than that
  release's.
- The plugin JSON-RPC protocol and `--rpc` stdio mode are separate contracts and
  are not described by this document.

## Consequences

### Positive

- Every client derives routes, parameters, bodies, and errors from one
  language-neutral artifact.
- The Go compiler binds server handlers and the Go client to the same operation
  definitions, so route and type drift in Go is a build error.
- Breaking changes are detected mechanically and tied to protocol versioning.
- Contract changes are reviewable as diffs to one committed document.

### Negative

- The emitter and operation catalog are Kit-owned code that must be maintained.
- JSON Schema cannot express every invariant `Validate` enforces; semantic
  validation continues to exist in each client.
- The macOS client takes runtime dependencies on the OpenAPI runtime and
  URLSession transport packages.
