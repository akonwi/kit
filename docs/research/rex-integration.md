# Rex integration: what's possible

Exploration, not a decision. Checked against `rex` 0.1.0-preview.14 (API
revision 12, commit 7e460d0) on the user's server, the public docs at
<https://www.superlogical.com/rex/docs> (`llms-full.txt`, fetched 2026-11-11),
and Kit at 97f0beea.

> [!NOTE]
> The single-view client and Rex placement are implemented on
> `explore/rex-integration` as proposed in
> [ADR 0067](../adrs/0067-place-workspace-views-in-rex-panes.md). They're
> documented in [workspace views in panes](../features/workspace-views.md).
> This page remains the research behind them.

## Summary

- **The lead idea works, and its core is not Rex-specific.** The piece that
  matters is a *single-view client*: `kit attach SERVER --session ID --view
  diff` runs one workspace surface (Diff, File, Subagent, Scratchpad) as an
  ordinary session client. Rex, tmux, Ghostty splits, or a second monitor can
  arrange those processes. Rex adds an API to place them from inside Kit,
  plus Lua for users' own layouts.
- **Every pane except the Agent pane already reads only server state** (diff,
  files, annotations, scratchpad, subagent conversations). The new session
  API is needed only to *retarget* an open view, such as "show line 40" in a
  Diff pane that is already open. Everything else uses existing contracts.
- **Rex mode is opt-in and always falls back.** Outside Rex, or if any Rex
  call fails, the TUI keeps its tab strip unchanged.
- **Other directions:** a Rex plugin for the agent and Rex status is cheap and
  useful. Mac terminal tiles backed by Rex blocks would be valuable, but they
  rest on an undocumented data protocol. A native Kit block type isn't possible
  yet: Rex has an undocumented `--block-dir` block-plugin loader, but no
  published protocol and no third-party creators.
- **Recommendation:** build the single-view client first (useful everywhere),
  then Rex placement behind a setting. Leave Mac tiles and block types until
  Rex documents those surfaces.

**User decisions so far (2026-11-11):**

- Direction: build the single-view client first, then Rex placement.
- Composer: view panes have none. The Agent pane owns the only composer.
- Retargeting: restart the view in place for now, and add client intents if
  this ships.
- ADR: draft one if work goes ahead.

