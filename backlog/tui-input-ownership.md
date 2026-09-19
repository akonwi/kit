# TUI input ownership completion proposal

Owner: `TUI-SHELL-002` in [the TUI ledger](tui.md).

## Goal

The visible active layer must also own keyboard input, focus, dismissal, and
paste. Preserve existing widgets and feature controllers; do not build a generic
arbitrary-depth overlay framework.

## Findings

- Shell overlay assembly, root key capture, Escape dismissal, and Ctrl+C each
  use separate ordering/eligibility rules.
- `openPalette` guards several modals but omits workspace file/tab pickers,
  the subagent picker, and pending interactions. Shell registers Ctrl+P even
  when another layer traps focus.
- An interaction dock can reclaim focus while a modal above it owns root key
  capture. Asynchronous interaction arrival does not reconcile all surfaces.
- Annotation-picker command handling can interpret pasted j/k/r as actions.
- Tests cover individual surfaces more thoroughly than their transitions and
  interactions with competing layers.

## Proposed ownership policy

Resolve one active keyboard owner from current client state:

1. Explicit child dialog of an open modal, such as explorer rename/delete.
2. Root modal, including the command palette, authentication, and pickers.
3. Pending interaction dock.
4. Composer transient picker, when its editor owns focus.
5. Focused selected workspace content or composer.

Toasts are visual feedback, not keyboard owners. Copy acts on eligible selected
content without changing ownership. The interaction dock remains non-modal for
transcript selection and mouse scrolling.

Only one independent root modal may open at a time. Support existing explicit
nested flows. Selecting a palette command continues to replace the palette
with its destination; it does not create a new arbitrary modal stack.

On asynchronous interaction arrival, close transient composer suggestions and
preserve drafts. If a modal is already open, keep it active and defer dock focus
until it closes. Do not mount competing reclaiming focus scopes. With an active
dock, ordinary global commands cannot open unrelated modals.

## Implementation shape

- Add a small TUI-local layer identifier and pure ownership resolver, consumed
  by rendering, root pre-frame key routing, shortcut registration, dismissal,
  and focus eligibility. Keep feature data in existing controllers.
- Centralize modal admission/replacement and explicit child transitions. Replace
  scattered lists of disallowed booleans with the resolver's eligibility rules.
- Route ordinary keys to the resolved owner only. Unhandled owner input may
  reach that owner's focused widget, never a lower layer's command handler.
- Handle Vaxis focus intents rather than rebinding Tab beneath app shortcuts.
  Register only eligible actions, since ignored actions can mask outer handlers.
- Capture logical return focus when opening a layer. Restore the parent modal,
  otherwise pending dock, otherwise the valid prior control/pane, with composer
  fallback. Scope restoration to the current session and pane incarnation.
- Paste is text insertion only. Non-text owners consume/ignore it. Associate
  bracketed paste with its originating owner/generation so asynchronous changes
  cannot redirect it into another control or interpret it as commands.

## Key policy proposed for approval

- Escape cancels only the innermost reversible action. Pending non-cancellable
  work consumes it rather than falling through to a lower layer or aborting a run.
- Ctrl+P and Ctrl+O are unavailable under a modal or interaction dock. Preserve
  explicitly local uses of these keys, such as configuration-picker Ctrl+O.
- Tab/Shift+Tab cycle inside the active modal/dock; otherwise preserve the
  workspace/composer contract. Workspace-tab shortcuts are blocked under traps.
- Make Ctrl+C consistently quit/detach the client, preserving server-owned work
  and interaction requests. This is a proposed behavior change: several current
  dialogs treat it as local cancellation. Keep cancellation on Escape and align
  all hints/tests with the chosen policy before shipping.

## Verification and completion

- Table-driven resolver tests for each owner and supported nesting transition.
- Render/input tests proving the visually top layer owns text, arrows, Enter,
  Escape, Tab, Shift+Tab, and pointer activation.
- Reachable overlap cases: file/tab/subagent picker plus Ctrl+P; interaction dock
  plus Ctrl+P; interaction arriving during palette, editor, and confirmation.
- Immediate input before the next rendered frame; focus restoration after close,
  pending completion, owner removal, session switch, and pane closure.
- Bracketed paste containing navigation/action characters and escape sequences;
  owner changes during paste; no lower-layer submission or mutation.
- Preserve dock background selection/scrolling, toast behavior, and inactive-pane
  input isolation. Verify the decided Ctrl+C policy in every owner.
- Remove superseded routing/guard lists. Complete `TUI-SHELL-002` only when these
  rules have one authority and the transition tests pass.

Validation: repository Go formatting, build, vet, full tests, and TUI race tests;
manual keyboard/mouse checks in a terminal for focus restoration and paste.

## Implementation checkpoint

The shell-owned surfaces now share an ownership resolver for admission,
rendering, keyboard routing, dismissal, and focus eligibility. Palette commands
replace their source modal even when an interaction arrives underneath it.
Ctrl+C detaches directly, including standalone session-picker child dialogs.
Bracketed paste is tied to its session, owner incarnation, controller, and pane;
actual insertion establishes paste provenance. The composer stays mounted while
the dock replaces its visible slot, preserving its draft and cursor.

Mounted tests cover competing shortcuts, configuration child cancellation,
same-owner paste replacement, discarded-paste provenance, modal/dock/composer
focus transitions, composer mouse interaction, and standalone child Ctrl+C.
Logical pane return addresses are session/workspace/incarnation checked, with
composer fallback. Stale rendered targets cannot receive keyboard/paste input
intended for a newly active shell owner.

`TUI-SHELL-002` remains in progress. Remaining completion work:

- Integrate pane-local dialogs, notably the diff target picker, with shell
  admission and ownership. It currently retains its independent pane-local
  scope; do not claim all modal paths are unified yet.
- Restore the exact prior eligible pane control (including inline editors and
  Agent content), rather than only a secondary-pane/composer region. Do not
  silently weaken the approved control-restoration requirement.
- Finish the pointer/hidden-pane transition matrix, including dock background
  selection followed by immediate keyboard/paste input and pane-local dialogs.
- Run manual terminal checks for focus, bracketed paste, and mouse behavior.
