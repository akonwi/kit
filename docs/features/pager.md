# Pager

Long-form output review mode for agent responses with section-by-section notes.

## Trigger

The pager activates automatically when the agent finishes a turn with 2+ sections. Press Escape to close.

## How it works

1. Agent completes a turn with structured output (e.g., multiple file edits, multi-step results)
2. The pager automatically splits the response into sections
3. Each section can be annotated with a note
4. Ctrl+Enter submits all notes as feedback to the agent

## Controls

- **Escape** — close pager
- **Ctrl+Shift+Right** — next section
- **Ctrl+Shift+Left** — previous section  
- **Ctrl+Up/Down** — scroll current section
- **Ctrl+Enter** — submit all notes as feedback

## Notes

- Notes are per-section and persisted in memory
- Notes auto-save when navigating between sections
- Empty notes are ignored on submit

## Source

`src/features/pager/`
