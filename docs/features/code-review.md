# Code Review

Kit includes a terminal-native code review surface exposed through `/code-review`.

It lets you inspect the current working-tree diff, add structured review comments,
and submit that review back into Kit as a code-review attachment.

## Current behavior

`/code-review`:

- shows the current working-tree diff for the active repository
- includes staged changes, unstaged changes, and untracked files that can be represented as patches
- opens as a terminal overlay
- shows a file list and a whole-file scrollable unified diff for the selected file
- uses change groups as navigation landmarks inside the selected file
- supports file-level review notes
- supports same-side line comments and line-range comments
- shows gutter markers for active selection and saved comments
- submits the result as a structured code-review attachment
- shows an empty state when there are no uncommitted changes

The in-TUI review flow uses the existing structured code-review attachment model rather than introducing a separate terminal-only review payload format.

## Interaction model

### File list mode

In the file list:

- move with `↑/↓` or `j/k`
- `Enter` focuses the selected file's diff
- `Space` collapses or expands the selected file
- `f` opens a file note editor
- `x` clears the selected file note
- `s` submits the current draft review
- `Esc` closes `/code-review`

### Patch focus mode

When focused on a file diff:

- the whole file diff remains visible in one scrollable pane
- `↑/↓` or `j/k` move the active line cursor
- moving past the top or bottom of a change group continues into the previous or next change group when possible
- `Tab` jumps to the next change group
- `Shift+Tab` jumps to the previous change group
- `Enter` comments the selected line, or confirms the current range selection
- `Ctrl+Enter` starts a same-side line-range selection from the current line
- `x` clears the selected saved line/range note, or cancels an active range selection
- `f` opens a file note editor
- `s` submits the current draft review
- `Esc` cancels an active range selection first, then returns to the file list

## Review notes

Current in-TUI review drafts support:

- file notes
- line notes
- same-side line-range notes

Submitted reviews are attached back into the composer as structured code-review context rather than inserted as a standalone plain-text message.

## How to access it

Run:

```text
/code-review
```
