# 0028: Hard-delete persisted sessions

## Status

Accepted

## Context

A persisted session's canonical conversation history lives in a separate SQLite
store, with additional session-owned records in the Kit registry and artifacts
in the attachment and subagent stores. Hiding a session from listings does not
reclaim this data and does not meet the user-facing promise of permanent
session deletion.

Deletion spans SQLite transactions and filesystem-owned stores. Session IDs
may also be supplied by clients for idempotent creation, so removing a registry
row alone could allow a later session to reopen leftover files under the same
identity.

## Decision

Deleting a persisted session permanently removes its registry row and
session-owned data, including the main droid database and SQLite sidecars,
subagent conversation databases, registry records with cascading ownership,
and the session's attachment ownership. Shared attachment bytes remain while
another session still owns them. Shared scratchpads remain available to
surviving semantic forks through the ownership transfer defined in ADR 0025.
Attachment cleanup fails closed when candidate manifests or ownership metadata
are corrupt. Since the filesystem ownership format does not provide a separate
per-session index, unrelated corrupt attachment metadata can block deletion
recovery until that metadata is repaired; this favors not falsely declaring
private data erased.

Deletion removes the session's owned records, not every historical mention of
its ID or content copied into another session's independent droid history.

Deletion is rejected while session work, child-agent work, peer-query work, or
session-scoped attachment operations are active. The daemon closes loaded droid
stores before removing their files. Session IDs are permanently reserved after
delete, preventing stale droid or attachment data from being exposed by
recreating the same ID.

The registry records pending external cleanup in the same transaction that
removes a session. Cleanup is idempotent; failed or interrupted cleanup keeps
the ID reserved and can be retried by repeating deletion for that ID. Daemon
startup does not run bulk deletion or orphan-store reconciliation. Sessions
archived by older versions remain archived until separately managed cleanup.

Temporary sessions remain process-local and continue to use disposal rather
than persisted-session deletion.
