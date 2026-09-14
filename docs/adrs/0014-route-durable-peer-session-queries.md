# 0014: Route durable queries between peer sessions

## Status

Accepted

## Context

Independent top-level sessions may develop useful knowledge after a semantic
fork or while working in parallel. A droid sometimes needs an answer informed
by another session's current conversation rather than a copy of that
conversation. Sharing raw transcript records would expose storage details,
consume the caller's context, and blur which droid produced a conclusion.
Treating the operation as a merge would also imply transcript mutation or
session lifecycle authority that peer sessions do not have.

Kit already runs multiple authoritative sessions concurrently. Each session has
one independently serialized droid runtime and Store, while the Kit server owns
cross-session metadata and scheduling. A peer query therefore crosses two
durability and execution domains and cannot be an in-process call from one
droid to another.

## Decision

Kit provides a durable peer-session mailbox. A droid can discover eligible
sessions, enqueue a query for one session, and inspect or wait for its result.
The recipient processes the query in its own context and returns the terminal
assistant response to the sender. Neither peer receives lifecycle control over
the other.

The initial model-facing capability has three operations, with exact tool and
Go names subject to normal refinement during implementation:

- discover eligible peer sessions;
- send a query and receive a stable request identity without waiting for its
  execution;
- inspect or wait, with a bounded timeout, for a request's terminal result.

Follow-up queries are new requests and may carry a thread identity and a
reference to the preceding request. The protocol does not copy either peer's
transcript into the other.

### Kit owns routing and persistence

The Kit server owns mailbox envelopes, request state, scheduling, and replies in
its shared store. Droids continues to own each session's transcript, context,
turns, tools, and execution state. Session clients and model tools use
server/session contracts and never open another session's droid Store or query
SQLite implementations directly.

A request records at least:

- a globally unique request ID;
- an idempotency identity derived from the sending session and tool call;
- sender and recipient session IDs;
- bounded message content;
- optional bounded thread, preceding-request, and hop metadata;
- queued, processing, and terminal state;
- the recipient turn identity once admitted;
- a bounded terminal assistant result or typed failure;
- creation, start, and completion times plus a mutation generation.

Insertion is idempotent for one sending tool call. Records are ordered
oldest-first per recipient. Repository operations use generation checks for
claims and terminal transitions.

Mailbox persistence is separate from the existing subagent parent mailbox.
Peer requests have symmetric session identities, request/reply state, and
authorization rules; they do not pretend to be child tasks or parent-owned
results.

### Queries run as distinct recipient turns

A peer query does not steer an unrelated active user turn. If the recipient is
busy, the request remains queued until that session reaches a settled boundary.
The recipient processes one claimed query as a distinct autonomous,
context-only reaction turn. This preserves correlation between one query and
one terminal response and prevents unrelated user-facing output from becoming
a peer reply.

Kit delivers the request through the droids boundary-message API with:

- the stable mailbox request ID as the boundary receipt identity;
- a dedicated peer-query boundary kind and session source;
- renderer-safe content identifying the sending session by current name and ID;
- bounded versioned details carrying canonical correlation metadata.

The boundary is context, not a synthetic user message. Kit uses droids receipt
and boundary status to reconcile ambiguous delivery and calls the ordinary
context-only reaction API to admit the turn. The recipient's configured model,
reasoning, system prompt, and tools apply to the reaction. Peer queries may
therefore cause normal tool side effects.

A future capability policy may provide a restricted tool bundle for peer-query
turns. That mitigation must preserve the mailbox and correlation contract and
must be visible to the recipient model; it is not required by the initial
implementation.

### Replies are captured by the host

When the dedicated recipient turn settles, Kit projects its terminal assistant
response into the request result. Completed, failed, aborted, and interrupted
turns all produce terminal request states. Tool activity and internal transcript
records are not copied into the sender's conversation.

The result becomes a boundary message for the sending session. If the sender is
active, droids may consume that result at a safe model boundary. If the sender
is idle, the result remains available to the inspect/wait tool and does not by
itself start another autonomous turn. This rule prevents automatic query/reply
ping-pong. A sender can explicitly continue after observing the result.

A terminal recipient turn and the shared mailbox result cannot commit in one
transaction. Kit uses stable request and recipient-turn identities to reconcile
this boundary after ambiguous writes or daemon restart. It never admits a
second recipient turn after durable association with the first.

### Discovery and authority

Discovery is server-authoritative and excludes the calling session, temporary
sessions, archived sessions, and sessions unavailable to the current single-user
server. Results are bounded and include only model-useful metadata: session ID,
current name, CWD, immediate lineage, and coarse availability. Discovery does
not expose transcript content, credentials, pending interactions, or internal
runtime/store identities.

