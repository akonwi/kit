# Diff modal (`/diff`)

## Status

Prototype in progress

## Goal

Add a `/diff` command that opens a terminal modal for browsing the current uncommitted Git diff.

The modal should support:

- browsing changed files
- expanding/collapsing file patches
- navigating between files
- navigating hunks within a focused patch

This is conceptually similar to the pager UX, but specialized for terminal diff inspection rather than markdown sections.

## Why

Current review of in-progress code changes is ad hoc:

- manual `git diff`
- agent reading changed files directly
- freeform follow-up prompts

A dedicated diff modal makes uncommitted change inspection a first-class interaction in Kit:

- inspect uncommitted changes quickly
- browse file patches without leaving the terminal
- keep lightweight diff viewing separate from richer review workflows

## Scope

### In scope for v1

- `/diff` command
- current uncommitted diff as review source
- file list
- hunk navigation
- file accordion list
- focused patch mode
- hunk navigation while patch-focused
- refresh action while modal is open

### Out of scope for v1

- annotations/comments
- split diff rendering
- staging / unstaging actions
- editing code directly in the modal
- commit history review
- merge-base or PR review workflows
- browser-backed code review workflows

## Review source

The source of truth should be the current uncommitted diff in the working tree.

Likely includes:

- unstaged changes
- staged changes

Open question:
- whether staged and unstaged changes should be merged into one logical review surface or shown separately in metadata

For v1, a single combined review surface is acceptable if the parsed diff model preserves file and hunk boundaries.

## UX

### Layout

Target direction: closely mirror the diffs.com review UI shape shown in the reference screenshot the user shared.

A modal overlay with three conceptual areas:

1. **File list**
   - changed files
   - status badge (`M`, `A`, `D`, `R`, etc.)
   - note indicators for file/hunk comments

2. **Diff pane**
   - shows the selected file diff
   - supports moving between hunks
   - selected hunk should be visually distinct
   - should evolve toward a richer, polished diff presentation similar to diffs.com rather than a plain text patch viewer

3. **Focused patch view**
   - patch focus mode should keep the file header visible while navigating the patch
   - hunk navigation should be clear in this focused mode
   - richer annotations are intentionally deferred to the browser-backed `/code-review` flow

### Navigation

Suggested controls:

- `↑/↓` — move files or hunks depending on active focus region
- `Enter` — focus the current expanded patch
- `[` / `]` or `Ctrl+J` / `Ctrl+K` — previous/next hunk
- `r` — refresh diff
- `Enter` — submit review feedback when not editing text
- `Esc` — close modal

Exact bindings can be refined during implementation.

## Hunk navigation model

The terminal diff viewer should support a current hunk concept while in focused patch mode.

Use cases:

- jump between hunks quickly
- keep context visible around the selected hunk
- prepare for a future handoff into richer browser-backed review

Inline annotation and comment anchoring are intentionally deferred from `/diff`.

## Data model

Proposed internal types:

```ts
type ReviewFile = {
  path: string;
  status: "modified" | "added" | "deleted" | "renamed" | "unknown";
  oldPath?: string;
  fileNote: string;
  hunks: ReviewHunk[];
  rawPatch: string;
};

type ReviewHunk = {
  id: string;
  header: string; // e.g. @@ -42,7 +42,9 @@
  note: string;
  lines: ReviewLine[];
};

type ReviewLine = {
  kind: "context" | "add" | "delete";
  text: string;
};
```

Notes:

- `id` should be stable for the current parsed diff snapshot
- hunk IDs do not need to survive a refresh if the diff changed; refresh can rebuild state conservatively

## Submission

`/diff` is a viewer, not a structured review submission flow.

Structured annotations and submission should move to the future browser-backed `/code-review` experience.

## Refresh behavior

Because the diff is based on working tree state, it may change while the modal is open.

A refresh action should:

- rerun diff collection/parsing
- attempt to preserve selected file when possible
- attempt to preserve hunk selection when possible
- preserve note text where anchors still match reasonably

For v1, preserving file selection and focused hunk where possible is acceptable.

## Rendering approach

The UI should be app-owned even if parsing/rendering helpers come from a library.

Kit should own:

- modal layout
- selection model
- focus/selection state
- keyboard handling
- patch-focused navigation

A diff library can provide:

- parsing files/hunks/lines
- status classification
- robust handling of rename/add/delete cases

## `@pierre/diffs`

Current direction: use `@pierre/diffs` for diff parsing / structuring, while keeping Kit's review UX app-owned.

The diffs.com docs and examples are still useful references, but richer annotation behavior should be treated as forward-looking input for the browser-backed `/code-review` flow rather than the terminal `/diff` viewer.

Questions to answer during investigation:

- Can it run cleanly in Bun / this repo's environment?
- Does it expose a usable AST/model for:
  - files
  - hunks
  - lines
- Does it help with rendering, or is it primarily parsing?
- Does it support combined staged + unstaged diff workflows, or do we still assemble the raw diff ourselves?
- Is it lightweight enough for an in-app modal workflow?

If it is not a good fit, we can fall back to parsing unified diff ourselves or use a smaller parser-oriented dependency.

## Architecture placement

Suggested modules:

- `src/features/review/`
  - `index.tsx`
  - `ReviewContent.tsx`
  - `review-controller.ts`
  - `diff-model.ts`
  - `feedback.ts`

Likely integration shape:

- built-in plugin registers `/diff`
- modal opened via existing overlay/custom UI path
- feedback submitted through runtime in the same spirit as pager feedback

## Risks

### 1. Diff parsing complexity

This is the biggest technical risk, and the main reason to investigate `@pierre/diffs` first.

### 2. Large diffs

Large diffs may require:

- viewporting / scrollbox usage
- hunk jumping
- lazy rendering later

### 3. Refresh invalidation

When the diff changes, hunk anchors may become invalid. v1 should prefer predictable rebuild behavior over overly clever state reconciliation.

## Recommendation

Proceed with:

1. dependency/feasibility investigation for `@pierre/diffs`
2. parser/data-model prototype
3. modal UI scaffold
4. file + hunk note submission

This should be treated as a focused code-review workflow, not just another diff viewer.
