# Code Review

Kit includes a terminal-native code review surface exposed through `/code-review`.

It lets you inspect the current working-tree diff, add structured review comments,
and submit that review back into Kit as a code-review attachment.

## Current behavior

`/code-review`:

- shows the current working-tree diff for the active repository
- includes staged changes, unstaged changes, and untracked files that can be represented as patches
- opens as a terminal overlay
- shows a file list and a whole-file scrollable diff for the selected file
- clearly marks skipped unchanged sections between change groups
- lets you navigate to skipped-section rows and expand or collapse them individually while reviewing a file
- supports unified and split diff views
- uses the saved `diffs.view` setting as the default diff view
- uses change groups as navigation landmarks inside the selected file
- supports inline file-level review notes
- supports inline same-side line comments and line-range comments
- highlights the active diff line and embeds saved comments directly below their target line/range
- autosaves committed review notes in memory for the active Kit session
- restores those notes when `/code-review` is closed and reopened in the same session
- submits the result as a structured code-review attachment
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
- `s` queues the current draft as an attachment and closes `/code-review`
- `Esc` closes `/code-review`, also queueing any current draft notes

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
- `s` queues the current draft as an attachment and closes review
- `Esc` cancels an active range selection first, then returns to the file list

## Review notes

Current in-TUI review drafts support:

- file notes
- line notes
- same-side line-range notes

File notes and line/range notes are authored inline. In note editors, `Enter` saves, `Shift+Enter` inserts a newline, and `Esc` cancels editing.

Closing review with committed notes creates or refreshes a visible code-review draft attachment in the composer. Clicking the draft attachment opens a read-only viewer in the right sidebar on wide terminals (or a dialog on narrow terminals); `e` opens the full review screen for editing. Reopening review edits the same in-memory draft, and closing it refreshes the attachment. Sending the next message consumes the attachment and clears that target's draft. Removing the attachment with `×` also discards that target's draft.

Historical code-review attachments in the transcript are also clickable and open the same viewer in read-only mode. The viewer presents the structured review grouped by file, including file notes and addition/deletion line ranges. It lazily reconstructs bounded diff excerpts for committed reviews from their stored parent/head revisions and shows live excerpts for working-tree drafts. Historical working-tree reviews remain comment-only because their original uncommitted source is not retained. Excerpts are UI-only and are never added to the message payload or prompt.

Committed notes autosave in memory. Closing and reopening `/code-review` during the same active Kit session restores them inline, where they remain editable and removable. Uncommitted editor text is not retained. Changing Kit sessions or exiting Kit discards review drafts.

Draft line coordinates are not reconciled when the underlying working tree changes while review is closed. Review drafts are intended for short-lived close/reopen workflows between agent turns.

## How to access it

Run:

```text
/code-review
```
