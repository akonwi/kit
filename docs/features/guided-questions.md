# Guided Questions

## Status

Available now.

## Goal

Provide structured multi-step clarification when the agent needs several user
answers instead of a single freeform follow-up.

## Current behavior

Kit exposes a `guided_questions` tool that the model can call to open a guided
questionnaire.

The questionnaire:

1. opens in a modal
2. shows one question at a time
3. supports `text`, `select`, `multiselect`, and `boolean` question types
4. returns structured answers to the tool result in `details.answers`

This is intended for cases where the agent is missing two or more pieces of
information and should collect them in a more structured way.

## Manual test command

Kit also exposes a manual test command:

- `/guidedQuestionsTest`

This opens a sample questionnaire so the flow can be tested without waiting for
an agent-triggered tool call.

## Notes

- guided questions are modal-based; they do not take over the normal composer dock
- `multiselect` answers are returned as string arrays
- the `guided_questions` guidance policy is appended to the system prompt while the tool is available
- after completion, the agent should use `details.answers` as the source of truth

## Source

- `src/features/guided-questions/`
- `src/shell/GuidedQuestionsModal.tsx`
- `src/features/commands/guided-questions-test.ts`
