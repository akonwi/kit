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

`start` and `message` commit a queued task before returning its conversation and
task IDs. They do not wait for child completion. `wait` is bounded and intended
for exceptional synchronization rather than normal delegation. A `message`
after interruption creates a new task linked to the interrupted task; Kit never
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

A terminal task atomically creates an idempotent mailbox item. If the parent is
active, Kit admits that item as a droids boundary for the next safe model
boundary. If the parent is idle, no model call starts; the item is delivered
before the next user-initiated parent run. Only bounded result metadata and the
summary enter parent context, never the child transcript.

Children cannot invoke the `subagent` tool. Dismissal aborts active and queued
work, tombstones the conversation, and removes its child conversation store
after runtime cleanup.

## Native workspace

Open `/subagents` from the command palette to view the current session's roster.
The pane merges active conversations with discovered definitions, sorts active
states before available agents, and shows each agent's status, description,
model, source, and latest activity. Selecting a conversation opens a retained
child transcript tab. Running work can be cancelled and conversations are
dismissed through confirmed destructive dismissal. The workspace follows the
standard wide split and narrow tab layouts and refreshes only while the
Subagents pane is open.
