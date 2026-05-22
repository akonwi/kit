# Guided Questions

Kit exposes a `guided_questions` tool that the model can call when it needs structured clarification from the user.

This is intended for cases where the agent is missing multiple pieces of information and should gather them in a more structured way than a single freeform follow-up.

Current behavior:

- the questionnaire opens in a modal
- it shows one question at a time
- it supports `text`, `select`, `multiselect`, and `boolean` question types
- it returns structured answers in `details.answers`
- `multiselect` answers are returned as string arrays
- the `guided_questions` policy is appended to the system prompt while the tool is available

After completion, the agent should use `details.answers` as the source of truth.

## How to access it

This feature is accessed through the `guided_questions` tool when the model decides structured clarification is needed.
