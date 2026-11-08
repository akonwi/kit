# 0061: Order the sessions list by attention

## Status

Proposed.

## Context

The iOS and browser clients open on a list of every session on a server. The
list is where a user decides what to look at next, so it should put first the
sessions that are blocked on them, then the ones that are working, then the
rest.

A client needs two things to draw that list:

- each session's identity, name, directory, and time of last change;
- whether the session is waiting on its user, working, or idle, and since
  when it has been waiting.

The server is authoritative for both. Interactions, runs, bash commands, and
subagent work are held by the session manager, and a pending interaction
belongs to a loaded runtime, so the manager knows every session that waits on
its user.

The iOS client merges the lists of several paired servers into one, so it
places sessions from different servers against each other and needs the same
ordering rule the server uses.

## Decision

### The attention order

The server defines one order for listed sessions, the attention order:

1. Sessions **awaiting a response**, the one waiting longest first: ascending
   by the creation time of the oldest pending interaction.
2. Sessions **running** a turn, a bash command, or subagent work, most
   recently changed first: descending by `updatedAt`.
3. **Idle** sessions, most recently changed first: descending by `updatedAt`.

Ties fall back to ascending session ID, so the order is total. The rule is part
of the session contract and documented beside `SessionList`. A client that
merges lists from several servers applies the same rule.

### Listed sessions carry their activity

`SessionInfo` gains an optional `activity` member, present only for a session
that is not idle:

- `status`: `awaitingResponse` or `running`, the values of
  `SessionActivityEntry.status`.
- `since`: for `awaitingResponse`, the creation time of the session's oldest
  pending interaction. Absent for `running`.

`listSessions` (`GET /v1/sessions`) gains an optional `order` query parameter.
With `order=attention` the server returns the sessions in the attention order
with `activity` filled in, and sets `SessionList.order` to `attention`. Without
it the response is unchanged: no `activity`, no `order`, the existing order,
and the list stays a read of saved sessions that touches no runtime. Released
clients never send the parameter, so the lists they show do not change. An
order the server does not know is refused with 400 `invalid_request`.

`getSessionActivity` stays for released clients.

### Clients poll

Clients keep the list current by requesting it again while it is on screen,
at an interval of their choosing, and stop when it leaves the screen. One
request returns the sessions and their activity together, so every response is
consistent on its own.

### Compatibility

The query parameter and the `activity` and `order` members are additive, so
the session protocol stays 50. A server released before them ignores
`order=attention` and returns a list without `SessionList.order`. The Go
client's `ListSessionsByAttention` reports that as an `UnsupportedError`. The
client then requests `getSessionActivity` alongside the list, as before, and
applies the attention order itself.

Clients rely on `SessionList.order`: a list is in the order a client asked for
only when the response names that order. The 400 for an unknown order keeps a
mistyped request from being answered with an unordered list. It is not how
clients detect support: a client that asks for an order added after
`attention` treats that refusal the same as a missing `order`, since older
servers ignore the parameter.

### What does not change

- What a list row says beyond its status: preview lines for waiting and
  working sessions stay with each client.
- Whether a session is marked unseen: that stays per device.
- The order of the session lists in the macOS app and the terminal client,
  which do not request the attention order.

## Consequences

- A session waiting on its user is at the top of the list on the iOS and
  browser clients, and gets there at the client's next poll.
- One request gives a consistent list and activity, instead of two requests
  joined by the client.
- The attention order is a contract: changing it, for example ranking unseen
  sessions above idle ones, is a protocol decision, and clients that merge
  lists change with it.
