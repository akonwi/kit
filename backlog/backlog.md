# Backlog

This file is the source index for the backlog.

Keep this list short and current. If an item needs more detail, link to a dedicated file in this directory.

## Conventions

- `[ ]` not started
- `[x]` done

Delete done items.

## Active items
- [ ] improvement: serialize or throttle session persistence writes to avoid overlapping save races
- [ ] enhancement: indicate bash mode in composer when using `!` and `!!`
- [ ] enhancement: better onboarding. the empty state of transcript can mention /login if unauthenticated
- [ ] enhancement: auto-paging should try to calculate the minimum threshold based on viewport height
- [ ] idea: let shell components query plugin state/capabilities via `PluginManager` in the component tree instead of importing global feature state
- [ ] idea: formalize plugin contribution to the status footer
- [ ] idea: explore whether diff/review tools could be enhanced with Ataraxy libs
  - https://github.com/Ataraxy-Labs/sem
  - https://github.com/Ataraxy-Labs/inspect
- [ ] wip: session explorer graph view — see `backlog/session-explorer-graph-view.md`
- [ ] [wip] fix: session auto-naming isn't working
  - added a toast and error throwing to help identify where/why it fails
  - error seen in a toast is
  ```
  Session auto-name failed: 400 Error from provider: 3 request validation errors: Input should be 'low', 'medium', 'high' or 'none', field: 'reasoning_effort.literal['low','medium','high','none']', value: 'minimal'; Input should be a valid integer, unable to parse string as an integer, field: 'reasoning_effort.int', value: 'minimal'; Input should be a valid boolean, unable to interpret input, field: 'reasoning_effort.bool', value: 'minimal'
  ```
- [ ] code review web app doesn't seem to refresh and reflect current diffs
- [ ] wip: transcript rendering from events — see `backlog/transcript-rendering-events.md`
- [ ] fix: don't ignore .agents directories by default
- [ ] enhancement: don't need to show number of lines in tool call results. that's not useful
- [ ] style: replace emojis with better (terminal native, text?) glyphs. more like the indicator for the code-review server status
- [ ] AgentRuntime should not know about `notifications`
