# Sub-agent session storage

Sub-agent conversations use session-shaped JSONL files, but they are not user-facing sessions.

## Layout

- Primary and handoff sessions: `~/.kit/sessions/<id>.jsonl`
- Sub-agent conversations: `~/.kit/sessions/subagents/<id>.jsonl`

The nested directory keeps sub-agents out of normal session discovery without encoding storage semantics in their IDs. A sub-agent header records its owner session, agent identity, source, model, and thinking level.

## Persistence boundary

The parent session retains only lightweight delegation lifecycle entries needed to preserve its transcript and restore the active sub-agent relationship. Detailed sub-agent history is written to the child file.

Child files persist prompts, message-start markers, completed assistant messages, completed tool results, compactions, failures, and aborts. Streaming text and thinking deltas and partial tool updates remain transient runtime events. A completed compaction atomically replaces the child history with its header and compaction entry, which embeds the retained turns.

Legacy sub-agent entries embedded in a parent session are not migrated into child files. Continuing a legacy conversation creates a fresh child history; the old parent entries remain untouched.

## Observation

Sub-agent conversations are observed through the `/subagents` explorer rather than the normal session list. On wide terminals the explorer uses a conversation-list and transcript split; narrow terminals drill from the list into a transcript view.

Completed transcript content is reconstructed from the child file. Streaming assistant messages and tool progress are published as transient manager state so the viewer can update live without persisting deltas. A per-conversation transcript revision refreshes durable content only after child entries have been written.

## Lifecycle

Sub-agent files are ephemeral but survive ordinary process and parent-session switches so conversations can continue. Dismissing a sub-agent aborts any active runtime, records the parent tombstone, and deletes the child file. Deleting an owner session cascades to its child files.
