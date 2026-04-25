# Diff Viewer

Kit includes a terminal-native diff viewer for inspecting the current working-tree diff.

The diff viewer is separate from browser-backed `/code-review`. It is for quick inspection inside the terminal rather than structured browser review.

Current behavior:

- it shows the current working-tree diff for the active repository
- that includes staged changes, unstaged changes, and untracked files that can be represented as patches
- it opens as a terminal overlay
- it shows a file list and patch view
- files can be expanded or collapsed
- a selected patch can be focused for scrolling
- if there are no uncommitted changes, it shows an empty-state message

## How to access it

Run:

```text
/diff
```
