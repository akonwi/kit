# OpenTUI upgrade plan

## Goal

Upgrade this repo from:

- `@opentui/core` `0.1.102`
- `@opentui/solid` `0.1.102`

to the current latest published versions:

- `@opentui/core` `0.2.2`
- `@opentui/solid` `0.2.2`

while minimizing regressions in shell layout, keyboard handling, colors/theme behavior, diff rendering, and app shutdown/startup stability.

## Why this matters

Notable OpenTUI changes since `0.1.102` include:

- theme mode reliability improvements
- color intent / palette changes
- Windows truecolor fixes
- tmux OSC 10/11 behavior improvements
- keymap work/refinement
- FFI/backend/platform boundary changes
- lifecycle hardening such as guarding palette publication after destroy

These are relevant to Kit because it relies on:

- Solid reconciler APIs
- keyboard-heavy shell interaction
- dynamic transcript/composer layout
- markdown and diff rendering
- terminal color correctness
- clean renderer lifecycle during quit/restart flows

## Upgrade strategy

Treat this as a **minor upgrade with behavior risk**, not a trivial patch bump.

Do the work on a dedicated branch and validate interactively after each stage.

## Proposed steps

### 1. Prepare and capture current baseline

Before bumping packages:

- record current versions in `package.json` / `bun.lock`
- note current known-good behaviors:
  - composer focus and submit
  - pending attachment rows above composer
  - transcript scrolling
  - markdown rendering
  - diff rendering
  - `/diff` interaction
  - `/code-review` browser flow
  - quit/restart behavior
- if possible, capture screenshots or short notes for:
  - normal transcript view
  - composer with attachments
  - diff view
  - modal/overlay usage

### 2. Read upstream release notes for the relevant range

Specifically review:

- `v0.1.103` through `v0.1.107`
- `v0.2.0`
- `v0.2.1`
- `v0.2.2`

Pay special attention to changes involving:

- keymap behavior
- focus management
- palette / color intent
- OSC theme/color handling
- renderer lifecycle
- FFI / platform loading
- diff / markdown rendering behavior

### 3. Bump dependencies

Update:

- `@opentui/core` → `0.2.2`
- `@opentui/solid` → `0.2.2`

Then refresh lockfile.

### 4. Fix compile-time or API breakage

Run:

- `bun run typecheck`
- `bun run check`

Address any API-level breakage first, especially around:

- component props
- keyboard/focus behavior
- preload/runtime setup
- theme/color usage

### 5. Interactive smoke-test pass

Run the app and verify:

#### Composer / input
- textarea focus on startup
- Enter submits
- Shift+Enter inserts newline
- Ctrl+C behavior unchanged
- queued follow-up behavior unchanged
- attachment rows remain above composer

#### Transcript
- transcript scroll still sticks correctly at bottom
- user/assistant/tool transcript entries render correctly
- compact code review entries still render distinctly
- image attachments still open correctly from transcript

#### Diff / rendering
- `/diff` still renders syntax-highlighted diff correctly
- line numbers still align
- markdown still renders correctly
- code blocks still highlight properly

#### Overlays / modal behavior
- palette opens/closes correctly
- any custom overlays still capture focus properly
- background shell remains locked/unlocked appropriately

#### Terminal compatibility
At minimum test in the terminal(s) you care about most:

- Ghostty
- tmux session, if supported/used
- macOS Terminal / iTerm / other actively used terminal, if relevant

Check:

- foreground/background colors
- cursor visibility
- scrollbar/selection oddities
- OSC palette/theme behavior

#### Lifecycle
- start app
- quit app
- reopen app
- verify no broken renderer state, stale palette events, or shutdown artifacts

### 6. Regression-focused review areas

If problems appear, investigate these first:

#### A. Color/theme regressions
Possible causes:

- palette intent changes
- OSC handling updates
- theme mode reliability changes

Look for regressions in:

- `src/shell/theme.ts`
- transcript colors
- border colors
- diff colors

#### B. Keyboard/focus regressions
Possible causes:

- keymap changes in upstream OpenTUI
- focus behavior changes in Solid reconciler

Look for regressions in:

- `src/shell/ComposerDock.tsx`
- overlay/palette focus flows
- any focusable boxes / textarea components

#### C. Layout regressions
Possible causes:

- subtle Yoga/layout behavior changes
- scrollbox sizing changes

Look for regressions in:

- `src/shell/AppShell.tsx`
- `src/shell/ComposerDock.tsx`
- `src/shell/TranscriptPane.tsx`
- diff/transcript scroll regions

#### D. Runtime/platform regressions
Possible causes:

- FFI/backend/platform-layer changes
- destroy/shutdown behavior changes

Look for regressions in:

- startup on local machine
- quit/restart cycles
- crashes or terminal cleanup failures

### 7. Decide whether to ship directly or stage follow-up fixes

If the upgrade is clean:

- commit dependency bump + any necessary compatibility fixes together

If the upgrade surfaces multiple unrelated UI regressions:

- commit the minimal compatibility bump
- capture any non-blocking regressions as separate backlog items

## Recommended validation checklist

Before asking to merge:

- `bun run typecheck`
- `bun run check`
- manual smoke test in primary terminal
- manual smoke test in tmux if relevant
- verify `/diff`
- verify `/code-review`
- verify transcript interactions
- verify quit/restart behavior

## Suggested commit structure

If possible, separate into:

1. dependency bump
2. compatibility fixes required by the bump
3. optional polish/follow-up cleanups

This will make regressions easier to isolate.

## Risks

### Medium risk
- keyboard/keymap behavior differences
- color/theme changes
- diff rendering or markdown rendering differences
- focus changes in nested overlays or inputs

### Lower but important risk
- shutdown lifecycle / renderer cleanup
- platform-specific package loading behavior

## Nice-to-have follow-up

If the upgrade is successful, consider adding:

- a short compatibility note under `docs/`
- lightweight manual regression checklist for core shell flows
- snapshot or interaction coverage for especially fragile shell components if/when test infrastructure grows
