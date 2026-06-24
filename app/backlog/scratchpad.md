# Scratchpad

## Goal

Add a user-owned scratchpad: a plain text note buffer for plans, reminders, TODOs, and context that can live alongside a Kit session.

The scratchpad is **read-only to the agent**. Users can edit it; the agent can read it as ambient context but cannot modify it.

## UX shape

### Surface

Use a right-side **inline panel**, similar to the turn activity sidebar, rather than a full-screen view or modal.

Why:

- Users should be able to see the transcript while reading or editing notes.
- It should feel like a companion workspace, not a separate task.
- It can reuse Kit's inline panel conventions: edge border, header strip, scrollable body, and footer hint bar.

On narrow terminals, use a dialog fallback. The scratchpad and other right-side panels should be mutually exclusive if they compete for the same slot.

### Panel layout

- Left edge border using `borderDefault`.
- Header strip with title `Scratchpad` in `textPrimary`.
- No line-count metadata in the header; it is not useful enough to spend UI space on.
- Body is scrollable plain text.
- Empty state: centered `No notes yet` in `textMuted`.
- Footer uses a bordered hint bar.

### Modes

#### Viewing mode

Default when opened.

- Body shows the current scratchpad content as plain text.
- Transcript/navigation keybindings should remain usable.
- Hint bar: `Enter edit · Esc close`.

#### Editing mode

Entered from viewing mode with `Enter` or `e`.

- Body swaps to a multiline editor, prefilled with the current scratchpad contents.
- Editor receives focus.
- Use accent treatment (`borderAccent`) to indicate active editing.
- `Ctrl+S` saves and returns to viewing mode.
- `Esc` cancels edits and returns to viewing mode.

This two-mode model avoids making the scratchpad a keyboard trap during normal transcript interaction.

## Agent relationship

When non-empty, include the scratchpad in agent context as ambient read-only context, similar to a context file:

```xml
<context-file path="<scratchpad>">
User scratchpad notes. Read-only to the agent; do not modify.

...
</context-file>
```

Behavior:

- Omit the scratchpad from context when blank.
- Include the latest saved scratchpad content on the next user turn.
- Do not expose any tool for the agent to edit the scratchpad.
- The agent should not quote or announce the scratchpad unless directly relevant; it is ambient context.

## Persistence

Persist per session, likely as `scratchpad.md` alongside the session data.

Rationale:

- Scratchpad notes are session-local working memory.
- Global guidance already has `~/.kit/AGENTS.md`.
- Plain markdown-compatible text remains easy to inspect or edit outside Kit.

## Non-goals for v1

- No markdown rendering; display as plain text.
- No tabs, sections, or hierarchy beyond what users type.
- No search.
- No rich attachments.
- No agent-authored scratchpad edits or suggested patches.

## Implementation outline

1. Add a `ScratchpadPanel` component following inline panel conventions.
2. Add scratchpad state to the app/session layer.
3. Persist scratchpad content per session.
4. Register a `scratchpad.toggle` command and default keybinding.
5. Integrate scratchpad content into context assembly when non-empty.
6. Coordinate the scratchpad with other right-side inline panels so only one occupies the slot at a time.
