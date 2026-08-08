# Code Review

Kit includes a terminal-native code review surface exposed through `/code-review`.

It lets you inspect the current working-tree diff, add structured review comments,
and submit that review back into Kit as a code-review attachment.

## Current behavior

`/code-review`:

- shows the current working-tree diff for the active repository
- includes staged changes, unstaged changes, and untracked files that can be represented as patches
- opens as an editable secondary workspace pane alongside the transcript
- uses a draggable divider and retains review state while the workspace resizes
- refreshes the working-tree diff automatically while review remains open, without resetting unchanged file state
- keeps commit and branch targets pinned to their selected revisions
- switches to Transcript/Code review tabs when both panes cannot remain useful
- shows a file list and a whole-file scrollable diff for the selected file
- keeps the file tree persistently visible when the measured review pane crosses the review UI's existing wide-layout breakpoint
- clearly marks skipped unchanged sections between change groups
- lets you navigate to skipped-section rows and expand or collapse them individually while reviewing a file
- supports unified and split diff views
- uses the saved `diffs.view` setting as the default diff view
- uses change groups as navigation landmarks inside the selected file
- supports inline file-level review notes
- supports inline same-side line comments and line-range comments
- highlights the active diff line and embeds saved comments directly below their target line/range
- autosaves committed review notes in memory for the active Kit session
- immediately projects committed notes into the composer as a structured code-review attachment
- restores those notes when `/code-review` is closed and reopened in the same session
- shows an empty state when there are no uncommitted changes

The in-TUI review flow uses the existing structured code-review attachment model rather than introducing a separate terminal-only review payload format.

## Interaction model

### File list mode

In the file list:

- move with `↑/↓` or `j/k`
- `Enter` focuses the selected file's diff
- `Space` collapses or expands the selected file
- `f` opens an inline file note editor
- `v` toggles unified/split diff view for the current review session
- `x` clears the selected file note
- `s` submits the composer immediately with the current review attachment and keeps the workspace open
- `Esc` closes `/code-review`; any current draft notes remain attached

### Patch focus mode

When focused on a file diff:

- the whole file diff remains visible in one scrollable pane
- `↑/↓` or `j/k` move the active line cursor
- moving past the top or bottom of a change group continues into the previous or next change group when possible
- `Tab` jumps to the next change group
- `Shift+Tab` jumps to the previous change group
- `↑/↓` can land on skipped-section rows between change groups
- `Space` expands or collapses the selected skipped section
- `Enter` opens an inline comment editor for the selected line, or confirms the current range selection
- `Ctrl+Enter` starts a same-side line-range selection from the current line
- `x` clears the selected saved line/range note, or cancels an active range selection
- `f` opens an inline file note editor
- `v` toggles unified/split diff view for the current review session
- `s` submits the composer immediately with the current review attachment and keeps the workspace open
- `Esc` cancels an active range selection first, then returns to the file list

## Review notes

Current in-TUI review drafts support:

- file notes
- line notes
- same-side line-range notes

File notes and line/range notes are authored inline. In note editors, `Enter` saves, `Shift+Enter` inserts a newline, and `Esc` cancels editing.

Saving a review note immediately creates or refreshes a visible code-review draft attachment in the composer. Pressing `s` submits the composer's current message and attachments immediately while keeping the review workspace open. Clicking the draft attachment restores the editable review workspace. Reopening review edits the same in-memory draft. Sending the next message keeps the review workspace open while consuming the attachment and clearing its submitted notes. Removing the attachment with `×` also discards that target's draft.

Historical code-review attachments render a compact summary in the transcript but are not interactive. The persistent review workspace remains the source for inspecting the current review target; Kit does not provide a separate read-only attachment viewer.

Committed notes autosave in memory. Closing and reopening `/code-review` during the same active Kit session restores them inline, where they remain editable and removable. Uncommitted editor text is not retained. Live working-tree refresh pauses while a note editor is open so the edited target does not move underneath it. Changing Kit sessions or exiting Kit discards review drafts.

Draft line coordinates are not reconciled when the underlying working tree changes while review is closed. Review drafts are intended for short-lived close/reopen workflows between agent turns.

## How to access it

Run:

```text
/code-review
```
