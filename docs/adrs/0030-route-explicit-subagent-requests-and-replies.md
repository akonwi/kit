# 0030: Route explicit requests and replies between subagents

## Status

Accepted. Autonomous turn chains are governed by
[ADR 0040](0040-do-not-limit-autonomous-turn-chains.md).

## Context

A parent session can delegate to named subagents, but independent children need a
way to exchange findings without asking the parent to relay every message. Child
conversations already have durable tasks, isolated droids contexts, and a bounded
supervisor. Task completion, however, is not necessarily an answer to a question:
a child may ask another child for help, finish its current turn, and continue
when the answer arrives. Blocking a child tool call while another child works
would occupy a supervisor slot and can deadlock a chain of child requests.

Kit needs correlated, durable communication without giving children authority to
create, cancel, or dismiss siblings, treating an arbitrary terminal assistant
message as a reply, or copying child transcripts into each other.

## Decision

Kit routes bounded request and reply envelopes among subagent conversations
owned by the same persisted parent session. The server
owns routing, authorization, delivery, and recovery. The existing subagent
supervisor continues to own child task execution and queue limits; each child
retains its own droids context and transcript.

### Model-facing operations

The following names describe the intended semantics; the concrete tool names
and argument shapes may be refined during implementation:

- `subagent_ask(agent, message)` is a **parent-only** convenience operation. It
  creates a request to a named child and waits for that child's explicit reply.
  The parent receives the reply as this tool's result, not a second parent
  mailbox item. A bounded wait ending does not retract the durable request.
- `subagent_send(agent, message)` submits the same reply-capable request
  without waiting and returns an opaque receipt. A parent or child may call it.
  Child callers address only configured siblings; sending to an unstarted
  sibling initializes it for inbox work. All sent messages can receive an
  explicit reply; sending does not block the
  caller until one arrives.
- `subagent_reply(receipt, message)` explicitly answers a request in the
  recipient's inbox. The first durable reply wins. Only the addressed child can
  reply; ending a child turn does not implicitly answer a request.
- `subagent_inspect(receipt)` reads the caller's outgoing request state and
  bounded reply if available. `subagent_inbox` lists the recipient's outstanding
  incoming requests in bounded, paginated order; an addressed recipient may
  inspect one incoming receipt to recover its full bounded message. Neither
  operation exposes another child's transcript or internal task identities.

A parent can address a configured agent that has no conversation yet: admission
creates that child using the parent's normal definition and model resolution.
A child can address an existing non-dismissed sibling, including one whose
last task failed, aborted, or was interrupted, or initialize a configured
sibling that has never started. Definition lookup and model resolution use the
parent's authoritative catalog and persisted configuration; an unknown or
explicitly dismissed recipient cannot be created by a child. The child-facing
tool is bound to its actual conversation and owner by the host, not to a
model-supplied sender ID. It does not expose the parent's `start`, `wait`,
`cancel`, or `dismiss` capabilities. The parent retains its existing
asynchronous delegation and lifecycle controls. Child instructions permit this
restricted inbox initialization without granting general child lifecycle
control.

### Routing authority and durable state

A request records an opaque receipt, owner session, sender kind (parent or
child), sender child identity when applicable, addressed recipient child,
bounded message, creation/deadline times, state, and at most one bounded reply
or typed terminal failure with its resolution time. `ask` and `send` differ
only in whether the caller waits for this reply; every request is reply-capable.
The repository records a unique sender-turn/tool-call identity so a retried
send or reply cannot duplicate a request or overwrite a committed answer.

A child can address only a live named sibling belonging to its owner or
initialize a configured sibling that has never started, and cannot address
itself; a parent may also create an as-yet-unstarted configured child.
Authorization is checked again in the repository transaction that admits the
envelope. An envelope never changes recipient after
admission. Server-created delivery records associate the envelope with queued
recipient work and, for a child sender, with queued return delivery. Unique
identities make each admission idempotent across worker retries and daemon
restarts. Internal task IDs remain server-owned; models receive only bounded
receipts and renderer-safe sender/recipient names.

The request ledger and typed delivery records live in Kit's shared store
alongside subagent conversations and tasks. They are not stored in either
child's droids database. One delivery mechanism routes ordinary parent-assigned
task completions, explicit request replies, and terminal request failures to
their intended recipient. Each inbox delivery is an existing supervisor task
with a durable origin kind (parent work, incoming request, or incoming reply)
and request identity where applicable. Completion uses that kind to decide
whether to create a parent-directed delivery in the same transaction as the
task's terminal state. Session and conversation deletion settles affected
outstanding requests with a typed failure before removing recipient state; a
surviving sender must not silently lose an unanswered request. Deleting the owning parent removes its entire routing domain.

### Inbox delivery and reply

Submitting a request creates a durable pending delivery. The repository
atomically admits exactly one inbox-kind task for that delivery when queue
capacity allows; if the queue is full, the pending delivery remains durable.
The existing supervisor claims and runs that task in the recipient's ordinary
FIFO conversation queue. Instead of treating the message as a user prompt, the
child runtime admits a structured, idempotent droids boundary with a stable
receipt, sender identity, and bounded lower-trust content,
then starts an inbox reaction turn. The server, not the model's echoed receipt,
is authoritative for envelope identity and reply permission. Boundary admission and durable task identity reconcile a crash between the
shared store and the child droids store. Inbox turns are not counted or limited
by the number of preceding autonomous turns
([ADR 0040](0040-do-not-limit-autonomous-turn-chains.md)). The supervisor's
queue and execution bounds apply. A recipient can
finish a turn without replying; the request stays open, and the child can reply
in a later turn.

