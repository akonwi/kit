# Kit v2 UI direction

## Status

Active direction. Accepted rules move into `.agents/skills/design/SKILL.md` as
they are decided; unresolved topics remain explicit explorations here.
Implementation progress is tracked in `docs/parity.md`.

## Research baseline

The reference implementation is the installed Kit `v0.34.0` and local
`main@a2c7434`. It was inspected through live PTY captures at 140×42 and through
main-branch source and feature documentation. Captured states included:

- first-run/empty transcript;
- active and completed turns;
- command palette and filtered command palette;
- settings and login dialogs;
- wide transcript plus Code Review workspace.

Primary source references:

- `app/src/shell/AppShell.tsx`
- `app/src/shell/HeaderBar.tsx`
- `app/src/shell/BottomStatusBar.tsx`
- `app/src/shell/ComposerDock.tsx`
- `app/src/shell/transcript/`
- `app/src/shell/WorkspacePaneHost.tsx`
- `app/src/shell/Dialog.tsx`
- `app/src/shell/Picker.tsx`
- `app/src/shell/themes/`
- `app/src/keymap/`
- `app/docs/features/`

## Direction workshop 1

The first review established these constraints:

- use a viewport-native root shell; the terminal viewport is Kit's outer
  boundary and the shell does not draw a complete enclosing frame;
- keep multi-session multiplexing architectural rather than ambient TUI UX;
  users compose one-session Kit clients through terminal tabs, panes, or tmux;
- preserve main's subagent oversight through a Subagents roster pane and
  retained per-subagent activity tabs;
- keep one universal command palette rather than adding a separate noun
  switcher or shortcut;
- rank palette results in one list instead of grouping them visibly;
- preserve the main-branch shell chrome roles: session name top-left, model
  settings, context percentage, and conditional release/update contributions
  top-right, transient status bottom-left, and working-directory/Git context
  bottom-right;
- do not add an elapsed-turn timer; replace the main-branch context progress bar
  with compact percentage text in the model-information cluster;
- make first-run and authentication the first implementation-grade mockup.

The dedicated first-run/auth exploration is
[`v2-auth-flow.md`](./v2-auth-flow.md).

## Accepted root shell boundary

Kit v2 is viewport-native:

```text
 kit                                      model · thinking
──────────────────────────────────────────────────────────

 transcript and workspace

──────────────────────────────────────────────────────────
 ▌ composer
──────────────────────────────────────────────────────────
 cwd · branch                                      status
```

The terminal or multiplexer pane supplies the outer boundary. Kit paints the
full viewport and uses full-width internal separators, but does not draw outer
corner glyphs, top/bottom rules, or left/right border cells. Dialogs, focused
controls, and meaningful local regions may still be framed.

This choice gives content two more columns and rows, avoids drawing a second box
inside terminal splits, and reduces border-junction and resize artifacts. Root
screens expose header/content/footer slots so local components never recreate
the removed frame.

## Current product shape

```text
┌─ header ────────────────────────────────────────────────────────┐
│ session name                              model · thinking      │
├─ primary transcript ───────────────┬─ retained workspace ───────┤
│ conversation                       │ pane tabs                  │
│ streaming prose                    │ review / scratch / files   │
│ compressed tool activity           │ activity / subagents       │
├─ pending state / attachments ──────┴────────────────────────────┤
│ composer                                                        │
├─ status ────────────────────────────────────────────────────────┤
│ mode / queue                                      cwd / VCS     │
└─────────────────────────────────────────────────────────────────┘
```

The application has a strong transcript-first center. Work details progressively
disclose through compact transcript rows and retained workspace panes. Dialogs,
transient pickers, full-screen reading surfaces, and workspace panes have
separate interaction roles. A layered keymap gives the active surface ownership
of input.

## Preserve from v0.34

1. **Transcript-first continuity.** Prose stays primary; tool detail is available
   without dominating the conversation.
2. **Semantic terminal-derived themes.** Preserve semantic roles, light/dark
   safety, syntax roles, diff roles, and user overrides rather than a fixed Kit
   palette.
3. **Renderer-neutral pane identity.** Workspace descriptors remain plain data;
   each renderer owns presentation.
4. **Retained workspace state.** Pane selection, drafts, scroll, and editor state
   survive tab and responsive-layout changes.
5. **Responsive split-to-tabs behavior.** Narrow terminals retain the same
   information architecture rather than falling back to unrelated dialogs.
6. **Derived keyboard hints.** The keymap/intent registry remains the only source
   for visible shortcuts.
7. **Progressive disclosure.** Tool calls, long output, skipped diff sections,
   and secondary navigation begin compact.
8. **Safe draft semantics.** Review and file feedback remain revision-scoped and
   fail closed when stale.
9. **Quiet empty states.** Primary empties may use the Kit wordmark; local list
   empties remain terse and contextual.
10. **Subagent workspace model.** A session-level roster pane discovers
    subagents; opening one creates or activates its retained workspace tab.

## Friction to address

- Detach safety for the attached session is not communicated clearly: users
  should understand whether its active turn survives closing the client.
- Subagent status and activity must stay discoverable without leaking into
  global cross-session chrome.
- The command palette mixes commands, prompt content, plugins, and navigation in
  one alphabetical list.
- Fixed-height dialogs leave large empty regions for narrow result sets.
- Clipped descriptions and separators can resemble rendering errors; truncation
  does not always communicate omission.
- The collapsed-workspace `<` handle has little meaning without learned context.
- Model configuration such as `medium` is shown without a label.
- Important status competes for header/footer width and can disappear into
  anonymous overflow.
- Keyboard meanings vary heavily by focused surface, while some transcript
  affordances are easier to discover with a mouse than a keyboard.
