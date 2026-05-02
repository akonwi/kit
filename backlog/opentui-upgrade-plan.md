# OpenTUI upgrade plan

## Goal

Upgrade this repo from:

- `@opentui/core` `0.1.102`
- `@opentui/solid` `0.1.102`

to:

- `@opentui/core` `0.2.2`
- `@opentui/solid` `0.2.2`

while minimizing regressions in shell layout, keyboard handling, colors/theme behavior, diff rendering, and app shutdown/startup stability.

---

## Working status

- [ ] Baseline captured
- [ ] Upstream release notes reviewed
- [x] Dependencies bumped
- [x] Compile-time/API breakage fixed
- [ ] Runtime boot checks passed
- [ ] Interactive smoke tests passed
- [ ] Terminal compatibility checks passed
- [ ] Lifecycle checks passed
- [ ] Follow-up regressions triaged
- [ ] Ready to merge

---

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

---

## Upgrade strategy

Treat this as a **minor upgrade with behavior risk**, not a trivial patch bump.

Work on a dedicated branch if possible. Keep commits logically separated, but only if each commit remains buildable and reasonably testable.

---

## Repo-specific hotspot files

These files deserve extra scrutiny during the upgrade:

### Runtime boot / renderer lifecycle
- `src/app/bootstrap.tsx`
  - `createCliRenderer`
  - `render`
  - `getTreeSitterClient`
  - preload/runtime assumptions
  - renderer destroy / quit flow

### Input / focus / keyboard behavior
- `src/shell/MessageComposer.tsx`
  - `textarea` props
  - `keyBindings`
  - `onPaste`
  - ref behavior
- `src/shell/ComposerDock.tsx`
- modal and overlay components:
  - `src/shell/Modal.tsx`
  - `src/shell/InlinePicker.tsx`
  - `src/features/login/LoginModal.tsx`
  - `src/features/sessions/SessionExplorerModal.tsx`
  - `src/features/guided-questions/GuidedQuestionsContent.tsx`
  - `src/features/settings/SettingsContent.tsx`

### Layout / scrolling
- `src/shell/transcript/pane.tsx`
  - `scrollbox`
  - `stickyStart="bottom"`
  - `stickyScroll`
- `src/shell/AppShell.tsx`
- `src/shell/ScreenLayout.tsx`
- `src/shell/ScreenHeader.tsx`
- `src/features/pager/PagerContent.tsx`
- `src/features/review/ReviewContent.tsx`

### Theme / color behavior
- `src/shell/theme.ts`
- transcript and diff rendering surfaces
- headers, borders, muted text, progress colors

### Asset / package layout assumptions
- `src/app/bootstrap.tsx`
  - `bun --preload=@opentui/solid/preload`
  - `node_modules/@opentui/core/assets`
  - tree-sitter worker + wasm asset lookup

---

## Stage 1 — Prepare and capture baseline

### Versions / dependency state
- [x] Confirm current versions in `package.json`
- [x] Capture relevant `bun.lock` state
- [x] Note any local patches or existing OpenTUI workarounds

Notes:
- Removed stale `@ts-ignore` workarounds for `onPaste` now that the upgraded OpenTUI typings cover it.

### Known-good behavior notes
- [ ] Composer focus on startup works
- [ ] Enter submits
- [ ] Shift+Enter inserts newline
- [ ] Ctrl+C behavior is unchanged from expected current behavior
- [ ] Pending attachment rows appear above composer
- [ ] Transcript scrolling behaves correctly at bottom
- [ ] Tool/live transcript rows render correctly
- [ ] Markdown rendering looks correct
- [ ] Diff rendering looks correct
- [ ] `/diff` interaction works
- [ ] `/code-review` browser flow works
- [ ] Quit/restart behavior works

### Useful baseline captures
- [ ] Normal transcript view noted or captured
- [ ] Composer with attachments noted or captured
- [ ] Diff view noted or captured
- [ ] Modal / overlay usage noted or captured
- [ ] Login/auth flow noted or captured
- [ ] Session explorer noted or captured
- [ ] Pager noted or captured

---

## Stage 2 — Review upstream changes

Review the relevant OpenTUI release notes/changelog entries before changing code.

### Versions to review
- [ ] `v0.1.103`
- [ ] `v0.1.104`
- [ ] `v0.1.105`
- [ ] `v0.1.106`
- [ ] `v0.1.107`
- [ ] `v0.2.0`
- [ ] `v0.2.1`
- [ ] `v0.2.2`

### Topics to watch for
- [ ] keymap behavior
- [ ] focus management
- [ ] palette / color intent
- [ ] OSC theme/color handling
- [ ] renderer lifecycle
- [ ] FFI / platform loading
- [ ] asset layout changes
- [ ] preload entrypoint changes
- [ ] diff / markdown rendering behavior

### Summarize upgrade-relevant findings
- [ ] Record any likely code changes needed in this file
- [ ] Record any likely manual test focus areas in this file

---

## Stage 3 — Bump dependencies

### Package updates
- [x] Update `@opentui/core` to `0.2.2`
- [x] Update `@opentui/solid` to `0.2.2`
- [x] Refresh lockfile

### Immediate verification
- [x] Dependency install/lock refresh completes cleanly
- [x] No obvious missing package/preload issues after install

---

## Stage 4 — Fix compile-time and API breakage

### Required checks
- [x] `bun run typecheck`
- [x] `bun run check`

