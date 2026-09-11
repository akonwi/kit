# Kit v2 first-run and authentication flow

## Status

Exploration selected for the first implementation-grade vaxis mockup. The root
shell is now viewport-native. The state model, copy, and interaction rules are
candidates for acceptance. Cross-session presence is intentionally absent from
the TUI.

## Goals

- Reach a focused composer in at most two user decisions: choose a provider,
  then approve or enter its credential.
- Never leave the user wondering what Kit is waiting for or how to exit.
- Keep credential ownership honest: Kit stores credentials locally and the
  persistent daemon uses them.
- Keep first-run geometry independent of optional workspace regions.
- Keep idle chrome calm. Only actionable authentication timing is shown.

## Root states

| State | Trigger | Presentation |
| --- | --- | --- |
| Probing | Client discovers/starts daemon and reads auth state | Wordmark plus quiet status; spinner only after a short delay |
| Gate | No valid provider credential | Header, centered connection prompt, hint footer; no composer |
| Provider selection | User activates the gate | Main-branch dialog and picker composition over the unchanged gate |
| Provider interaction | OAuth/device/API-key work is active | Same dialog frame, with method-specific content |
| Ready, new | Credential saved and no session was selected or resumed | Empty transcript plus focused composer |
| Ready, resumed | Credential saved and startup resolved a requested or current-directory session | Restored transcript plus focused composer |
| Degraded gate | Credential exists but no model is usable | Gate with persistent warning and corrective actions |

First-run content is centered in the measured content region, not blindly in the
terminal viewport. This keeps it correct when workspace layout changes the
available region.

## Probing presentation

For the first 300 ms, render the stable wordmark and `Starting Kit…` without an
animated indicator. After 300 ms, add the standard spinner and update the line
to the active phase, such as `Starting local daemon…` or `Checking provider
credentials…`. A failure becomes a persistent root state with `r retry`, `d
diagnostics`, and `ctrl+c quit`; it never falls through to an unauthenticated
gate that implies the daemon is healthy.

## Authentication state machine

```text
PROBING
  credential + model available ───────────────────────────────► READY
  no usable credential ───────────────────────────────────────► GATE
  daemon start/discovery fails ────────────────────────────────► START_FAILURE

GATE
  Enter/connect ──────────────────────────────────────────────► SELECT_PROVIDER
  Ctrl+C ─────────────────────────────────────────────────────► QUIT

SELECT_PROVIDER
  choose device OAuth ────────────────────────────────────────► DEVICE_BEGIN
  choose browser OAuth ───────────────────────────────────────► BROWSER_BEGIN
  choose API key ─────────────────────────────────────────────► KEY_ENTRY
  Escape ─────────────────────────────────────────────────────► GATE

DEVICE_BEGIN
  instructions received ──────────────────────────────────────► DEVICE_WAIT
  error ──────────────────────────────────────────────────────► SELECT_PROVIDER + ERROR

DEVICE_WAIT
  approved ───────────────────────────────────────────────────► SAVING
  declined / expired ─────────────────────────────────────────► SELECT_PROVIDER + ERROR
  Escape ─────────────────────────────────────────────────────► CANCEL_POLL ► SELECT_PROVIDER

BROWSER_BEGIN
  authorization URL ready ────────────────────────────────────► BROWSER_WAIT
  error ──────────────────────────────────────────────────────► SELECT_PROVIDER + ERROR

BROWSER_WAIT
  callback approved ──────────────────────────────────────────► SAVING
  declined / timed out ───────────────────────────────────────► SELECT_PROVIDER + ERROR
  Escape ─────────────────────────────────────────────────────► CANCEL_CALLBACK ► SELECT_PROVIDER

KEY_ENTRY
  submit ─────────────────────────────────────────────────────► SAVING
  rejection ──────────────────────────────────────────────────► KEY_ENTRY + ERROR
  Escape ─────────────────────────────────────────────────────► SELECT_PROVIDER

SAVING
  generation-safe save + model available ────────────────────► READY + SUCCESS
  auth changed in another Kit process ────────────────────────► SELECT_PROVIDER + CONFLICT
  saved but no usable model ──────────────────────────────────► DEGRADED_GATE

DEGRADED_GATE
  connect another provider ───────────────────────────────────► SELECT_PROVIDER
  re-check availability ──────────────────────────────────────► PROBING
  Ctrl+C ─────────────────────────────────────────────────────► QUIT

START_FAILURE
  retry ──────────────────────────────────────────────────────► PROBING
  open diagnostics ───────────────────────────────────────────► DIAGNOSTICS
  Ctrl+C ─────────────────────────────────────────────────────► QUIT

DIAGNOSTICS
  retry ──────────────────────────────────────────────────────► PROBING
  Escape ─────────────────────────────────────────────────────► START_FAILURE
```

The current Go implementation already supplies the Codex device-flow states:
`OpenAICodexDeviceInstructions`, cancellation-aware polling, and guarded atomic
replacement through `AcquireSave`.

