# 0073: Layer the session runtime into public packages

## Status

Accepted. Extends [ADR 0043](0043-publish-droids-from-the-kit-module.md), which
published the droids runtime, with the layers Kit builds above it. Builds on the
session contract of [ADR 0036](0036-publish-turns-events-and-transcript-contract.md),
the native clients of [ADR 0056](0056-build-a-native-ios-client.md), the
client-hosted tools of [ADR 0051](0051-let-attached-clients-host-session-plugins.md),
and the open session event union of
[ADR 0072](0072-share-shell-executions-with-every-attached-client.md).

## Context

Droids gives an application one durable autonomous conversation, with its own
turn controls: starting a turn, steering a running one, informing a future one,
pausing, aborting, and shutting down. A product that people chat with needs
more above it:

- a registry of many conversations and supervision of each one's droid;
- a persisted, sequenced event stream and a paged transcript that clients can
  replay;
- interactions that ask the person something (confirm, input, select, guided
  questions, authorize) and wait for the answer;
- subagents with mailboxes, conversation naming, model configuration, and
  attachments;
- an HTTP API and streaming transport for all of that;
- clients that render transcripts, figures, and interactions.

Kit has all of these, below `internal/` (`internal/session`,
`internal/server`) and in its clients (`apps/apple/KitShared`, `apps/web`).
Other applications built on droids rebuild them. Ranger, a household assistant
on droids, maintains its own conversation runtime, approvals, follow-up
questions, transcript projection, stream protocol, and iOS and web chat
clients. It wants Kit's conversation experience while remaining its own
service, with its own accounts, data, tools, and deployment.

Several properties of Kit's session layer keep it from serving another
application:

- **It is single-owner.** A session belongs to the one person who runs the
  server. There is no owner or scope on a session, and no way to host many
  people's sessions in one process.
- **It assumes a coding workspace.** A session record requires a working
  directory, and the runtime bundle builder receives a working-directory
  provider. Bash executions, version control, pull requests, annotations, peer
  queries, prompt commands, and process plugins live in the same manager as
  turns and events.
- **Follow-ups are queued in memory.** Messages sent while a turn runs wait in
  the session manager's own queue and are lost on restart. Droids has no way
  to queue a prompt for the next turn, so the session layer built one.
- **Interactions are held in memory.** A pending confirmation is cancelled when
  the server shuts down. A person answering from a phone hours later, after a
  restart, loses the question.
- **Tools cannot require confirmation before they run.** A tool can ask a
  question while it executes, but a host cannot declare that a tool runs only
  after the person approves its exact arguments.
- **The contract has no core.** Coding features (working-directory changes,
  version control and pull-request streams, bash history) sit in the same
  contract as turns and transcripts, so a client cannot target only the
  conversation.

## Decision

Kit publishes its session runtime as layers above droids, and moves what
governs a single conversation into droids itself. Each layer is
optional and depends only on the layers below it. Kit is built from these
layers plus its coding extensions, the same way another application is.

```text
 Clients      core client + conversation views (Swift, TypeScript)
 Transport    sessionhttp: core session API and event stream
 Control      sessions: registry, scopes, event stream, transcript,
 plane        subagents, extensions
 Runtime      droids: one durable conversation, with turns, steering,
              queued prompts, interactions, tool confirmation
```

Package names below are working names.

### The control plane: `github.com/akonwi/kit/sessions`

`sessions` hosts many conversations for many owners in one process. It owns:

- the session registry: create, fork, rename, archive, delete, temporary
  sessions;
- supervision of each session's droid: opening, closing, and exposing droids'
  turn controls to clients;
- the persisted, sequenced event stream and its replay;
- transcript projection and paging;
- projecting droids' queued prompts and interactions to clients;
- subagents, their mailboxes, and their supervision;
- model and thinking configuration, and compaction through droids;
- conversation naming;
- attachment ingestion through an `attachment.Store` interface;
- client-hosted tools (ADR 0051).

Host applications supply:

- **Storage**: repository interfaces for sessions, events, subagent
  mailboxes, and attachments, with SQLite implementations provided.
- **A runtime bundle builder**: the prompt, tools, MCP namespaces, and subagent
  catalog for a session, given its record. The builder receives the session's
  scope and extension metadata, not a working directory.
- **Extensions**: additional per-session state, event kinds, and operations
  registered with the manager. Working directory, shell executions, version
  control, pull requests, annotations, peer queries, prompt commands, the
  scratchpad, and process plugins are Kit extensions. An extension that
  raises an interaction, as a process plugin does, asks through the session's
  droid.
- **Observers**: callbacks for session events, so a host can, for example, send
  a push notification when a turn finishes or an interaction is requested.

`sessions` must not import packages below Kit's top-level `internal`
directory, as ADR 0043 requires of droids.

### Conversation behavior belongs to droids

Everything that governs one conversation is a droids feature, because the
droid is already the durable agent. Droids provides durable steering,
informing a future turn, reacting to a boundary message, pausing, resuming,
aborting, and shutting down. It gains three more, recorded in the droid's
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
  can read. A retried or replayed call runs at most once.
- A turn waiting for a declared interaction is waiting, not running. It stays
  waiting across restarts, and the answer continues it.