- Review is capable but cognitively dense: target, tree/patch mode, draft scope,
  range selection, and attachment lifecycle overlap.

## Candidate v2 principles

### 1. The daemon enables continuity without demanding attention

Each TUI presents one attached session. The UI clearly communicates whether
that session's turn is running, queued, detached, completed, or interrupted,
while terminal tabs and multiplexers compose multiple clients naturally.

### 2. Subagent oversight belongs to the session workspace

A Subagents pane presents the attached session's roster. Opening an agent from
the roster or transcript creates or activates one retained activity tab for
that agent; subagent state does not become global session chrome.

### 3. The transcript is the narrative; Activity is the evidence

The transcript explains what happened. High-frequency events, raw tool updates,
and diagnostics belong in Activity unless they are necessary to understand the
conversation.

### 4. One palette is the universal find-and-act surface

`Ctrl+P` finds actions, files, panes, prompts, plugins, and on-demand explorers
such as Sessions and Subagents in one ranked list. Result metadata explains
type and scope without splitting the list into permanent visual groups.

### 5. Density follows activity

Idle screens are calm. Running screens surface actionable queue, retry,
compaction, bash-mode, and completion state in the established status regions.
Context usage appears as compact percentage text with the model settings.
Subagent telemetry appears in transcript activity and workspace panes. There is
no elapsed-turn timer; chrome appears because it changes the next action, not
because a slot exists.

### 6. Surfaces have one owner

The shell owns global chrome and split layout. Workspace hosts own tabs and
edges. Pane bodies own only their local context and actions. Borders communicate
ownership or focus, never decoration.

### 7. Text must be honest

Truncated content uses an ellipsis. Overflow is labeled. Metadata is named when
its meaning is ambiguous. Separators span their actual boundary.

### 8. Keyboard and pointer interaction are peers

Every pointer action has a discoverable keyboard path and visible focus state.
The active surface owns input; hidden retained surfaces do no work and consume no
input.

### 9. Wide and narrow layouts share one information architecture

A split becomes tabs or a drawer at named thresholds. State and terminology do
not change merely because the viewport does.

### 10. Interruption semantics are explicit

Abort, detach, clear, cancel, queue, and stop-daemon are distinct words and
states. Destructive scope is always visible before activation.

## Information architecture

```text
Kit daemon                                  architectural, not TUI navigation
└── concurrent sessions
    └── one or more terminal-composed clients

TUI client
├── one attached session
│   ├── transcript + composer
│   ├── run state and queue
│   ├── workspace panes
│   │   ├── activity
│   │   ├── code review
│   │   ├── scratchpad
│   │   ├── files
│   │   ├── MCP / releases / diagrams
│   │   ├── Subagents roster
│   │   └── retained subagent activity tabs
│   └── subagent lineage + mailbox
└── on-demand surfaces
    ├── universal ranked palette
    ├── session explorer
    └── settings / auth
```

There is no global session strip, session rail, cross-session running count, or
TUI-native multiplexer. Existing sessions remain reachable through the
on-demand session explorer, while users compose simultaneous session views with
their terminal's native workflow.

## Surface taxonomy

| Surface | Purpose | Presentation |
| --- | --- | --- |
| Persistent chrome | Session name top-left; model/thinking/context and update contributions top-right; transient status bottom-left; cwd/Git bottom-right; composer | Fixed rows with established ownership |
| Workspace surface | Transcript and retained task context | Wide split; narrow tabs/drawer |
| Transient overlay | Universal palette, contextual pickers, toasts, context menus | No heavy frame; content-hugging within bounds |
| Dialog | Settings, login, guided questions, destructive confirmation | Centered modal with bounded content |
| Takeover | Pager, fatal errors, migration/recovery | Full viewport with fixed header/footer |

## Visual grammar

- Use semantic theme tokens; do not encode hue into component APIs.
- Default spacing unit is one terminal cell. Add blank rows only to separate
  conceptual groups, not every control.
- Use one structural border per ownership boundary. Avoid nested full frames.
- Primary content uses normal weight and terminal foreground. Muted metadata is
  subordinate but remains legible.
- Status glyphs have stable meanings: filled circle running/dirty, empty circle
  idle/clean, flag subagent, envelope mailbox, check success, slash cancel.
- Focused rows use background inversion or a muted fill rather than an extra
  border.
- Context usage is a bare percentage in the top-right model cluster (`41%`),
  using semantic progress color thresholds. The header separator is structural,
  not a progress bar. Omit context at zero or when unavailable.
- Do not add an elapsed-turn timer.
- Overlays hug their result count until reaching a named maximum height. The
  command palette keeps its input near the top quarter so filtering changes only
  its bottom edge instead of moving the whole surface.
- Header/footer overflow says what was hidden (`⋯ 3 more`) instead of showing an
  unexplained glyph.
- Animation is limited to meaningful progress, entry/exit, and state change;
  motion never substitutes for a status label.

## Navigation model to test

```text
Ctrl+P       universal palette: actions, explorers, files, panes, prompts
Tab          next visible workspace surface
Shift+Tab    previous visible workspace surface
Escape       cancel the innermost reversible interaction
```

Additional pane bindings remain open. Every action remains rebindable and
visible hints come from the active intent registry.

## Mockup scenarios

The companion `v2-shell-exploration.html` compares the installed baseline with
candidate v2 states:

1. first attach to one session;
2. active streaming run with queue and transcript subagent activity;
3. Subagents roster and retained per-agent activity tab;
4. narrow terminal;
5. single ranked, content-hugging command palette.

## Decisions to make before implementation

1. How should one ranked palette balance relevance, recency, exact matching,
   type, and scope without visible result groups?
2. Which additional pane bindings, if any, deserve defaults?
3. Which v0.34 interactions are parity requirements versus deliberate v2
   simplifications?
