# Review modal (`/review`)

## Status

Prototype in progress

## Goal

Add a `/review` command that opens a modal for reviewing the current uncommitted Git diff.

The modal should support:

- browsing changed files
- navigating hunks within a file
- leaving **file-level** notes
- leaving **hunk-level** notes
- submitting structured review feedback back to the agent

This is conceptually similar to the pager UX, but specialized for code review over diffs instead of markdown sections.

## Why

Current review of in-progress code changes is ad hoc:

- manual `git diff`
- agent reading changed files directly
- freeform follow-up prompts

A dedicated review modal would make code review a first-class interaction in Kit:

- inspect uncommitted changes quickly
- anchor feedback to the right file or hunk
- send structured review comments back to the agent

## Scope

### In scope for v1

- `/review` command
- current uncommitted diff as review source
- file list
- hunk navigation
- file-level notes
- hunk-level notes
- structured feedback submission to the agent
- refresh action while modal is open

### Out of scope for v1

- line-level comments
- split diff rendering
- staging / unstaging actions
- editing code directly in the modal
- commit history review
- merge-base or PR review workflows

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

3. **Note area / annotations**
   - can target either:
     - current file
     - current hunk
   - note mode should be explicit in the UI
   - should be designed so it can later grow into inline comment / annotation affordances inspired by diffs.com annotations support

### Navigation

Suggested controls:

- `↑/↓` — move files or hunks depending on active focus region
- `Tab` — cycle focus region / note target
- `[` / `]` or `Ctrl+J` / `Ctrl+K` — previous/next hunk
- `r` — refresh diff
- `Enter` — submit review feedback when not editing text
- `Esc` — close modal

Exact bindings can be refined during implementation.

## Anchoring model

### File-level notes

A file note applies to the entire changed file.

Use cases:

- naming comments
- structural comments
- tests needed for the whole file
- overall design concerns

### Hunk-level notes

A hunk note applies to the currently selected diff hunk.

Use cases:

- incorrect logic in a specific change block
- edge case in a local edit
- request for rewrite of a particular patch section

### No line-level comments in v1

Line-level review is intentionally deferred. Hunk-level anchoring is the right first granularity.

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

## Feedback submission

Submitting review comments should generate a structured user message back to the agent.

Example shape:

```md
Here is my review feedback on the current uncommitted changes.

## src/runtime/agent-runtime.ts

### File-level feedback
The overall reload flow looks good, but the user-facing messaging should be tighter.

### Hunk: @@ -664,7 +670,8 @@
This reloading logic should probably avoid showing a toast if nothing changed.

## src/features/commands/reload.ts

### Hunk: @@ -1,6 +1,10 @@
The description should mention context refresh explicitly.

Please use this review feedback to revise the changes.
```

If there are no notes, submission should be disabled or no-op.

## Refresh behavior

Because the diff is based on working tree state, it may change while the modal is open.

A refresh action should:

- rerun diff collection/parsing
- attempt to preserve selected file when possible
- attempt to preserve hunk selection when possible
- preserve note text where anchors still match reasonably

For v1, preserving file notes by file path and hunk notes by hunk header is acceptable.

## Rendering approach

The UI should be app-owned even if parsing/rendering helpers come from a library.

Kit should own:

- modal layout
- selection model
- note state
- keyboard handling
- structured feedback generation

A diff library can provide:

- parsing files/hunks/lines
- status classification
- robust handling of rename/add/delete cases

## `@pierre/diffs`

Current direction: use `@pierre/diffs` for diff parsing / structuring, while keeping Kit's review UX app-owned.

The diffs.com docs and examples are also useful product references, especially their annotations support. We should treat that as a forward-looking design input even if v1 remains file/hunk-note based.

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

- built-in plugin registers `/review`
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
