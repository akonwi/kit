# 0073: Make droids the unit applications compose, and share conversation UI

## Status

Accepted. Extends [ADR 0043](0043-publish-droids-from-the-kit-module.md), which
published the droids runtime, and builds on the native clients of
[ADR 0056](0056-build-a-native-ios-client.md).

## Context

Droids gives an application one durable autonomous agent, with its own turn
controls: starting a turn, steering a running one, informing a future one,
reacting to a boundary message, pausing, aborting, and shutting down.

Applications that people talk to compose droids in different shapes. Kit
hosts sessions for one person, each with a coding workspace, subagents, child
sessions, and queries between sessions, and serves them through its session
contract to a terminal, macOS, iOS, and browser client. Ranger, a household
assistant on droids, has its own accounts and households, a main conversation
and projects per person, its own routes and sign-in, and a simpler
conversation experience. Their registries, ownership, relations between
droids, coordination, and network APIs differ because their products differ.

Two things repeat across them:

- **Behavior of one conversation that must survive restarts.** Prompts sent
  while a turn runs wait for the next turn. A question to the person, or an
  approval a tool needs before it runs, stays pending until the person
  answers, possibly hours later from a phone. Built above droids, each
  application has to keep this state durable and consistent with the
  droid's own.
- **Rendering a conversation.** Markdown, figures, Mermaid diagrams, code
  highlighting, tool rows, chips, the composer, and the interaction dock are
  the same work in every client, and the difference between applications is
  their design and words, not the components.

## Decision

The droid is the unit. Droids owns the behavior of one droid's conversation.
Applications compose droids into their own products, and share conversation
UI rather than a session runtime, transport, or contract.

```text
 Application   registry, ownership, relations between droids, coordination,
               network API, product UX           (Kit, Ranger, …)
 Shared UI     conversation components: markdown, figures, Mermaid,
               highlighting, tool rows, chips, composer, interaction dock
 Runtime       droids: one durable agent, with turns, steering, queued
               prompts, interactions, tool confirmation
```

### Conversation behavior belongs to droids

Everything that governs one conversation is a droids feature, because the
droid is already the durable agent. Droids provides durable steering,
informing a future turn, reacting to a boundary message, pausing, resuming,
aborting, and shutting down, and three more features recorded in the droid's
store.

**Queued prompts.**

- A prompt admitted with a queue option starts the next turn once the current
  one settles, in admission order. When the droid is idle it starts at once.
- A turn that is paused or interrupted holds the queue until it settles. A
  turn that ends in a policy stop leaves the queue waiting.
- The queue is bounded.
- A queued prompt carries bounded application metadata, which droids returns
  with it, so a host can restore the prompt as the person wrote it.
- Queued prompts can be listed, withdrawn, and promoted to steering the
  running turn.
- A queued prompt that cannot start stays first in the queue with the reason,
  and the queue waits until that prompt is withdrawn.
- The queue survives restarts, but a restored queue waits until the
  application starts it, so queued work does not run when nobody is attending
  to the conversation. Queuing another prompt on an idle droid starts it.
- An application that follows the turns its queue starts can have droids
  track them: each started prompt stays, across restarts, until the
  application acknowledges it, and the queue waits while too many are
  unacknowledged.

**Interactions.**

An interaction asks the person something: confirm, input, select, guided
questions, or authorize. Droids records every interaction in its store,
reports pending and settled interactions as events and in snapshots, and
settles each one once. Interactions take two forms:

- A **declared** interaction is derived from a tool call's arguments before
  the tool runs. The call parks in its durable pre-execution phase until the
  interaction settles, and the tool then runs with the answer. A declared
  interaction survives restarts, and its answer resumes the call, after a
  restart if necessary. The application's before-tool policy still applies:
  if the policy identity that admitted the call changed while it waited, as
  Kit's plugin policy does with every daemon lifetime, droids runs the
  before-tool hook again under the current policy before the call runs, and
  the answer stands. A call whose policy changed at any other point is
  blocked.
- A **live** interaction is asked by code that is already running: a tool
  during its execution, such as an MCP server asking for authorization, or the
  application outside any turn. It is listed and settled like a declared one,
  but the code waiting for it does not outlive the process, so after a
  restart droids settles it as unavailable.

Droids provides standard confirm, input, select, and guided-question tools as
declared interactions. An application chooses whether to include them and
what to name them.

A prompt submitted to steer while one of the running turn's interactions is
pending settles that interaction as declined, with a reason the model can
read, and steers into the turn. This is the only rule. Queued prompts are
unaffected: they start only after the turn settles. Whether a client lets the
person send a prompt while an interaction is pending is the client's choice.
Kit's clients do not; the person answers the interaction first.

**Tool confirmation.**