The spike results are under [Spike](#spike-2026-11-11).

## What Rex offers (verified)

| Surface | What it gives Kit | Status |
| --- | --- | --- |
| `rex` CLI (`split`, `run`, `window new`, `focus`, `block close`, `capture`, `events`, `api call`) | Create, place, focus and close terminal blocks running any command; read events | Documented; CLI is the supported automation surface and ships with the server (`$REX_BIN_DIR/rex`) |
| Control API (`session.new_split`, `new_block`+`move_block`, `focus_block`, `zoom_block`, `float_block`/layers, `set_block_label`, `session.view`) | Same, finer-grained; floating layers | WebSocket at `/api/control`; methods self-described (`rex api describe`), wire framing not documented |
| `data.connect` → `/api/data` WebSocket, subprotocol `rex.data.terminal.v1` | Stream a block's terminal content | Ticket documented; the data subprotocol is not |
| Block methods (`terminal.program_status`, `process`, `format`, `write`, `resize`, `set_theme`) and events (`program_status_changed`, `child_exited`, `desktop_notification`, …) | Watch other panes' status, exit, output | Documented |
| Program Status Protocol (OSC 7501) | Kit → Rex tab/session indicators | Open spec; Kit's TUI already reports it ([terminal-turn-status.md](../terminal-turn-status.md)) |
| `init.lua`: `rex.action`, `rex.bind`, `rex.on`, `rex.session.*` | User-defined layouts and keys that launch Kit views | Documented; user config, Kit must not edit it |
| Client actions (`-C CLIENT do …`) | Drive Rex.app UI | Need Remote Control (off by default). Kit shouldn't depend on them |
| Hosts (`-S HOST`, Tailscale) | Run blocks on another machine | Documented |
| Block creators | `block_creator.list` shows only `com.superlogical.terminal` and `.onboarding` | `rex server run --block-dir DIR` "discovers executable block plugins" (`internal/blockplugin` in the binary), but the protocol is undocumented, nothing in the docs mentions it, and it needs server flags that Rex.app manages |
| "Unified priority inbox" | Status from non-terminal apps | **Planned** in the Program Status page ("in the future… a unified priority inbox view"). There's no API today |

Rex's environment inside a block: `TERM_PROGRAM=rex`, `REX_SESSION`,
`REX_BLOCK`, `REX_SERVER` (unix socket URL), `REX_BIN_DIR`. A command started
by `rex split` runs through the **login shell's environment, not the caller's**,
so `KIT_HOME` and other variables don't carry over. There's no `--env` flag.

## 1. TUI tabs as Rex panes

### Shape

```text
 Rex window "kit · <session name>"
┌──────────────────────────────┬─────────────────────────┐
│ kit (Agent)                  │ kit attach … --view diff│
│ transcript + composer        │ Diff                    │
│ owns OSC 7501 status         ├─────────────────────────┤
│ places sibling views via rex │ kit attach … --view     │
│                              │   file:internal/x.go:40 │
└──────────────────────────────┴─────────────────────────┘
          │  each pane is an independent session client  │
          └──────────────► Kit server (authoritative) ◄───┘
```

Each pane is its own `kit` process and its own attachment to one explicit
session. Panes never talk to each other directly. Shared state (annotations,
scratchpad, diff, subagent roster) already lives on the server, and Rex holds
the layout.

### Entry point

The single-view client extends `kit attach`, which already never starts,
stops, or replaces a server:

```sh
kit attach <server> --session <id> --view diff
kit attach <server> --session <id> --view file --path internal/x.go --line 40
kit attach <server> --session <id> --view host-file --path ~/notes.md
kit attach <server> --session <id> --view subagent --conversation <id>
kit attach <server> --session <id> --view scratchpad
```

- `<server>` is what the parent connected to: a Kit home path for the local
  server, or a saved server name. Credentials resolve from files as they do
  today. **Tokens never go in argv.**
- The parent launches the view with its own absolute executable path
  (`os.Executable`) and, if it has one, an explicit `KIT_HOME`, because Rex
  doesn't pass the environment through. That keeps dev and channel builds
  pointed at their own homes:
  `rex split --shell=none -- /usr/bin/env KIT_HOME=… /abs/kit attach …`.
- A view renders one surface edge to edge. It has its own small header (the
  scope the tab label used to carry) and no tab strip. Whether it has a
  composer is a design decision (see the questions below).
- Views don't report OSC 7501. Only the Agent pane owns the `app=kit` record,
  so Rex shows one status per Kit session, not one per pane.
- In the TUI, `shell.open_tab` is the single choke point for opening any tab.
  Rex mode replaces its body with "place or focus a view" and keeps the
  in-process path as the fallback.

```text
fn open_tab(tab):
  match placement:
    in_process        => workspace.add(tab); show_tab(tab.id)      // today
    rex(ctx)          => match rex.place(ctx, tab):
                           ok(block)  => remember(tab.id, block)
                           err(_)     => workspace.add(tab); show_tab(tab.id)  // fall back, toast once
```

### Placement and identity

- **The Agent process places views** for openings that start in the Agent
  pane: tool rows, the palette, mentions, chips, and the subagent picker. Its
  map from `tab.id` to Rex block lives in the process, like today's
  `workspace::Tabs`. The first view splits right of the Agent block. Later
  views split below the most recent view, so they stack in one column. Users
  rearrange the layout with Rex.
- **Dedupe:** if the mapped block still exists (checked with `session.view`),
  focus it with `rex focus`. If not, place a new one. Block labels show the
  tab label (`Diff`, `x.go`, the agent's name). They're for display, not
  identity, because users can rename them.
- **Openings from inside a view**, such as the Diff pane's link to a File tab,
  use the same placement helper in that process. Without the Agent's map,
  they dedupe by label within the Kit window. A mismatch only produces a
  second pane, which is harmless.
- **Retargeting an open view** ("reveal line 40", a chip that reveals diff
  lines) is the one cross-process message. There are two options:
  1. *Restart in place.* Close the block and create a replacement in the same
     split. This needs no API, but it loses the view's scroll position.
  2. *Client intents through the server.* This adds a session API event
     addressed to attachments, like `open-url` in
     [ADR 0049](../adrs/0049-let-plugins-ask-attached-clients-to-open-web-pages.md).
     A view would subscribe to intents for its own view identity.
  Option 1 is enough for a first version. Option 2 is the version to keep if
  this ships.

### Focus and keyboard

- **Rex owns movement between panes:** its keymap, which uses `cmd` keys on
  the Mac, plus mouse focus. **Cooper owns keys inside a pane.** Neither
  needs to change.
- Kit's `ctrl+]` / `ctrl+[` tab cycling and the tabs picker call
  `rex focus <block>` for Kit's own blocks in Rex mode, so muscle memory still
  works.
- Today, Diff and File are mouse-only and leave keyboard focus in the
  composer. In a separate pane, a click moves Rex focus there and keys go to
  that view. Views therefore need a minimal keymap: scroll, `q` to close, and
  annotate.
- New views open with `--focus=false` when an opening is incidental, such as
  a subagent starting (the design language says these never steal focus).
  They take focus when the user explicitly opened them.

Restarting in place works without flicker or layout drift, and the spike
confirmed it:

1. Create the replacement block detached with `session.new_block`. Its
   process starts while it is out of view.
2. Place it beside the old block with `session.move_block` (API only:
   `rex move` and `rex swap` can't target a detached block).
3. Close the old block. The replacement takes the old block's space, and
   focus doesn't move.

### Closing and reopening

- Closing a view pane in Rex ends its process. Views hold no client state
  except the Scratchpad's unsaved draft. It flushes on unmount today, and it
  would also need to flush on SIGHUP/SIGTERM. The signal path is unverified.
- When the Agent pane exits normally, it closes the blocks it created
  (`rex block close`). It never closes blocks it didn't create. After a
  crash, views keep working as standalone clients and exit when the session
  is deleted or the user closes them.
- Views aren't restored when Kit restarts, matching today's rule that "tabs
  are client-local and never persisted". If Rex restores a session after its
  server restarts, a view that re-runs re-attaches without trouble. Whether
  Rex re-runs commands on restore is unverified.

### Detecting Rex (and staying correct outside it)

Rex mode is active only when all of these hold. Otherwise the TUI behaves
exactly as it does today in Ghostty, tmux, or a plain terminal:

1. The setting is on (for example `tui.workspace_panes = "rex"`, default
   `tabs`).
2. `TERM_PROGRAM=rex`, `REX_SESSION`, and `REX_BLOCK` are set.
3. `$REX_BIN_DIR/rex whoami --json --autostart=false` succeeds, reports
   `block_id == $REX_BLOCK`, and reports `api_revision >= 12`, the lowest
   revision Kit has tested.

Check 3 catches the cases where environment variables lie. If they were
forwarded over `ssh`, or Kit is running inside tmux inside Rex, the socket
isn't there or the block doesn't match. **`--autostart=false` is required**,
because the CLI otherwise starts a Rex server on a machine that doesn't have
one. Inside tmux running in Rex, Cooper already disables OSC 7501, and Rex
mode should also stay off, because the user is using tmux's layout.

### Remote hosts

- Views always run **where the Agent TUI runs**, on the Rex server named by
  `REX_SERVER`. Kit never uses `-S`. A view therefore reaches the Kit server
  the same way its parent did: a local home, or a saved or paired server.
- If the Kit server is remote (`kit attach devbox`), views also attach to
  `devbox` by saved name. That needs nothing new.
- A Rex host on another machine, such as a TUI started by `rex -S devbox
  run kit`, is just "Rex on devbox", and the same rules apply there.

### Cost

| Piece | Size | Risk |
| --- | --- | --- |
| `--view` single-view client (CLI flags, a `shell` variant that mounts one surface, view header and keymap) | Medium. The surfaces already exist as components with services from `attached_session` | Low |
| Rex placement module (detect, place, focus, close, using CLI `--json` and `--autostart=false`) | Small to medium | Medium: preview API |
| Retarget through restart-in-place | Small | Low |
| Client intents (session API, OpenAPI, Go/Swift/TS clients) | Medium | Contract change, so it needs an ADR |
| Design-language update for composer-less views | Small | Needs a decision |

## 2. Other directions

| Direction | Feasibility | Cost | Notes |
| --- | --- | --- | --- |
| **Rex plugin for the agent** (typed tools: split, run, capture, wait, focus own blocks; watches `program_status_changed`/`child_exited` on blocks it started and submits a session message ([ADR 0041](../adrs/0041-let-plugins-submit-session-messages.md)) when they finish or block) | High | Low–medium | Keeps Rex out of Kit core: plugins run on the server host, which is where Rex is when the server is local. Typed tools can enforce "only touch blocks this session created". Today's CLI plus skill already works, so the gain is safety and notifications without polling. |
| **Built-in Rex tools** in Kit core | High | Medium | Not recommended. Built-ins run on the Kit server, which may not be the machine where the user's Rex runs, and they would put a preview dependency in core. |
| **Kit → Rex status from the Mac app** | Low today | — | Rex has no API to post status without a terminal, and the inbox is planned. Revisit when Rex ships it, probably as a plugin that uses session status ([ADR 0066](../adrs/0066-share-session-status-with-plugins.md)). A hidden terminal block that prints OSC 7501 would work, but it's a hack. |
| **Rex → Kit status** (show Rex panes' program status in Kit) | High | Low as a plugin footer item | `rex events --json` with `program_status_changed`. Useful for "tests in Rex: blocked on password". Its scope is the same Rex session as the agent's blocks. Showing every Rex session would conflict with the design rule against ambient multiplexing chrome. |
| **Mac terminal tiles backed by Rex blocks** | Medium–low | High | Gains: terminals survive app quits, become visible to the agent, and remote hosts remove ADR 0044's "only with a local daemon" limit. But the tile needs the undocumented `rex.data.terminal.v1` stream and input over `block.event`/`write`, and libghostty would have to be driven by external I/O instead of its own pty. Wait for Rex to document data connections, or ask Superlogical. |
| **Native Kit block type in Rex** | Not yet | — | The server can load executable block plugins (`--block-dir`), but the protocol isn't published, nothing ships with one, and enabling it requires server flags that Rex.app manages. Even then, Rex.app would need to know how to draw a non-terminal block. Ask Superlogical whether third-party block creators are planned. |
| **Rex Lua or keys that open Kit views** | High once `--view` exists | Docs only | A user's `init.lua` action can split and run `kit attach … --view diff`. The open question is how Lua learns the Kit session in the focused block. The cheapest answer is a client-local file the TUI writes, `$KIT_HOME/run/rex/<block-id>` → session ID, which an action reads with `io.open`. Kit documents recipes and never edits `init.lua`. |

## 3. Constraints and risks

- **API stability.** Rex is a preview: there's no stable channel yet, and the
  API has reached revision 12. Use the **CLI with `--json`** rather than the
  WebSocket control protocol, because it's the documented automation surface
  and ships with the server (`$REX_BIN_DIR/rex`), so the versions match.
  Require `api_revision >= 12`, feature-detect methods with `rex api list -q`,
  and treat every failure as "fall back to tabs". Kit tests would use a fake
  `rex` executable, with no Rex in CI.
- **Licensing and distribution.** Rex is proprietary and in beta, with no
  license checks or telemetry. Kit doesn't bundle, link, or redistribute it.
  It only runs the user's installed CLI, which is fine. OSC 7501 is an open
  spec. Any future block plugin or API binding needs Superlogical's terms
  checked first.
- **Server authority.** Unchanged. Views are ordinary attachments that use
  the session API. The Kit server never talks to Rex in the TUI design, and in
  the plugin design only a plugin does.
- **No process-global active-session state.** Each view gets an explicit
  `--session`. The optional Lua lookup file is keyed by Rex block, written by
  the client that owns that block, and removed on exit. It doesn't record a
  "current session".
- **Clients use the session API.** Views use existing contracts. Only client
  intents would extend them, following `docs/api-development.md`.
- **Design language.**
  - It already endorses "compose Kit clients with terminal tabs, panes, tmux".
    Rex mode is that, automated.
  - It conflicts with "the composer remains fixed beneath every selected tab"
    and "the tab owns the pane title". In Rex mode the Rex pane plays the
    role of the tab, so views need a pane-owned header and an explicit rule
    about whether they have a composer.
  - Annotation chips appear above the Agent composer only.
  - The root boundary stays viewport-native in each pane.

## What an ADR would decide

If the user goes ahead, the ADR (number to be confirmed; 0067 is next) would
decide:

1. The single-view client: `kit attach … --view KIND`, the view kinds and
   identities, and the rule that views never start servers or report
   program status.
2. Placement as a TUI client concern: the `in_process | rex` modes, the
   setting, the detection rules, the fallback, and that Kit never uses `-S`
   or client actions.
3. Ownership: the Agent process creates and closes only its own blocks, and
   views exit on their own.
4. Retargeting: restart-in-place now, or client intents (a session API
   change) now.
5. The design-language amendment for views shown outside the tab strip: their
   header, composer policy, and keymap.
6. Rex compatibility policy: the minimum API revision, CLI-only integration,
   and no Rex in CI.

## Spike (2026-11-11)

The spike used a throwaway build of this worktree bound to a `rexspike`
channel, with an isolated `KIT_HOME` under `/tmp` and its own Kit server. It
ran in a labelled Rex session, `kit-rex-spike`, with the worktree as the cwd.
The hack was about 20 lines in `tui/shell.ard`: an environment variable
opened the Diff tab on attach and left out the tab strip, transcript,
pending row, composer and dock. Everything the spike created has been
removed, and the hack is reverted.

| Question | Result |
| --- | --- |
| Can a workspace surface run alone as its own client? | **Yes.** `diff_pane` needed no changes, only a different composition in `shell`. The view attached to the Agent's session with `kit attach <home> --session <id>` and showed the working tree with the header and footer. |
| Do separate processes stay in sync? | **Yes.** A file added to the worktree appeared in the view within about 5 s through the server's working-tree observation, with no Agent involvement. |
| Does placement work from outside the TUI? | **Yes.** `rex split --label Diff --focus=false --shell=none -- /usr/bin/env KIT_HOME=… /abs/kit attach …` placed the view beside the Agent without moving focus (about 50 ms per call). |
| Is the caller's environment inherited? | **No.** A split started with a variable set in the caller's environment saw neither it nor `KIT_HOME`. Inside the pane, Rex sets `TERM_PROGRAM=rex`, `REX_SESSION`, `REX_BLOCK`, `REX_SERVER` and `REX_BIN_DIR`. |
| Retarget by restarting in place | Works with `new_block` + `move_block` + close (see above). `rex swap`/`rex move` reject detached blocks, so this needs `rex api call`. |
| Closing | `ctrl+c` in the view exits the process. Rex closes the pane, and the sibling takes its space. |
| Status | Only the Agent pane should report OSC 7501. The Agent's `app=kit` root record showed up in `program_status`. The hacked view didn't suppress its own report; the real version must. |

The spike didn't test mouse interaction inside a view pane, how Rex.app
sizes panes (nothing displayed the session, so panes stayed 80×24),
whether Rex restores a session's views after it restarts, or remote hosts.
The isolated server had no credentials, and creating a session needs an
available provider, so a dummy API key stood in for one. No turn ran.

## Open questions for Superlogical

- Are third-party block creators (`--block-dir`) planned to be public, and
  can Rex.app draw non-terminal blocks?
- Will `rex.data.terminal.v1` be documented for embedders?
- When will the inbox, or another API for posting status without a terminal,
  arrive?
- Does session restore re-run a block's command?
