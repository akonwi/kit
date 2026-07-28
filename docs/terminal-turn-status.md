# Terminal turn status

Kit signals agent-turn state at three terminal integration layers:

1. **Running title** — while a turn is active, the normal session title is prefixed with `⏳`. This keeps the state visible in terminal tab chrome.
2. **Surface progress** — in Ghostty, Kit emits the OSC 9;4 indeterminate progress report while a turn runs and removes it when the turn completes. Ghostty renders this as an animated bar at the top of the terminal surface.
3. **Completion attention** — the notifications feature continues to emit BEL and a terminal-mediated notification when a turn completes. With Ghostty's default bell features, an unfocused surface gains a persistent `🔔` title marker and requests application attention until the user returns.

The running signals are driven directly by the runtime rather than a reloadable plugin so cwd-driven plugin reloads cannot leave them stale. Disposal always restores the idle title and removes terminal progress.

OSC 9;4 output is currently gated to terminals identified as Ghostty through `TERM_PROGRAM` or `TERM`, and is disabled inside tmux or screen until explicit passthrough wrapping is supported. The title and completion signals remain terminal-independent.