- A tool definition may require confirmation before it runs. A flag asks
  before every call with a default question: "Allow <tool>?", with the call's
  arguments as its detail. Optionally, the tool decides per call whether to
  ask, and writes the yes-or-no question, which can show facts it resolved,
  such as what a call would change; fields it leaves empty get the defaults.
  Droids raises the question as a declared confirm interaction after any
  pre-execution hooks allow the call, and runs the tool only with the
  arguments that were approved. The tool rechecks what it acts on when it
  runs.
- Denial, or a question settled without an answer, returns a result the model
  can read. A tool may write its own result for a declined call instead. A
  retried or replayed call runs at most once.
- A turn waiting for a declared interaction is waiting, not running. It stays
  waiting across restarts, and the answer continues it.

Kit's session layer keeps no queue or interaction state of its own. Its
follow-up operations (queue, restore to the composer, promote to steering)
and its interaction operations (list pending, respond) project the droid's
for its contract. Kit starts a restored queue when it loads the session,
after resuming any turn a restart interrupted. A session loads the first time
anything needs it: a client, a peer query, a subagent result, an ACP client,
or a plugin.

### Applications compose droids

Droids provides no registry, group of droids, ownership, or transport. Each
application decides which droids exist, who may use them, how they relate,
how they coordinate, and how clients reach them. Coordination between droids
is built on what droids provides: boundary messages with idempotent receipts,
reactions to them, and forks.

Kit's sessions, workspace, subagents, child sessions, peer queries, and
session contract are Kit's composition, for one person. Another application
composes droids for its own product, with its own accounts, routes, and
network API.

### Shared conversation UI

`KitShared` provides conversation UI modules that any Swift application can
use:

- markdown and transcript rendering, figures, Mermaid, and code highlighting;
- tool rows and chips;
- the composer and the interaction dock;
- dictation.

The modules take view models, a theme, and a vocabulary. They do not depend
on Kit's API client, so an application feeds them from its own API and
presents them in its own design and words. Kit's API client and Kit-specific
presentations, such as shell executions, created and deleted sessions, and
annotations, are separate modules that Kit's apps use alongside them.

Shared web UI is outside this decision.

### Order

Each step leaves Kit working:

1. Queued prompts in droids; Kit's follow-ups move onto them.
2. Interactions and tool confirmation in droids; Kit's interaction broker
   moves onto them.
3. Split `KitShared`'s conversation UI from Kit's API client and Kit-specific
   presentations, and move the composer, chips, and interaction dock into it,
   in stages that each keep the macOS and iOS apps building and passing their
   tests.

## Consequences

Positive:

- Every droids application gets durable queued prompts, interactions, and
  tool confirmation, and a person can answer a declared interaction after a
  restart.
- Applications keep the shape their product needs, without carrying Kit's
  sessions, workspace, or relations between droids.
- Swift applications render conversations with Kit's components, under their
  own design and words, and receive Kit's improvements to them.

Trade-offs:

- Each application builds its own registry, ownership, transport, and the
  projection from droids' events and snapshots to its clients.
- Each application maps its API to the conversation modules' view models.
- The conversation modules become shared dependencies of other applications,
  so Kit-specific changes stay out of them, and changes to them need
  compatibility review.

## Alternatives considered

- **Publish Kit's session layer as packages**: a control plane hosting many
  owners' sessions, scoped by owner, with a mountable HTTP transport and a
  core contract with capability-advertised extensions. It carries Kit's
  opinions about sessions, subagents, child sessions, and queries between
  sessions, and another application's registry, routes, and ownership don't
  match Kit's.
- **A group of droids, a thread or pod, in droids**, owning membership,
  delivery between droids, and one stream across them. Applications differ
  in who starts which droid, who may message whom, and how a group appears to
  people.
- **A standard network contract for one droid's conversation, with shared
  clients.** It fixes every application's API to one shape; view models let
  each application keep its own.
- **Keep conversation behavior in each application.** Each rebuilds durable
  queues, interactions, and confirmation above droids and keeps them
  consistent with the droid's state.
- **Run one Kit server per person for other applications.** Each person needs
  an isolated machine because Kit's tools run on the host, and the other
  application cannot share conversations between people or own sign-in.

## Open questions

- Whether droids offers an optional control plane: a registry of an
  application's droids with metadata, to make managing many of them easier.
- The shape of the conversation modules' view models, and whether they follow
  droids' events and snapshots.

## Related

- [0043: Publish droids from the Kit module](0043-publish-droids-from-the-kit-module.md)
- [0056: Build a native iOS client](0056-build-a-native-ios-client.md)
- [0071: Let a session delete its child sessions](0071-let-a-session-delete-its-child-sessions.md)
- Ranger ADR 0016, "Build Ranger's conversation on Kit's session layers"
