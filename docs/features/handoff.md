# Handoff

Transfer conversation context to a new session.

## Trigger

`/handoff` command or UI action.

## How it works

1. User triggers handoff (command or picker)
2. A summary of the current session is generated
3. A new session is created with the summary as initial context
4. User continues in the new session with full history

## Summary

The handoff summary provides context for the new session:
- Recent conversation highlights
- Key decisions or code changes
- Current state of work

## Use case

- "Hand off" work to continue in a fresh context
- Start a new direction while preserving the original conversation
- Clear token budget while maintaining context

## Source

`src/features/commands/handoff.ts`