`subagent_reply` durably resolves the request and admits return delivery for
a child sender. If the sender is busy, that delivery waits in its queue. If it
is idle, the ordinary supervisor starts an inbox turn. No child holds an
execution slot waiting for the reply. The recipient sees a durable tool result
confirming that the reply was accepted; a retried call with the same identity
observes that result rather than sending another reply.

A parent request records its delivery mode at admission. For `subagent_ask`,
the waiter observes the resolved request and receives the reply as its sole
automatic result: no parent result boundary or parent-directed delivery is
created. If the ask turn is interrupted or the wait ends, the reply remains
durable for explicit inspect on a later turn; the canceled tool call is not
claimed to have resumed. For a parent-originated `send`, Kit
admits one idempotent parent-directed reply or terminal-failure delivery keyed
by receipt. The same delivery mechanism also handles parent-assigned task
completions, identified by their task ID. Kit delivers the item at a safe model
boundary or starts an autonomous parent reaction when idle.
Inspect reports a resolved send's state while its result awaits mailbox
admission, but exposes the reply only after mailbox delivery is durable. This
avoids a separate tool-result reaction before the automatic result arrives.
Durable per-request delivery state prevents both an ask tool result and a
parent-directed delivery for the same request.

Child request/reply inbox turns do not create parent completion deliveries,
regardless of whether the request came from a parent or sibling. A
parent-originated `send` creates a parent-directed delivery only for its
explicit reply or terminal failure, never for the recipient task's completion.
A parent-originated `ask` returns through its tool result instead of creating
a parent-directed delivery. Ordinary parent-started child tasks retain their
completion delivery behavior through the same typed mechanism. Task origin
and the parent request's delivery mode are persisted before execution so
completion and recovery apply the same policy.
The roster and child transcript still show lifecycle and activity for all
admitted child work.

### Failure, bounds, and recovery

Requests have finite, configurable deadlines; an unanswered request settles as
expired, and the sender receives that outcome through the same return path.
Recipient dismissal or deletion also settles open requests with typed failures.
Interrupted recipient work does not masquerade as an explicit reply: the
request remains open until another turn replies, its deadline passes, or the
recipient disappears. Replies attempted after resolution report the existing
terminal state. Canceling a waiting `ask` cancels only that wait, not the
request; the caller can inspect it later.

Routing respects the supervisor's per-conversation, per-session, and global
queue and execution limits. Pending delivery remains durable when a queue is
full and is retried on capacity changes and startup. Message/reply sizes,
outstanding requests per owner and child, inbox backlog, and individual request
deadlines are bounded. Requests do not carry a causal hop count. Models can
choose to exchange successive requests indefinitely; these limits bound
concurrent work, not the total number of sequential exchanges. Users can
cancel or dismiss the participating children. Asynchronous sends never reserve
a worker slot while awaiting another child. Startup scans reconcile open
envelopes, terminal failures, and undelivered replies without replaying a
committed tool side effect or silently admitting duplicate child work.

### Ownership boundaries

The subagent domain defines request state, repository operations, tool-facing
authorization, and supervisor delivery hooks. The shared storage implementation
performs admission, reply, and delivery transitions transactionally with its
child-task records where both are mutated. The child runtime factory supplies
only the restricted sibling tool; it does not install the parent delegation
tool. The session manager routes parent-originated requests and results without
letting clients or children open each other's droids stores.

## Consequences

A child can ask another child for information and resume when an explicit reply
arrives without blocking either child worker. Parent responses remain
unambiguous: a synchronous ask yields one tool result, a parent `send` yields
one eventual reply or failure delivery, and ordinary parent-originated
asynchronous delegation yields a task-completion delivery.

This requires new shared-store routing state, a child inbox admission path,
reply-aware tool surfaces, and recovery tests. Terminal task summaries alone
cannot serve as request replies: they do not record who asked the question or
whether the recipient intended to answer it. Peer-session queries remain a
separate capability with different participants and reply semantics.

## Alternatives considered

- **Expose the parent's `subagent` tool to children.** This gives children
  lifecycle authority, and its `message` action may steer unrelated active work;
  task completion still routes only to the parent.
- **Block a child `ask` tool until another child finishes.** Waiting children
  consume the supervisor's running slots and can deadlock on nested asks or
  cycles. Terminal task output is also not necessarily an intended reply.
- **Infer replies and parent results from task completion chains.** This
  couples unrelated turns and requires the server to guess when a child's
  answer is final. Explicit `reply` makes that decision belong to the
  recipient.
- **Copy sibling transcripts into a shared context.** This breaks isolation,
  attribution, and bounded context usage.

## Validation

Tests should cover same-owner authorization and self-address rejection,
idempotent retried tool calls, synchronous `ask` versus asynchronous `send`,
explicit one-time replies, two siblings exchanging requests without held worker slots,
queue saturation, expiration, dismissal, parent ask cancellation and result
routing, no duplicate parent mailbox items, restart between each durable
transition, and visible inbox/roster presentation.
