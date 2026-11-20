# 0071: Let a session delete its child sessions with the user's approval

## Status

Accepted

## Context

A top-level session can create persistent sessions with `create_session`
([ADR 0017](0017-create-top-level-sessions-from-model-tools.md)), and the
person can fork it. Both kinds record it as their parent. A session that
coordinates work this way accumulates children it no longer needs, and only
the person can remove them, one at a time, from a client.

Deleting a session is permanent
([ADR 0028](0028-hard-delete-persisted-sessions.md)). A model must not be able
to delete anything without the person's consent, nor anything outside the
sessions it is responsible for.

## Decision

### The tools

Kit gives the model of every persistent session without a parent, the same
sessions that get `create_session`, a `delete_session` tool. It takes 1 to 20
session IDs and deletes those sessions and all of their descendants.

`peer_session discover` with `children: true` lists the caller's direct
children, most recently active first, each with the number of its
descendants, so the model can find what to delete.

### Ownership

The server decides what the call may delete from stored parent links at the
time it runs, never from the call's arguments:

- The caller must be a persistent session without a parent.
- Every named session must be a persistent session whose stored parent is the
  caller. A forked session counts like one `create_session` made.
- The descendants are every session below a named one through stored parent
  links.

The caller, its parent, a sibling, an unrelated session, and a temporary
session can never be deleted: none has the caller as its stored parent. A
subagent is not a session, and its conversation is deleted with the session
that owns it.

A named ID that is not one of the caller's children is reported as not
deleted, with the reason "this session has no child with this ID", and the
call goes on with the rest. The result is the same whether the ID names no
session or a session the caller doesn't own, so the tool doesn't reveal which
IDs exist. When none of the named IDs is a child, nothing is asked.

One call covers at most 50 sessions, descendants included, so the approval
can list them all.

### Busy sessions

A session with a running turn, queued prompts, a running shell command,
unfinished subagent work, an active peer query, or another operation in
progress cannot be deleted. If any session the call covers is busy, the
call deletes nothing and asks nothing; its result names each busy session and
why.

### Approval

Every call asks the person once, through the caller's confirm interaction,
before deleting anything. The approval lists every session the call would
delete, each named request followed by its descendants indented below their
parents, as one line each: its name, its working directory, and when it was
last active. It ends by saying that deletion can't be undone. Its buttons are
**Delete** and **Keep**.

Only the person's confirmation deletes. A refusal, a cancellation, an aborted
turn, or an unavailable session deletes nothing and returns a result the
model can read, which is not an error:

```json
{"declined": true, "resolution": "negative_confirmation",
 "message": "The user declined. Nothing was deleted."}
```

The tool asks through droids' tool confirmation
([ADR 0073](0073-layer-the-session-runtime-into-public-packages.md)): before
the call runs, the server resolves the sessions and writes the approval, and
the call runs only once the person approves. A call that can't be approved,
because an ID is invalid, no ID names a child, a session is busy, or the
sessions don't fit one approval, asks nothing and ends with an error result
that says why. The declined result above is the tool's own, written through
droids' declined hook.

### Deletion

After approval the server reads the stored parent links again and deletes
each requested session that is still the caller's child, with its
descendants as they are then: approving a child covers its descendants,
including any created after the approval was asked, which the approval
did not list. A requested session that has left the tree, by being deleted
or no longer having the caller above it, is reported as not deleted.

Each session is deleted through the session service's delete, the path a
client's delete takes: the session manager's permanent delete
([ADR 0028](0028-hard-delete-persisted-sessions.md)) and the server's
per-session workspace, diff, file-index, and annotation state. Descendants
are deleted before their ancestors, and a session is deleted only while it
has no children: the check and the deletion are one storage transaction, so
a fork made during the deletion keeps its parent. A session that can't be
deleted keeps every session above it, so deletion never turns a descendant
into a top-level session.

The result lists the deleted sessions, deepest first, and the sessions not
deleted with the reason for each. It is an error only when nothing was
deleted.

## Consequences

- A coordinating session can clean up after itself without the person
  deleting children one by one, and the person sees every session before it
  goes.
- Approval waits for the person, so a deletion holds the caller's turn until
  they answer.
- A pending approval survives a daemon restart and can be answered after it.
  Plugin approvals don't: the plugins' before-tool checks run again before
  the deletion, which then plans again from the stored parents.
- Session-level children only: a child session cannot delete its own forks,
  since it has no session tools.

## Related

- [ADR 0009: Keep temporary sessions process-local](0009-keep-temporary-sessions-process-local.md)
- [ADR 0017: Create top-level sessions from model tools](0017-create-top-level-sessions-from-model-tools.md)
- [ADR 0028: Hard-delete persisted sessions](0028-hard-delete-persisted-sessions.md)
- [ADR 0073: Layer the session runtime into public packages](0073-layer-the-session-runtime-into-public-packages.md)
