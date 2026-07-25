# Shell and persistent workspace plan

Status: implementation in progress; phases 1–4 complete.

Reference mockup: [`mockups/code-review-pane.html`](./mockups/code-review-pane.html)

## Goal

Evolve Kit's main shell from a transcript with occasional overlays and sidebars into a workspace with:

- the transcript as the primary surface
- a persistent secondary pane that can host code review or the scratchpad
- a draggable divider on wide terminals
- a tabbed transcript/review presentation on narrow terminals
- fixed, predictable shell chrome that continues to support plugin contributions

Kit continues to own layout, focus, resizing, responsive behavior, and overflow presentation. Plugins contribute bounded app-owned status content rather than arbitrary shell UI.

## Shell structure

```text
┌ session ───────────────────── model · thinking · plugin header items ┐
├━━━━ context usage ━━━━━━━━━─────────┬─────────────────────────────────┤
│ Transcript                          │ Code review                     │
│                                     │                                 │
│ Composer                            │ Review hints                    │
├ queued/bash guidance ───────────────┴ repo · plugin footer items ─────┤
```

### Header

The header is a fixed single row and does not wrap.

- Left: session name
- Right: model, thinking level, then plugin header contributions
- Built-in items are privileged and retain guaranteed space
- Plugin items use the remaining width

A fixed row prevents plugin registration from shifting the entire shell and keeps toast positioning stable.

### Transcript context progress

Context usage belongs to the transcript, not the global shell or review pane.

- Preserve the existing progress-bar visualization and blue/amber/red thresholds
- Render it on the transcript's top boundary
- Limit it to the transcript column when the workspace is split
- Recalculate it when the divider or terminal resizes
- Hide it when the Review tab is selected in narrow mode
- Do not duplicate the percentage in the footer

### Footer

The footer has left and right regions only. There is no center region and no generic runtime-state label.

The left region appears only for existing actionable guidance:

- bash mode
- bash-excluded mode
- queued-message guidance

The right region contains:

- the privileged VCS location contribution
- standard plugin footer contributions

When no left guidance is present, the right region receives the available space.

## Contribution overflow

Header and footer contributions never wrap and are never partially clipped.

When standard plugin contributions do not fit:

1. Keep privileged built-ins visible.
2. Pack complete plugin contributions in registration order.
3. Replace omitted contributions with an overflow indicator such as `… +3`.
4. Clicking the indicator opens a small app-owned status picker.
5. The picker lists all contributions and preserves their styling.
6. Activating an actionable item invokes its existing plugin click handler.

Automatic scrolling or marquee behavior is explicitly rejected because persistent movement is distracting, makes statuses harder to read, and makes clickable content unstable.

The existing public APIs remain compatible:

```ts
kit.header.set(...)
kit.footer.set(...)
```

Plugins continue to provide text, styled segments, side, and an optional click action. Kit owns measurement, packing, overflow, and expansion. Contribution priority does not need to become public yet.

### Privileged built-ins

Kit-owned contributions use an internal privileged classification with guaranteed space. This is not initially exposed through the public plugin API.

The VCS location remains a privileged internal contribution rather than becoming a hardcoded shell component. Existing built-in hide identifiers and behavior remain supported.

## Secondary workspace

The workspace has one app-owned secondary pane. Code review and the scratchpad can occupy it, with only one secondary surface active at a time. Opening one replaces the other without introducing a separate sidebar system.

### Wide terminals

The transcript and active secondary pane coexist horizontally. Their divider supports mouse dragging.

- Persist a preferred review-pane ratio rather than a fixed column count
- Clamp the effective ratio to useful minimum widths for both surfaces
- Terminal resizing clamps presentation without overwriting the preference
- Do not provide default keyboard bindings for resizing
- Resize commands may exist without defaults so users can configure bindings

The normal inline review presentation stacks its file list above the diff:

```text
Transcript │ Files
           │────────────
           │ Diff
```

When the measured review pane width crosses the review UI's existing wide-layout breakpoint, it promotes to a persistent tree-and-diff layout:

```text
Transcript │ Tree │ Diff
```

This decision depends on the review pane's measured width, not only the terminal width. Phase 4 should reuse the breakpoint logic already owned by the review UI rather than introduce a separate shell threshold.

The scratchpad uses this same pane host on wide terminals. On narrow terminals it retains its existing dialog presentation initially; the transcript/review tabs remain specific to review.

### Narrow terminals

Use a tabbed transcript/review workspace with one visible surface at a time:

```text
[ Transcript ] [ Code review · 2 ]
```

- Transcript and review state remain mounted or otherwise retained
- The composer belongs to the Transcript tab
- Context progress is visible only on the Transcript tab
- The global header and footer remain visible

### Minimizing review

Minimizing removes the review pane and returns the width to the transcript. Do not show minimized panels in the footer.

Initial restore paths are:

- `/code-review`
- the composer review attachment, when one exists
- the Review tab in narrow mode

A collapsed right-edge drawer handle may be considered later if usage demonstrates that a persistent restore affordance is necessary. It is not part of the initial plan.

## Migration sequence

### 1. Migrate shell chrome (complete)

- Introduce the fixed one-row header and footer
- Establish privileged and standard contribution packing
- Add deterministic overflow indicators and expansion surfaces
- Move context progress to the transcript boundary
- Preserve current transcript, overlay, and plugin behavior

This phase should ship independently before the editable review pane.

### 2. Introduce workspace state (complete)

Model explicitly:

- active secondary pane
- minimized/open state
- preferred pane ratio
- focused workspace surface
- narrow-mode selected tab

Replace the current loosely coupled `rightPanel` behavior through this interface rather than bypassing it.

### 3. Add the resizable pane host (complete)

- Host both code review and scratchpad as secondary pane surfaces
- Add the mouse-driven divider
- Clamp widths and persist the preferred ratio
- Prevent divider gestures from triggering transcript text selection or copy
- Add unbound resize commands only if useful for user customization

### 4. Extract review presentation (complete)

Separate editable review state and behavior from its current full-screen `ScreenLayout`, then render it through:

- the stacked inline layout
- the extra-wide tree-and-diff layout
- the narrow tab layout
- the existing full-screen review during migration where needed

Review selection, comments, scroll position, and drafts must survive responsive layout changes.

### 5. Consider other secondary panes

After code review and scratchpad establish the workspace abstraction, evaluate migrating activity into the same pane host. Do not generalize the public plugin API into arbitrary panel composition as part of this work.

## Key implications

- The shell becomes a persistent workspace rather than a transcript with temporary sidebars.
- Fixed chrome requires measured, deterministic contribution overflow.
- Review keymaps must be gated by workspace focus rather than relying on a modal scope.
- Responsive transitions must preserve review and transcript state.
- Plugin status APIs remain stable while Kit retains control of layout and interaction.