## Root-shell decision

Kit v2 uses a viewport-native root shell. The screen contract remains:

```text
Shell
├── header slot
├── content slot
└── footer slot
```

### Rejected alternative: framed canvas

```text
┌──────────────────────────────────────────────────────────────────┐
│ kit                                                              │
│──────────────────────────────────────────────────────────────────│
│                             k i t                                │
│                          ━━━━━━━━━━━                             │
│              Connect an AI provider to get started.              │
│                                                                  │
│                       ↵ connect a provider                       │
│──────────────────────────────────────────────────────────────────│
│ ↵ connect · ctrl+c quit        ~/Developer/agent/kit-v2 · kit-v2 │
└──────────────────────────────────────────────────────────────────┘
```

This preserves continuity with v0.34 and creates strong containment, but spends
two columns and rows, duplicates terminal/multiplexer pane boundaries, and
creates more corner, resize, and border-junction artifacts.

### Accepted: viewport-native

```text
 kit
──────────────────────────────────────────────────────────────────

                             k i t
                          ━━━━━━━━━━━
              Connect an AI provider to get started.

                       ↵ connect a provider

──────────────────────────────────────────────────────────────────
 ↵ connect · ctrl+c quit        ~/Developer/agent/kit-v2 · kit-v2
```

The terminal viewport supplies the outer boundary. Kit gains usable space and
reduces border-composition failures; product identity comes from the wordmark,
semantic theme, spacing rhythm, and internal separators. The shell must paint
the full viewport background and every structural separator must span its real
boundary.

The selected wrapper must render the same measured content at 46 columns. The
viewport-native narrow variant is:

```text
 kit
──────────────────────────────────────────────

                    k i t
                 ━━━━━━━━━━━
      Connect an AI provider to get started.

            ↵ connect a provider

──────────────────────────────────────────────
 ↵ connect · ctrl+c quit    …/kit-v2 · kit-v2
```

## Provider selection

The vaxis client preserves the main-branch `Dialog.Root` plus `Picker`
composition rather than inventing a separate auth card. At wide sizes the
surface uses 70% of the available width, bounded to 48–96 columns:

```text
        ┌──────────────────────────────────────────────────────────┐
        │ Connect a provider                                        │
        │                                                          │
        │ Filter providers                                         │
        │ >                                                        │
        │                                                          │
        │ OpenAI Codex                  ChatGPT plan · device code  │
        │ Anthropic                     API key                     │
        │ OpenAI                        API key                     │
        │ Claude                        Pro or Max plan · browser   │
        │                                                          │
        │ ↑ up · ↓ down · Enter select · Esc close                 │
        └──────────────────────────────────────────────────────────┘
```

The list is derived from the providers currently supported by Droids: OpenAI
Codex device login, Anthropic API key, OpenAI API key, and Claude Pro/Max
browser OAuth. The focused picker row uses the full-width picker selection background and foreground; provider
name and method share one row. The header contains only
the task title—an option count merely repeats what the list already shows. The
filter remains visible, and the body owns bounded height and scrolling. The
dialog uses the shell background so its border cells do not reveal a conflicting
surface tint. The surrounding shell is not dimmed or recolored. A trapped focus
scope, not a visual scrim, establishes modality.

The single universal palette decision does not change this dialog: provider
selection is one bounded step inside an active task, not global navigation.

## Codex device-code wait

The host and code below are illustrative placeholders. The UI always renders
the exact runtime `VerificationURI` and `UserCode` returned by
`OpenAICodexDeviceInstructions`.

Wide:

```text
        ┌──────────────────────────────────────────────────────────┐
        │ Complete login                              OpenAI Codex │
        │                                                          │
        │ Open the link and enter the code to complete auth.       │
        │                                                          │
        │ Open this URL                                            │
        │ https://auth.openai.com/codex/device                      │
        │                                                          │
        │ Enter this code                                          │
        │ FJKP-QRSA                                                │
        │                                                          │
        │ ⠹ Waiting for approval — expires in 12:41                 │
        │                                                          │
        ├──────────────────────────────────────────────────────────┤
        │ C copy code · Esc cancel                                 │
        └──────────────────────────────────────────────────────────┘
```

Narrow:

```text
  ┌──────────────────────────────────────────┐
  │ Complete login              OpenAI Codex │
  │                                          │
  │ Open the link and enter the code to      │
  │ complete authentication.                 │
  │                                          │
  │ Open this URL                            │
  │ https://auth.openai.com/codex/device     │
  │                                          │
  │ Enter this code                          │
  │ FJKP-QRSA                                │
  │                                          │
  │ ⠹ Waiting — expires in 12:41             │
  │                                          │
  ├──────────────────────────────────────────┤
  │ C copy code · Esc cancel                 │
  └──────────────────────────────────────────┘
```

