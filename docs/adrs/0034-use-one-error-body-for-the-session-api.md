# 0034: Use one error body for the session API

## Status

Proposed

## Context

Session API clients need to distinguish failures they can recover from, such as
a stale revision, a busy session, or an incompatible daemon, from failures they
can only report. They also need a human-readable explanation to show the user.
Under ADR 0031 each operation declares its failures in the published contract,
and clients generate their decoders from it.

A contract can describe a failure precisely only if every failure has the same
shape: a machine-readable identity, a message for people, and optional
structured details whose shape is fixed by that identity. When some failures
carry only free text, clients must branch on HTTP status or parse messages, and
a generated client cannot decode every error response through one type.

## Decision

Every non-success response from the session API, including requests rejected
before routing, has `Content-Type: application/json` and this body:

```json
{
  "error": {
    "code": "scratchpad_revision_conflict",
    "message": "scratchpad revision conflict",
    "details": { "scratchpad": { "...": "..." } }
  }
}
```

### Fields

- `code` is required. It is a stable snake_case identifier and the only field
  clients branch on.
- `message` is required. It is non-empty, bounded, renderer-safe text for
  display. Clients do not parse it. Responses with status 500 use a fixed,
  generic message and never expose internal error text.
- `details` is present only for codes that define details. Within an
  operation, its schema is fixed by the code. A code without details never
  sends an empty `details` object.
- A code has one meaning across the protocol. Generic and domain codes share
  names where they mean the same thing (`not_found`, `limit_exceeded`,
  `capacity_exceeded`, `unavailable`).

### Codes and statuses

- Each operation declares, in the operation catalog, every status it can
  return and the codes permitted for each status. A code maps to exactly one
  status within an operation.
- Domain codes identify failures that clients handle specifically, such as
  scratchpad revision conflicts, workspace and diff errors, annotation evidence
  errors, and plugin command failures.
- Failures without a domain code use a generic code:

| Status | Generic code | Meaning |
|---|---|---|
| 400 | `invalid_request` | Malformed body, unknown fields, or invalid input |
| 401 | `unauthorized` | Missing or invalid daemon bearer token |
| 403 | `forbidden` | Rejected origin or disallowed access |
| 404 | `not_found` | The addressed resource does not exist |
| 409 | `conflict` | The request conflicts with current state |
| 409 | `instance_mismatch` | The request targets a different daemon instance |
| 413 | `limit_exceeded` | A request or result exceeds a bound |
| 421 | `invalid_host` | The request's `Host` is not the daemon's |
| 422 | `unprocessable` | Well-formed input that cannot be applied |
| 426 | `protocol_mismatch` | Client and daemon session protocols differ |
| 429 | `capacity_exceeded` | A bounded queue or subscriber limit is full |
| 500 | `internal` | An unexpected server failure |
| 503 | `unavailable` | A required service is closed or unavailable |

- A generic code is the floor, not the goal. When a client needs to tell apart
  two failures that share a generic code, for example a busy session and a
  configuration conflict that are both 409 `conflict`, the protocol adds domain
  codes rather than having clients inspect messages.
- Adding a code to an operation, or changing a code's status or details, is a
  protocol change.

### Representation

- In Go, `internal/httpapi` owns the error body type and the per-operation
  declarations. The server maps domain errors to a status, code, message, and
  details. `httpapi.Call` decodes only the codes the operation declares for the
  received status and returns a typed error that exposes the code and any typed
  details.
- In the published contract, each declared error status references the shared
  error body. Where any permitted code for that status defines details, `error`
  is a union discriminated by `code`. Otherwise `code` is an enum of the
  permitted codes.
- Stream operations use this body for failures before the stream starts.
  Failures after a stream starts are stream records and are out of scope here.
- Kit does not use RFC 9457 problem details. Its `type` URI and
  `title`/`detail` split add no value for a single-origin API, and the
  `error.code` identity is what generated clients and existing consumers
  branch on.

## Consequences

### Positive

- Generated clients decode every failure through one declared type and switch
  on codes rather than statuses or messages.
- The contract states exactly which failures each operation can produce.
- Middleware rejections, including protocol mismatches, become as diagnosable
  as handler failures.

### Negative

- Every error site in the server must produce a code, and failures that
  clients currently distinguish only by status need domain codes.
- Declaring statuses and codes per operation adds catalog entries that must be
  kept complete. The conformance middleware and contract diffs enforce this.
