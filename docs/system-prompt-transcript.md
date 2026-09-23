# System prompt in the transcript

Since pi-ai / pi-agent-core 0.87, the system prompt and tool declarations are
system messages inside the model transcript rather than a separate field.
The current prompt is the replay of every system message (`content` appended,
`sections` patched by name, tools added/removed). Models that accept
mid-conversation system messages receive later updates in place; others get
the replay collapsed into one leading message.

## Decisions

- **System messages are model-only.** They live in `Agent`'s pi transcript
  and never enter Kit turns, `message.committed`, or persisted sessions.
  Restored turns are stripped of any system messages.
- **Named sections.** Kit owns sections listed in `SYSTEM_SECTIONS`
  (`runtime/agent.ts`):
  - `kit.system-prompt`: base prompt, plugin additions (sorted for a stable
    order), external-plugin slots, and project context files.
  - `kit.scratchpad`: the active session scratchpad, kept separate so edits
    replace only this section instead of re-sending the whole prompt.
- **Coalesced updates.** `Agent.setSystemPrompt` / `setSystemSection` queue
  changes; they are applied once when the next run starts (`prompt` /
  `continue`). Changes that revert to the current value are dropped. Before
  the `Agent` instance's first run, changes are folded into the single leading
  system message, including for restored sessions. System messages are never
  persisted, so this keeps restarts to one copy of the prompt with a stable
  request prefix. After the first run, changes are appended as updates. Use `Agent.systemPrompt` to read the effective prompt including
  queued changes.
- **Mid-run changes wait.** A run snapshots the transcript when it starts, so
  section changes made during a run apply at the start of the next run.
