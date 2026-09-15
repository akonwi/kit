# Subagents

Subagents are durable, asynchronous child executions owned by one persisted
parent session. They use independent model context and conversation storage
while sharing the parent's workspace and applicable coding guidance.

## Definitions

Kit discovers non-recursive Markdown definitions from:

1. `$KIT_HOME/agents/*.md`
2. `<session cwd>/.kit/agents/*.md`

Files are loaded in sorted filename order and the first definition for a name
wins, so user definitions override project definitions. A definition requires
frontmatter `name` and `description`, accepts an optional model selector, and
uses its Markdown body as child instructions. Both canonical `provider/model`
selectors and production-compatible model IDs are retained during discovery.
At delegation time Kit canonicalizes selectors known to the v2 provider
registry. If an explicit selector is unavailable, the child inherits the active
parent droid's model and thinking configuration and the client shows an
ephemeral warning toast.

```markdown
---
name: scout
description: Inspects a repository and reports concise evidence.
model: openai/gpt-5.6-sol
---
Inspect only. Cite exact file paths.
```

Malformed and duplicate definitions produce non-fatal diagnostics. Native
clients show each newly encountered diagnostic as a persistent warning toast;
the roster also retains the current warning count. Reload the session context
to apply changed definitions to an already loaded parent.

## Model tool

The parent receives one `subagent` tool with these actions:

- `list_agents`
- `start`
- `message`
- `inspect`
- `wait`
- `cancel`
- `dismiss`

The model addresses subagents only by configured agent name; conversation and
task IDs are internal persistence details. `start(agent, message)` creates or
continues the named child session. `message(agent, message)` durably steers its
active droids turn through the same steering mechanism as a main session, so the
message is consumed at the next safe model boundary without interrupting an
in-flight provider response. If no turn is active, it starts or queues a new
turn instead. `wait(agent)` blocks until the child has no active or queued work
and returns immediately when it is already settled. A message after interruption
creates a new internal task linked to the interrupted task; Kit never
automatically retries interrupted work.

Only persisted sessions can delegate. Temporary sessions return
`SUBAGENT_UNAVAILABLE_TEMPORARY_SESSION` because their process-local lifecycle
cannot provide the durability promised by this feature.

## Scheduling and recovery

Kit runs one daemon-wide scheduler with fixed bounded global, per-session, and
queue limits. It preserves FIFO within each conversation and rotates among
parent sessions with eligible work. One conversation never runs more than one
task at a time.

Client detachment does not cancel child work. During daemon shutdown or startup
recovery, running tasks become `interrupted`; queued tasks remain queued and are
scheduled after recovery commits. Interrupted tasks are not resumed
implicitly.

## Results and isolation

Each conversation has its own droids SQLite store under
`$KIT_HOME/droids/subagents/`. Kit's main SQLite database owns conversation and
task lifecycle, scheduling, bounded summaries, and the parent mailbox.

A terminal internal task atomically creates an idempotent mailbox item. If the parent is
active, Kit admits that item as a droids boundary for the next safe model
boundary. If the parent is idle or unloaded, Kit loads it and starts an
autonomous context-only reaction turn. The model sees the bounded completion
boundary and decides whether to respond, use tools, delegate more work, or stop.
Kit limits autonomous parent reactions to four concurrent sessions and eight
consecutive reactions per session; reaching the chain limit leaves later
mailbox items pending until the next user prompt. Attached native clients watch
session-wide run admission and settlement, so autonomous responses appear
without requiring another user interaction. Only the agent identity, terminal state, bounded result metadata, and summary
enter parent context—never internal IDs or the child transcript. Pending
mailbox owners are scanned after startup so a crash cannot strand a committed
completion.

Children cannot invoke the `subagent` tool. Dismissal aborts active and queued
work, tombstones the conversation, and removes its child conversation store
after runtime cleanup.

## Native workspace

Open `/subagents` from the command palette to show the current session's modal
roster and status picker. It merges active conversations with discovered
definitions, sorts active states before available agents, and shows each agent's
status, description, model, source, and latest activity. Selecting a durable
conversation creates or focuses its retained child transcript tab. Starting or
activating background work does not create tabs or change their order.

Child tabs project durable history and live events through the same transcript
presentation used by the parent, including Markdown, buffered assistant prose,
pending thinking, tool-work chips, and inline tool activity. They follow live
output when pinned, open at the final response, and use the durable completion
summary while full history is still loading. Subagent-name labels in Agent work
chips and Activity tool rows open that same retained tab. Running work can be
cancelled, and conversations are dismissed through confirmed destructive
dismissal. The picker refreshes while open; an individual conversation refreshes
while its tab is active.
