# Backlog

This file is the source index for the backlog.

Keep this list short and current. If an item needs more detail, link to a dedicated file in this directory.

## Conventions

- `[ ]` not started
- `[x]` done

Delete done items.

## Active items
- [ ] feat: sub-agents (borrow from pi-subagents)
  - discovery, runtime, persistence, and compact transcript replay are in place
  - see `backlog/subagents.md`
- [ ] feat: explicit `read_thread` tool for agent to react to thread references
- [ ] ux: lean into command palette instead of slash picker
  - keep picker UX for file and thread references
- [ ] idea: explore useful Unicode text glyphs for TUI affordances and status labels
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
- [ ] review: explore an alternative split-diff view for in-TUI review
  - see `backlog/review-split-diff-view.md`
- [ ] feat: setting for "zen" mode. minimal transcript
  - don't show tool calls in transcript
- [ ] refactor: extract `AgentRuntime` core so main and sub-agent runtimes can share execution machinery
  - see `backlog/agent-runtime-extraction-for-subagents.md`
