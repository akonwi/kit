# Workspace views in panes

The terminal client shows a session's Diff, File, Subagent, and Scratchpad
views as tabs beside the Agent tab. Each of them can also run on its own, as a
separate client of the same session, so a terminal multiplexer can arrange
them side by side ([ADR 0067](../adrs/0067-place-workspace-views-in-rex-panes.md)).

## One view per client

`kit --view` shows one view of a session:

```sh
kit --session 3f2a --view diff
kit --session 3f2a --view file --path internal/server/server.go --line 120
kit --session 3f2a --view host-file --path ~/notes/todo.md
kit --session 3f2a --view subagent --conversation conversation_…
kit --session 3f2a --view scratchpad
kit attach devbox --session 3f2a --view diff    # a session on another server
```

`--session` is required. `--annotation N` makes a Diff or File view reveal an
annotation, as clicking its chip does.

A single view shows the session's header and footer around the view, with no
transcript or composer. Annotations made in it reach the session's other
clients at once. Links to other views open as tabs in the view, or in panes
under Rex (below). Press `ctrl+c` to close it.

A single view never starts a server, never reports the session's status to
the terminal, and never interrupts a run. Those belong to the client showing
the whole session.

Any multiplexer can arrange single views: run one in a tmux pane or a Ghostty
split next to `kit`.

## In Rex

When `kit` runs directly in a [Rex](https://www.superlogical.com/rex/docs)
terminal, opening a view opens it in a Rex pane:

- The first view opens to the right of Kit, and later views stack below it.
- Opening a view that is already open focuses its pane. Opening it at a line,
  or at an annotation, shows that target in the running view, which keeps
  the rest of its state
  ([ADR 0068](../adrs/0068-reveal-targets-in-open-workspace-views.md)). On
  an older Kit server, the view restarts in the same place instead.
- Closing a pane closes that view. Quitting Kit closes the panes it opened.
- Rex moves focus between panes, and Kit's keys work inside each one.

Kit uses the `rex` tool that ships with the Rex server, from `REX_BIN_DIR`. It
never starts a Rex server and needs Rex API revision 12 or later. If a pane
can't be opened, Kit shows one warning and keeps that view, and later ones, as
tabs. Inside tmux running in Rex, and in other terminals, views stay tabs.

To keep views in Kit's tab strip in Rex too, turn off **Open views in Rex
panes** under `/settings` › This terminal. The row appears only when Kit runs
in Rex, and the change applies to the next view opened: panes already open
stay open. It saves `workspacePanes` in `$KIT_HOME/settings.json`, which you
can also set by hand:

```json
{ "workspacePanes": "tabs" }
```

The default is `"rex"`.
