# Backlog

This file is the source index for the backlog.

Keep this list short and current. If an item needs more detail, link to a dedicated file in this directory.

## Conventions

- `[ ]` not started
- `[x]` done

Delete done items.

## Active items
- [ ] AgentRuntime should not know about `notifications`
- [ ] feat: mcp support. perhaps with mcp-porter as a gateway 
- [ ] feat: self-extensibility like Pi
  - Kit knowing how to create themes, change settings, write skills, prompts, etc
- [ ] improvement: serialize or throttle session persistence writes to avoid overlapping save races
- [ ] improvement: migrate session persistence from `.json` to `.jsonl` so appends do not require rewriting the full session on each save
- [ ] new: markdown formatting for thinking text
- [ ] idea: let shell components query plugin state/capabilities via `PluginManager` in the component tree instead of importing global feature state
- [ ] idea: formalize plugin contribution to the status footer
- [ ] idea: explore whether diff/review tools could be enhanced with Ataraxy libs
  - https://github.com/Ataraxy-Labs/sem
  - https://github.com/Ataraxy-Labs/inspect
- [ ] feat: support in-TUI code review by using modals for commenting UX