Sending revalidates the recipient independently of discovery. A queued request
to a recipient that becomes archived or unavailable settles with a typed
failure rather than silently retargeting. A peer cannot switch, rename, archive,
delete, abort, retry, or otherwise control another session.

### Scheduling, loops, and bounds

Peer-query execution participates in daemon-wide and per-session autonomous
reaction limits. It also preserves each session's admission serialization and
does not hold the sender's session mutation lock while the recipient executes.
Mailbox workers and scans are bounded, cancellable, and restart-safe.

Kit bounds:

- query and reply text;
- discover result count;
- queued requests per sender and recipient;
- delivery batches and concurrent recipient reactions;
- inspect/wait duration;
- thread depth and cross-session hop count;
- retained terminal requests.

Self-query is rejected. A request carries its ancestral route so Kit can reject
a cycle before admission. Timeout of a waiting tool call does not cancel the
durable peer request. Provider or tool side effects are not claimed to be
exactly resumable after interruption; recovery reports an interrupted terminal
result when safe continuation cannot be proven.

The autonomous reaction-chain policy in droids applies to peer-query boundaries
as well as subagent-result boundaries. Droids exposes a generic or explicit
policy for reaction-driving boundaries rather than requiring Kit to evade chain
accounting with application-specific kinds.

### Events and presentation

Request transitions are projected as bounded session events so clients can
refresh authoritative state after reconnect or replay loss. Transcripts and
diagnostics identify peer-query boundaries and results with source session and
request identity. Clients may add convenience workflows, but the mailbox is a
server capability and is not dependent on one UI remaining attached.

## Required properties

The implementation must demonstrate:

- bounded discovery with authoritative eligibility revalidation;
- idempotent send admission under repeated and ambiguous tool execution;
- deterministic per-recipient ordering and one active mailbox query per
  recipient session;
- queuing behind active, paused, interrupted, compacting, or otherwise occupied
  recipient work without steering that work;
- exactly one associated recipient turn for each admitted request;
- normal recipient tools and side effects during the initial capability;
- completed, failed, aborted, interrupted, archived-recipient, and unavailable
  terminal results;
- durable result inspection after sender or recipient unload and daemon restart;
- reconciliation across the shared Kit store and independent droid Stores;
- safe-boundary result delivery to an active sender without autonomous reply
  chains;
- cycle, self-query, queue, payload, worker, timeout, and retention bounds;
- concurrent unrelated sessions making progress without a process-global active
  session or cwd;
- validated protocol values and renderer-safe model context at every boundary.

## Consequences

### Positive

- Sessions collaborate through conclusions produced by their own droids rather
  than transcript copying or merge semantics.
- Fork parents and children can exchange later knowledge while remaining
  independent.
- Queries remain durable when clients disconnect and when target sessions are
  not loaded.
- Stable request and turn identities make cross-store recovery testable.
- The initial protocol can later support restricted recipient capabilities
  without changing callers.

### Trade-offs

- A query consumes provider tokens and may perform side effects in the recipient
  workspace.
- Replies are eventual rather than synchronous and may wait behind recipient
  work.
- Cross-store atomicity requires idempotent relay and reconciliation logic.
- Explicit cycle and reaction limits are necessary because peer sessions form a
  graph rather than an ownership tree.
- Terminal mailbox records require a retention policy separate from transcript
  retention.

## Alternatives considered

### Copy or expose another session's transcript

Rejected as the primary mechanism. It consumes caller context, couples the
feature to transcript projection, and asks the caller to infer conclusions that
the recipient droid can provide directly.

### Merge a child transcript or summary into its parent

Rejected. Peer consultation must not mutate transcript ancestry, delete a
session, or imply parent/child lifecycle authority.

### Execute the recipient immediately inside the sending tool call

Rejected. A long blocking nested call couples two session locks and lifecycles,
creates deadlock pressure, and handles busy recipients and disconnects poorly.
Durable enqueue plus bounded inspection/wait keeps execution independently
recoverable.

### Steer an active recipient turn

Rejected. Its final response could mix a user's active request with a peer query,
making correlation and disclosure boundaries ambiguous.

### Reuse the subagent parent mailbox tables and types

Rejected. Those records encode parent ownership, child conversation/task
identity, and terminal child-result semantics that do not apply to symmetric
peer sessions.

## References

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [0004: Model a droid as an autonomous agent runtime](./0004-droids-agent-runtime-boundary.md)
- [0005: Fork settled droid conversations semantically](./0005-droids-semantic-forking.md)
- [0006: Make droids the session data authority](./0006-droids-as-session-data-authority.md)
- [`../droids-sdk.md`](../droids-sdk.md)
- [`../features/subagents.md`](../features/subagents.md)
