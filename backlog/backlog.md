# Backlog

This file is the source index for the backlog.

Keep this list short and current. If an item needs more detail, link to a dedicated file in this directory.

## Conventions

- `[ ]` not started
- `[x]` done

Delete done items.

## Active items
- [ ] fix: /reload doesn't pick up theme files
- [ ] idea: let shell components query plugin state/capabilities via `PluginManager` in the component tree instead of importing global feature state
  - see `backlog/plugin-chrome-and-capabilities.md`
- [ ] idea: formalize plugin contribution to the status footer
  - see `backlog/plugin-chrome-and-capabilities.md`
- [ ] idea: explore whether diff/review tools could be enhanced with Ataraxy libs
  - https://github.com/Ataraxy-Labs/sem
  - https://github.com/Ataraxy-Labs/inspect
- [ ] review: make saved gutter comment markers respect segmented same-side ranges
  - see `backlog/review-gutter-comment-markers.md`
- [ ] review: make overlapping saved gutter comment markers distinguishable
  - see `backlog/review-gutter-comment-markers.md`
- [ ] feat: setting for "zen" mode. minimal transcript
  - don't show tool calls in transcript
- [ ] refactor: extract `AgentRuntime` core so main and sub-agent runtimes can share execution machinery
  - see `backlog/agent-runtime-extraction-for-subagents.md`
