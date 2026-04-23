# Browser-backed code review (`/code-review`)

## Status

Prototype in progress

## Decision

Split diff inspection from full review:

- `/diff` stays terminal-native and focuses on browsing the current uncommitted diff
- `/code-review` should become a browser-backed review experience with richer hunk navigation and annotation UI

## Why

The current OpenTUI diff surface works well for lightweight inspection, but it is a poor fit for richer review interactions like:

- clear visual framing around the current hunk
- inline annotations/comments
- sticky local headers while scrolling patch content
- richer review layout and future commenting workflows

A browser-rendered SPA removes most of those rendering constraints.

## Product shape

### `/diff`

Terminal-native modal for:

- quickly viewing current uncommitted diffs
- file-by-file accordion browsing
- hunk navigation while focused on a patch
- staying entirely inside the terminal

### `/code-review`

Browser-backed review UI for:

- richer annotation affordances
- hunk-focused review
- clearer visual anchoring
- future structured code review workflows

## Architecture direction

Kit should remain the orchestrator:

1. gather the current diff review payload
2. launch a localhost-backed review page
3. open the browser
4. exchange state/results with the SPA
5. receive structured review output back into Kit
6. attach submitted review state to the next user message sent from Kit

Current direction:

- localhost HTTP server owned by Kit
- browser page opened directly with the system browser
- WebSocket bridge for browser ↔ Kit communication
- a real SPA entrypoint served by Kit
- client-side diff rendering in the SPA using `@pierre/diffs`
- plugin-owned review submission state, with app-owned extension points for composer decoration/rendering

Likely later refinements:

- localhost HTTP + WebSocket for live review state
- localhost HTTP + POST for one-shot submit or export flows
- richer client-side review state and persistence
- attachment rendering primitives for composer and transcript surfaces

## Submission payload

Current browser submit payload:

```ts
type CodeReviewSubmission = {
  submittedAt: string;
  files: Array<{
    path: string;
    fileComment: string;
    ranges: Array<{
      side: "additions" | "deletions";
      startLine: number;
      endLine: number;
      comment: string;
    }>;
  }>;
};
```

Notes:

- this is intentionally lightweight
- it is meant to describe feedback for the current diff set, not long-term review storage
- anchoring is currently by:
  - file path
  - diff side
  - selected line range
- that is acceptable for the current product goal of contextual feedback on the current uncommitted diff

## Review attachment flow in Kit

Agreed UX:

1. user opens `/code-review` in the browser
2. user writes comments and submits from the browser
3. the submission is **not** immediately appended to the transcript as its own user message
4. instead, Kit should treat it as a **pending attachment** for the next user message
5. when the user returns to Kit, the composer should show that a code review attachment is pending
6. when the user sends their next message, Kit should send both together:
   - the typed user message
   - the attached submitted review payload
7. after send, the pending review attachment should clear from the composer

Why:

- the browser review submission is better modeled as structured context for the next prompt than as a standalone conversational turn
- this keeps the typed message and the submitted review in a single user turn
- it leaves room for richer transcript rendering later

## Composer presentation for pending attachments

Settled direction for the first implementation:

- show pending attachments in a compact drawer/row directly above the composer
- do **not** show an extra label such as "Attached to next message"
- each attachment row should include:
  - disclosure affordance (`▸` / `▾`)
  - plugin-defined icon
  - attachment summary text
  - trailing clickable `×` remove control
- the full row should toggle expand/collapse, except for the `×` remove button
- start with a single attachment in practice, but keep the design structurally extensible to multiple attachments later

Example collapsed row:

```text
▸ 🧐 Code review · 3 comments · 2 files                    [×]
```

Example expanded row:

```text
▾ 🧐 Code review · 3 comments · 2 files                    [×]
  src/foo.ts
    - 2 comments
  src/bar.ts
    - 1 comment
  [Open review preview]
```

Attachment icon guidance:

- plugins should define the icon shown with their attachment summary
- code review attachment icon can use either `📝` or `🧐`
- regular file attachments can later use a paperclip-style icon such as `📎`

First-pass expanded content should stay lightweight:

- file list
- comment counts per file
- quick preview affordance
- no full inline diff rendering inside the composer drawer yet

## Plugin architecture implication

The important extension point is composer decoration/rendering, not necessarily shared attachment state ownership.

Working assumption:

- the CodeReview plugin can maintain its own pending review state
- the shell/composer needs an extensible way for plugins to contribute attachment-like UI above the composer
- this mechanism should be generic enough to support future attachment types beyond code review

This same model should later support transcript rendering for attached review content, including the possibility of rendering diff excerpts with the terminal diff UI.

## Inspiration

- `pi-diff-review`
- `glimpse`

These are still useful references for the browser review UX and host/UI split, even though the current prototype direction is to use the system browser directly rather than a Glimpse window.

## Current implementation status

The current implementation now includes:

- `/code-review` opens a localhost-backed SPA
- the SPA connects back to Kit over WebSocket
- Kit sends diff/session state to the SPA
- the SPA renders patches client-side with `@pierre/diffs`
- the browser shell uses Kit-aligned theme tokens
- review comments can be authored as:
  - file comments
  - single-line comments
  - same-side line-range comments
- browser submit sends a structured review payload back to Kit
- after submit, the browser review form resets locally and refreshes current diff state

Still missing:

- transcript-integrated review attachments
- composer decoration for pending submitted reviews
- persistence of submitted reviews into sent conversation messages
- richer in-terminal rendering of submitted review content