The URL remains visible and carries OSC 8 hyperlink metadata. Because the TUI
owns mouse reporting, activating the rendered link also invokes the platform
browser opener directly. The URL and code are separated with labels and spacing
rather than another border nested inside the dialog. The action footer
retains its top border so keyboard controls remain a distinct dialog region. The
code is emphasized without inserting display characters that would corrupt
copy/paste.
The wait line mounts Kit's shared spinner widget, which uses the standard
Braille frames at an 80 ms cadence; Codex auth does not own separate animation
state. The countdown is shown because expiry is actionable. Below two minutes it changes
to warning semantics and says what to do next.

## Ready state

```text
 Unnamed session                                        GPT-5.6 Sol
────────────────────────────────────────────────────────────────────
 ✓ Connected to OpenAI Codex — using GPT-5.6 Sol          [toast]

                              k i t
                           ━━━━━━━━━━━
                  Ask a question or give a task.

                     ctrl+p to open the palette

────────────────────────────────────────────────────────────────────
 ▌ Ask kit to do something…
────────────────────────────────────────────────────────────────────
                                  ~/Developer/agent/kit-v2 · kit-v2
```

At 46 columns, the ready state preserves the same focus and hierarchy:

```text
 Unnamed session                  GPT-5.6 Sol
──────────────────────────────────────────────
 ✓ Connected to OpenAI Codex          [toast]

                    k i t
                 ━━━━━━━━━━━
       Ask a question or give a task.

          ctrl+p to open the palette

──────────────────────────────────────────────
 ▌ Ask kit to do something…
──────────────────────────────────────────────
                            …/kit-v2 · kit-v2
```

No elapsed-turn timer, cross-session run count, or child status appears in shell
chrome. Context percentage belongs to the model-information cluster and is
omitted here because a new empty session has no meaningful usage. Startup
either opens the requested session, resumes the current
directory's selected/latest session according to CLI policy, or starts a new
one. Other sessions remain available on demand through the session explorer;
they are not promoted into first-run chrome.

## Candidate copy

| Context | Copy |
| --- | --- |
| Gate | `Connect an AI provider to get started.` |
| Gate action | `Connect a provider` |
| Provider dialog title | `Connect a provider` |
| Provider filter | `Filter providers` |
| Login dialog title | `Complete login` |
| Device wait | `Waiting for approval — expires in {m:ss}` |
| Expiring code | `Code expires in {m:ss} — esc to get a new code` |
| Declined | `Approval was declined. Choose a provider to try again.` |
| Expired | `The code expired before approval. Choose a provider to get a new one.` |
| Concurrent change | `Sign-in changed in another Kit process. Choose a provider to retry.` |
| Cancelled | `Sign-in cancelled.` |
| Success | `Connected to {provider} — using {model}` |
| No model | `Connected to {provider}, but no model is available. Check plan access, or connect another provider.` |

Errors name the failed actor or state and end with the next available action.
Credential values are never echoed. Product/provider names are used verbatim.

## Focus and accessibility

- The gate is the initial focus target. Opening provider selection creates a
  modal focus scope.
- Escape cancels the innermost reversible step: polling, method entry, provider
  selection. Escape does not quit from the gate; Ctrl+C does.
- Successful auth moves focus to the composer and shows its cursor.
- Cancelling polling cancels the underlying context; no orphaned work remains.
- Spinner plus text communicate waiting. Color is never the sole status cue.
- The device code remains selectable and has a keyboard copy command.
- API-key input is masked; paste works; validation never reports key length or
  content.
- Visible hints are generated from the active intent registry.
- URLs remain visible even when an open-browser action exists, preserve SSH and
  headless use, carry OSC 8 hyperlink metadata when safe, and explicitly handle
  activation while TUI mouse reporting is enabled.
- Terminal title remains idle during authentication; the visible gate/dialog
  copy names the exact action. The `?` marker is reserved for feedback requested
  by an active agent turn.
- Wide and narrow snapshot tests reject clipped glyphs, unlabeled truncation,
  incomplete separators, and overflow outside the measured content region.

## Session scope

The auth and first-run shell presents one attached session. It has no global
session strip, rail, or cross-session telemetry. Terminal tabs and multiplexers
compose simultaneous Kit clients; the on-demand session explorer remains
available for explicit switching.

Subagent monitoring is session-local and belongs to the workspace. It does not
alter auth-gate geometry or introduce global presence chrome.

## Acceptance checks for the first vaxis mockup

- The viewport-native gate renders at 46 and 140 columns without an enclosing
  border or incomplete separator.
- Provider and device dialogs use the main-branch frame, preserve the undimmed
  shell behind them, and stay within the viewport.
- Instruction-render failure aborts before polling or saving.
- Poll cancellation restores provider-selection focus and leaves no goroutine.
- Device expiry and denial transition to actionable inline messages.
- Save conflict uses the concurrent-change copy and does not replace credentials.
- Success resolves the model and focuses the composer.
- Degraded success remains visible until corrected; it is not a transient toast.
- No state renders cross-session presence or multiplexing chrome.
