# Sub-agent session storage

Sub-agent conversations use session-shaped JSONL files, but they are not user-facing sessions.

## Layout

- Primary and handoff sessions: `~/.kit/sessions/<id>.jsonl`
- Sub-agent conversations: `~/.kit/sessions/subagents/<id>.jsonl`

The nested directory keeps sub-agents out of normal session discovery without encoding storage semantics in their IDs. A sub-agent header records its owner session, agent identity, source, model, and thinking level.

## Persistence boundary

The parent session retains only lightweight delegation lifecycle entries needed to preserve its transcript and restore the active sub-agent relationship. Detailed sub-agent history is written to the child file.

Child files persist prompts, message-start markers, completed assistant messages, completed tool results, compactions, failures, and aborts. Streaming text and thinking deltas and partial tool updates remain transient runtime events.

Legacy sub-agent entries embedded in a parent session are not migrated into child files. Continuing a legacy conversation creates a fresh child history; the old parent entries remain untouched.

## Lifecycle

Sub-agent files are ephemeral but survive ordinary process and parent-session switches so conversations can continue. Dismissing a sub-agent aborts any active runtime, records the parent tombstone, and deletes the child file. Deleting an owner session cascades to its child files.