### Areas to fix first if broken
- [ ] component prop API changes
- [ ] `textarea` prop or ref behavior changes
- [ ] keyboard binding API changes
- [ ] focus behavior changes
- [ ] renderer/bootstrap API changes
- [ ] theme/color API changes
- [ ] preload/runtime setup changes
- [ ] tree-sitter asset lookup changes

### Exit criteria
- [x] Repo typechecks cleanly
- [x] Repo formatting/lint checks pass cleanly

---

## Stage 5 — Runtime boot checks

These checks matter even if TypeScript passes.

### Dev/start boot
- [x] `bun run dev` boots successfully
- [x] `bun run start` boots successfully
- [x] App renders without immediate runtime crash

### Preload / asset assumptions
- [x] `@opentui/solid/preload` still resolves correctly
- [x] tree-sitter worker path still works
- [x] core asset path assumptions still work
- [ ] syntax/highlight assets still load

Notes:
- `@opentui/solid/preload` now resolves via the package `exports` map (`./scripts/preload.ts`), not a literal top-level `preload.js` file.

### Quit path
- [x] app can quit cleanly after boot
- [ ] no broken terminal cleanup after quit

---

## Stage 6 — Interactive smoke tests

### Composer / input
- [ ] textarea focus on startup
- [ ] Enter submits
- [ ] Shift+Enter inserts newline
- [ ] paste handling still works
- [ ] queued follow-up behavior unchanged
- [ ] attachment rows remain above composer
- [ ] composer mode states still render correctly

### Transcript
- [ ] transcript scroll still sticks correctly at bottom
- [ ] user messages render correctly
- [ ] assistant messages render correctly
- [ ] tool transcript entries render correctly
- [ ] live tool updates render correctly
- [ ] compact code review entries still render distinctly
- [ ] image attachments still open correctly from transcript

### Diff / rendering
- [ ] `/diff` renders syntax-highlighted diff correctly
- [ ] line numbers align correctly
- [ ] markdown renders correctly
- [ ] code blocks still highlight properly
- [ ] review/diff content layout still looks right

### Overlays / modal behavior
- [ ] palette opens/closes correctly
- [ ] login modal behaves correctly
- [ ] session explorer behaves correctly
- [ ] guided questions behaves correctly
- [ ] settings screen/modal behaves correctly
- [ ] background shell locks/unlocks correctly when overlays are active
- [ ] focus returns correctly after closing overlays

### Pager / full-screen flows
- [ ] pager opens correctly
- [ ] pager layout still fits correctly
- [ ] pager keyboard handling still works
- [ ] full-screen review flows still work

---

## Stage 7 — Terminal compatibility checks

At minimum, test in the terminals that matter most.

### Primary terminals
- [ ] Ghostty
- [ ] tmux session, if relevant
- [ ] macOS Terminal / iTerm / other actively used terminal, if relevant

### Things to verify
- [ ] foreground/background colors look correct
- [ ] cursor visibility looks correct
- [ ] selection behavior is acceptable
- [ ] scrollbar behavior looks correct
- [ ] OSC palette/theme behavior looks correct
- [ ] no obvious rendering corruption or flicker

---

## Stage 8 — Lifecycle checks

### Restart / cleanup behavior
- [ ] start app
- [ ] quit app
- [ ] reopen app
- [ ] repeat quit/reopen at least once

### Things to verify
- [ ] no stale renderer state
- [ ] no stale palette events
- [ ] no shutdown artifacts
- [ ] no crash on destroy/exit
- [ ] terminal state is restored correctly on exit

---

## Stage 9 — Regression triage

If problems appear, classify them here.

### Color / theme regressions
- [ ] Reviewed `src/shell/theme.ts`
- [ ] Checked transcript colors
- [ ] Checked border colors
- [ ] Checked diff colors
- [ ] Captured any follow-up work needed

### Keyboard / focus regressions
- [ ] Checked composer focus
- [ ] Checked overlay focus flows
- [ ] Checked keymap behavior changes
- [ ] Captured any follow-up work needed

### Layout / scrolling regressions
- [ ] Checked transcript scroll regions
- [ ] Checked composer layout
- [ ] Checked pager/review layout
- [ ] Captured any follow-up work needed

### Runtime / platform regressions
- [ ] Checked startup behavior
- [ ] Checked quit/restart cycles
- [ ] Checked platform-specific issues
- [ ] Captured any follow-up work needed

---

## Recommended validation before merge

### Automated
- [ ] `bun run typecheck`
- [ ] `bun run check`

### Manual
- [ ] smoke test in primary terminal
- [ ] smoke test in tmux if relevant
- [ ] verify `/diff`
- [ ] verify `/code-review`
- [ ] verify transcript interactions
- [ ] verify quit/restart behavior

---

## Suggested commit structure

Use this only if each step remains green enough to be useful.

- [ ] Commit 1: dependency bump
- [ ] Commit 2: compatibility fixes required by the bump
- [ ] Commit 3: optional polish / follow-up cleanup

If a bump-only commit is not viable, prefer fewer but coherent commits over a broken split.

---

## Risks

### Medium risk
- keyboard/keymap behavior differences
- color/theme changes
- diff rendering or markdown rendering differences
- focus changes in nested overlays or inputs
- scroll/layout behavior differences

### Lower but important risk
- shutdown lifecycle / renderer cleanup
- platform-specific package loading behavior
- preload or asset path changes

---

## Nice-to-have follow-up

If the upgrade is successful:

- [ ] add a short compatibility note under `docs/`
- [ ] keep a lightweight manual regression checklist for core shell flows
- [ ] add test coverage for fragile shell interactions if/when test infrastructure grows
