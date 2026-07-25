# Pager

The pager provides a focused reading surface for substantial assistant output.

Instead of reading long responses only in the transcript, the pager can open that content in a section-based view for more deliberate review.

Current behavior:

- `/pager` opens the pager for the last long assistant response
- if the pager is already open, `/pager` closes it
- Kit can also auto-open the pager after a turn completes when the assistant response is long enough
- auto-open respects the `pager` setting
- if there is no long assistant response to page through, Kit shows a warning instead of opening the pager

The pager is designed to:

- split long content into sections
- let the user move section by section
- support focused reading outside the normal transcript flow
- support structured feedback on paged content

## Notes and feedback drafts

Each section can have one committed note. Press `n` to edit the current section's note. In the editor:

- `Enter` saves the note
- `Shift+Enter` inserts a newline
- `Esc` cancels the edit and restores the previously saved note

Saved notes are displayed below the paged content. Press `x` to clear the current section's note.

Closing the pager with `Esc` preserves committed notes in memory and creates or refreshes a `Pager feedback draft` attachment in the composer. Pressing `s` attaches the same draft and closes the pager. The attachment can be opened to resume the pager, and `/pager` also restores the latest draft.

The draft is scoped to the active Kit session and latest paged response. Sending the next user message consumes it. If message submission fails, the attachment and notes are restored. Removing the attachment discards the draft. Session changes and application exit also clear it. Uncommitted editor text is not retained.

## How to access it

Run:

```text
/pager
```

The pager may also open automatically after long assistant responses if pager auto-open is enabled in settings.
