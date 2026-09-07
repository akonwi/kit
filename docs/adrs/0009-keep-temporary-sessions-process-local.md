# 0009: Keep temporary sessions process-local and defer owner leases

## Status

Accepted

## Context

A temporary session is owned by one foreground Kit command and is expected to
leave no durable session or conversation artifacts after orderly exit. The
persistent local daemon still owns its runtime, so temporary cleanup must work
across the client/server boundary and must account for interrupted creation,
active agent work, and direct shell execution.

Persisting temporary metadata in the SQLite session registry and deleting it
when no clients are connected appears to offer automatic cleanup. However,
Kit's session protocol does not have a durable transport connection that
represents attachment lifetime. An attached client issues separate HTTP
requests, event polls may have gaps, and multiple clients may eventually attach
to one authoritative session. A TCP connection count would therefore report
transport activity rather than session ownership.

The general session contract also treats client disconnect as detach rather
than abort. Temporary sessions need an explicit owner-bound lifetime without
changing that behavior for persisted sessions.

## Decision

Temporary session metadata and conversation state remain process-local to the
authoritative daemon:

- metadata is held by the session supervisor and is not inserted into the
  SQLite session registry;
- the droid uses an in-memory Store rather than a per-session SQLite Store;
- normal session listing and implicit resume cannot discover temporary
  sessions; and
- daemon termination removes all remaining temporary state by construction.

The foreground owner selects the temporary session ID before creation and
installs cleanup against that identity before issuing the create request. On
orderly return, including handled cancellation, it invokes the dedicated
temporary-session disposal operation.

Disposal is distinct from persisted-session deletion. It atomically prevents
new admission for the temporary identity, coordinates with creation or runtime
loading already in progress, cancels active parent and direct-shell work, waits
for that work to settle, closes the droid Store, and removes the process-local
metadata. Repeated and concurrent disposal requests are safe. Persisted session
IDs are rejected by the disposal operation, and temporary IDs are rejected by
persisted deletion.

Transport connection closure does not dispose a session. The initial protocol
does not count TCP connections, infer attachment from request activity, or add
client leases. If an owner disappears without executing cleanup, its temporary
state may remain in daemon memory until daemon shutdown, but it does not leave a
registry row or droid database.

### Rejected connection-counted registry cleanup

Kit does not insert a non-persistent registry row and delete it when the number
of open transports reaches zero. That count is neither a stable attachment
identity nor a reliable failure detector, and persisting the row creates the
very durable metadata artifact that temporary mode avoids. It would still need
a lease or heartbeat to distinguish an idle client from a failed owner, so the
registry row does not solve owner-loss cleanup by itself.

### Deferred owner leases

If abnormal owner-loss cleanup becomes necessary, Kit should add logical owner
leases rather than connection counting. A lease design must define:

- an opaque lease identity created atomically with the temporary session;
- explicit release on orderly client exit;
- bounded expiration and renewal for failed or partitioned clients;
- whether and how additional clients acquire independent leases;
- atomic coordination between final lease loss, new lease acquisition, prompt
  admission, and disposal;
- whether active work extends a lease or is canceled when ownership expires;
  and
- daemon-restart behavior, which invalidates leases and process-local session
  state.

Lease expiration should invoke the same authoritative disposal path as explicit
owner cleanup. Persisting lease metadata or temporary registry rows requires a
separate justification; leases do not require conversation data to become
durable.

## Required properties

The implementation must demonstrate:

- temporary creation writes neither a Kit session-registry row nor a droid
  database;
- temporary sessions are absent from saved-session listing and implicit resume;
- a client-selected temporary identity can be disposed after an ambiguous create
  response;
- disposal coordinates with concurrent creation and runtime loading;
- disposal cancels and settles active parent and direct-shell work before
  releasing runtime state;
- disposal is retry-safe and cannot dispose a persisted session;
- persisted deletion cannot archive a temporary session; and
- daemon shutdown waits for in-progress temporary cleanup and removes all
  remaining process-local state.

## Consequences

### Positive

- Temporary conversations and metadata are never intentionally written to disk.
- SQLite WAL, freelist, backup, and crash-recovery behavior cannot retain
  temporary session content or registry metadata.
- Session ownership remains a protocol concern rather than being inferred from
  transport implementation details.
- Persisted-session detach and recovery semantics remain unchanged.
- A future lease reaper can reuse the existing disposal operation.

### Trade-offs

- Repeated clients killed without cleanup can accumulate temporary runtime state
  in the daemon until it exits.
- The daemon must coordinate creation, loading, active work, and disposal in
  memory rather than relying on a registry row for identity reservation.
- Multi-client temporary-session ownership remains undefined until leases are
  designed.
- Adding lease expiration later will require protocol, supervision, and timing
  policy beyond the current foreground-command lifecycle.

## Related

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [0006: Make droids authoritative for session conversation data](./0006-droids-as-session-data-authority.md)
- [0008: Establish the initial native CLI surface](./0008-establish-initial-native-cli-surface.md)
- [`../parity.md`](../parity.md)
