# Workspace tab ceiling recovery

Tracked by `TUI-WORK-004` in the [native TUI backlog](tui.md).

Opening a new workspace pane at the 32-secondary-tab ceiling is currently
rejected with an actionable warning. The user can open the existing pane picker
through the `tabs` command, close a tab, and retry the original action.

Defer a more direct recovery flow until there is a clear interaction design for
the rejected pane request. A future design should decide whether reaching the
ceiling should:

- open the pane picker directly from the warning;
- retain the rejected descriptor while the picker is open;
- automatically open it after the user explicitly closes an eligible tab; or
- keep the picker as navigation/management only and require a manual retry.

Any implementation must preserve the existing guarantees: never evict a tab
silently, keep Agent non-closable, leave tab order and selection unchanged when
the open is rejected, and avoid retaining a stale pending descriptor across a
session switch or workspace-incarnation change.