`sessions` keeps no queue or interaction state of its own. Its follow-up
operations (queue, restore to the composer, promote to steering) and its
interaction operations (list pending, respond) are thin projections of the
droid's, for clients and the transport. Kit starts a restored queue when it
loads the session, after resuming any turn a restart interrupted. A session
loads the first time anything needs it: a client, a peer query, a subagent
result, an ACP client, or a plugin.

### Scope

Every session has an opaque **scope**, set at creation and immutable. Every
operation takes the caller's scope, and a session is visible only within its
scope. `sessions` does not authenticate or authorize people; the host maps a
request to a scope. Kit uses one scope for its single user. A multi-person
host uses one scope per person, or per shared space, and decides who may act
in which scope.

### The transport: `github.com/akonwi/kit/sessions/sessionhttp`

`sessionhttp` serves the core session API and event stream as an
`http.Handler` that a host mounts under its own path. The host provides
authentication middleware that yields the request's scope. Kit's server mounts
it with device-token authentication and adds its extension routes. Another
host mounts it with its own sign-in.

### The core contract

The session contract (`api/kit-session.openapi.json`) is divided into a
**core** and **extensions**:

- The core covers sessions, turns, events, transcript, interactions, subagents,
  configuration, attachments, and client-hosted tools.
- Each extension (working directory, shell executions, version control, pull
  requests, annotations, peer queries, prompt commands, scratchpad, plugins,
  devices) is advertised as a capability by the server.
- Protocol versioning (ADR 0045) applies to the core and to each extension.
  Clients ignore event kinds and capabilities they do not use, and hide
  features whose capability is absent. The session event union is open
  (ADR 0072), so an extension adds event kinds, as shell executions add
  `bash.started` and `bash.completed`, without a core protocol change.

### Clients

- `KitShared` separates a core client and the conversation views (transcript,
  figures, Mermaid, interactions, attachments, dictation) from Kit-specific
  views, such as annotations and shell executions. The conversation views take a theme and a vocabulary, so another
  application can present them under its own design and words.
- The TypeScript types and client for the core contract are published as a
  package that other web applications can use.

### Order

Each step leaves Kit working:

1. Queued prompts in droids; Kit's follow-ups move onto them.
2. Interactions and tool confirmation in droids; Kit's interaction broker
   moves onto them.
3. Scope on sessions; the working directory moves to an extension.
4. Split the manager into the core and Kit extensions. The shell-execution
   extension keeps the lock order ADR 0072's implementation documents on the
   manager.
5. Publish `sessions` and `sessionhttp`; Kit's server consumes them.
6. Divide the contract into core and extensions, with capabilities.
7. Split `KitShared`, and publish the TypeScript core client.

## Consequences

Positive:

- Applications on droids get Kit's conversation runtime, transport, and
  clients without rebuilding them, while keeping their own accounts, data,
  tools, and deployment.
- Droids gains durable queued prompts, interactions, and tool confirmation,
  so every droids application gets them, and a person can answer a declared
  interaction from Kit's iOS client after a restart.
- The coding features become explicit extensions, which makes the core easier
  to reason about and test.
- Ranger can replace its conversation runtime and chat clients with these
  packages.

Trade-offs:

- Kit publishes a larger supported Go API, following ADR 0043's pre-1.0
  versioning, and changes to it need compatibility review.
- The manager split is a large refactor of Kit's most central package.
- Multi-scope hosting adds a scope check to every session operation and
  storage query.
- `KitShared` and the TypeScript client become shared dependencies of other
  applications, so Kit-specific changes must stay out of the core views.

## Alternatives considered

- **Keep the layers internal and run one Kit server per person for other
  applications.** No Kit refactor, but each person needs an isolated machine
  because Kit's tools run on the host, and the other application cannot share
  conversations between people or own sign-in.
- **Add accounts to Kit's server.** Makes Kit a multi-user product, which
  contradicts its single-user, local-first model.
- **Let each application build its own session layer on droids.** The current
  state for Ranger; it duplicates the runtime and clients and drifts from Kit.
- **Publish only the clients.** Other applications would still have to
  implement the contract's server side by hand.

## Open questions

- Package names and whether the transport is a subpackage or a sibling. The
  vocabulary is expected to move from "sessions" to "threads"; if it does,
  whether Kit's wire contract follows. This is settled before step 5.
- Whether the scratchpad and subagent mailbox belong in the core.
- How existing Kit sessions migrate to scoped records with working-directory
  extension metadata.

## Related

- [0036: Publish the turns, events, and transcript contract](0036-publish-turns-events-and-transcript-contract.md)
- [0043: Publish droids from the Kit module](0043-publish-droids-from-the-kit-module.md)
- [0045: Connect clients to local, SSH, and network servers](0045-connect-clients-to-local-ssh-and-network-servers.md)
- [0051: Let attached clients host session plugins](0051-let-attached-clients-host-session-plugins.md)
- [0056: Build a native iOS client](0056-build-a-native-ios-client.md)
- [0072: Share shell executions with every attached client](0072-share-shell-executions-with-every-attached-client.md)
- Ranger ADR 0016, "Build Ranger's conversation on Kit's session layers"
