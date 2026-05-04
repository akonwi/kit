# Review gutter comment markers

## Context

The current in-TUI review diff overlay renders saved line/range comments in a dedicated gutter lane in `src/features/review/ReviewContent.tsx`.

This is a useful improvement, but the current marker model is lossy in two ways.

## Issue 1: segmented same-side ranges render as one continuous block

### Problem

`savedCommentMarkers()` currently finds the first and last matching visible line index for a saved range inside a hunk, then renders one continuous vertical marker from `startIndex` to `endIndex`.

That assumes all lines belonging to the saved range are one contiguous visible block.

In unified diff rendering, same-side lines may be separated by:

- context rows
- opposite-side rows
- nearby mixed edits inside the same hunk

So a saved range note can visually appear to comment on rows that are merely *between* the first and last matching rows, even when those rows are not actually part of the saved range.

### Current source of the issue

In `ReviewContent.tsx`:

- accumulate `startIndex` and `endIndex`
- render one marker with `height: endIndex - startIndex + 1`

### Suggested direction

Render saved comment markers as **one or more visible segments**, not a single start/end span.

A likely shape:

- scan hunk rows in order
- collect consecutive matching rows into segments
- render one `SavedCommentMarker` per segment

That keeps the gutter overlay aligned with the actual visible rows covered by the saved range.

## Issue 2: overlapping or nested saved ranges are ambiguous in one gutter lane

### Problem

The draft model allows multiple saved range notes for the same file, including overlapping or nested ranges.

But the gutter currently renders all saved markers into one absolute lane:

- `left={1}`
- `width={1}`

If multiple saved ranges touch the same rows, their markers visually overlap in the same column. The user cannot tell:

- that there are multiple comments there
- where one saved comment starts vs another
- whether the overlap is intentional or accidental

### Suggested direction

Keep the compact gutter, but make overlapping saved comments distinguishable.

Possible approaches, in increasing complexity:

1. **Priority + count indicator**
   - keep one lane
   - render the most relevant marker visually
   - add a count badge or alternate glyph when multiple comments overlap on a row

2. **Small multi-lane layout**
   - allocate 2-3 saved-comment gutter columns when needed
   - place overlapping saved ranges in separate lanes
   - keep active cursor/range markers in their own lane

3. **Compressed overlap glyphs**
   - use alternate glyphs for overlap starts/continuations
   - e.g. a distinct marker when 2+ saved comments share a row

### Recommendation

Start with:

- segmented marker rendering for correctness
- then add a minimal overlap indicator for rows touched by 2+ saved comments

That should preserve the current compact UI while making the gutter materially more truthful.

## Implementation notes

This work likely stays localized to:

- `src/features/review/ReviewContent.tsx`

Potentially with a helper model like:

```ts
type SavedCommentMarkerSegment = {
  key: string;
  top: number;
  height: number;
  lane?: number;
  overlapCount?: number;
};
```

## Non-goals

- no need to redesign the entire diff renderer
- no need to change the attachment schema
- no need to make the gutter visually heavy

The goal is simply to make saved comment markers more accurate and more distinguishable while keeping the current compact whole-file review UX.
