# 0067: Place workspace views in Rex panes

## Status

Accepted. Extends the terminal client
([ADR 0035](0035-build-the-terminal-client-with-ard-and-cooper.md)); no
session API change.

## Context

The terminal client shows a session's workspace views (Diff, File, host File,
Subagent conversations, and the Scratchpad) as tabs beside the Agent tab, one
at a time. Users who want two views side by side, or a different arrangement,
would need the client to grow its own split layout.

Terminal multiplexers already arrange panes. [Rex](https://www.superlogical.com/rex/docs)
also exposes an API, through its `rex` command-line tool, to split, focus, and
close panes. That lets a program arrange its own panes. Every workspace view
already reads only state the server owns: the working tree, files,
annotations, the scratchpad, and subagent conversations. A view can therefore
run as its own client of the session without coordinating with the client
that opened it.

Rex is in preview. Its API is versioned by revision, and the command-line tool
ships with each Rex server.

## Decision

### Single-view clients

`kit --session ID --view KIND` runs a terminal client that shows one workspace
view of a session on this home's server. `kit attach SERVER --session ID
--view KIND` does the same on another server:

| `--view` | Shows | Other flags |
| --- | --- | --- |
| `diff` | the Diff view | `--annotation N` reveals an annotation's lines |
| `file` | a file of the session's workspace | `--path` (required), `--line`, `--annotation` |
| `host-file` | a file on the session's server outside its workspace | `--path` (required), `--line` |
| `subagent` | a subagent conversation | `--conversation` (required) |
| `scratchpad` | the session's Scratchpad | |

A single view:

- is an ordinary attachment to the session through the session API. It never
  starts or replaces a server, even this home's own, which `kit` otherwise
  starts.
- has no Agent tab, transcript, composer, interaction dock, or annotation
  chips. Those belong to the client showing the whole session.
- keeps the session's header and footer.
- reports no terminal status, so a multiplexer shows one status per session,
  from the client showing the whole session. It doesn't offer updates either.
- never asks to connect a provider, and never interrupts a run.
- quits on `ctrl+c`.
- outside a multiplexer that places panes, opens any other view it links to as
  a tab of its own, in a strip without an Agent tab. With nothing open, it
  says how to close it.

The view's arguments are navigation input, not identity. A view's identity is
the same as its tab's: the session plus `diff`, the workspace and path, the
host path, the conversation, or `scratchpad`.

### Rex placement

By default, the client opens views in Rex panes when all of these hold. The
client setting `workspacePanes` can be `rex` (the default) or `tabs`, which
keeps views in the client's own tabs even in Rex:

1. `TERM_PROGRAM` is `rex`, and `REX_SESSION`, `REX_BLOCK`, and `REX_BIN_DIR`
   are set.
2. `TMUX`, `STY`, and `ZELLIJ` are unset, because another multiplexer
   between Kit and Rex owns the layout.
3. On the first opening, `$REX_BIN_DIR/rex whoami` reports this client's own
   block and an API revision of at least 12.

Every `rex` invocation passes `--autostart=false`, so Kit never starts a Rex
server, and is bounded to five seconds. Kit uses only the `rex` command-line
tool. It never calls the control WebSocket, client actions, or other hosts
(`-S`).

Each opening of a view runs a single-view client. Its command line is
`/usr/bin/env KIT_HOME=<this client's home> <this executable> --session <id>
--view …` for this home's server. For another server, `attach <server>`
follows the executable, where `<server>` is the saved name, home, URL, or SSH
destination the parent attached to. The explicit home is needed because Rex
starts commands with the login shell's environment, not the caller's.
Credentials never appear on the command line.

- The first view opens to the right of the client's pane, and each later one
  below the last one opened that is still open. Rex focuses the new pane.
- Opening a view that has an open pane focuses that pane. An opening with a
  line or an annotation first tells the running view to show it
  ([ADR 0068](0068-reveal-targets-in-open-workspace-views.md)). If the view
  can't be told, the opening replaces the pane in place: the new pane splits
  the old one, and the old one closes.
- Opening views from inside a single view follows the same rules, using the
  panes that single view opened.
- The client showing the whole session closes the panes it opened when it
  exits. A single view leaves the panes it opened open.
- If placement fails, the client shows one warning, opens that view as a tab,
  and keeps every later view in tabs. Switching sessions abandons an opening
  in progress and closes any pane it made; that isn't a failure.
- If a retargeted pane's old pane can't close, the new one closes instead.

Outside Rex, or with the default setting, the client behaves as before.

### Keyboard and focus

Rex owns moving between panes. Kit owns keys within a pane. Tab cycling
(`ctrl+]` / `ctrl+[`) only moves among tabs open in that client.

## Consequences

- Users arrange workspace views with Rex, and with any other multiplexer by
  running `kit --view` in their own panes, without a split layout in
  the client.
- Views opened in panes keep their own scroll position and selection. Two
  Diff views of different targets can stay side by side.
- A view that can't be told a new target, for example on a server without
  [ADR 0068](0068-reveal-targets-in-open-workspace-views.md)'s operations,
  restarts and loses its own state.
- A preview Rex can change. Kit depends only on documented `rex` commands,
  checks the API revision, and falls back to tabs on any failure.
- Remote Kit servers work unchanged: a view attaches to the same server as
  its parent, by the same saved name, URL, or SSH destination.
