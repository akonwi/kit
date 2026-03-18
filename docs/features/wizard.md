# Wizard / Questionnaire

Guided question flows for complex multi-step interactions.

## Trigger

Activated by the agent via tool calls that request structured input.

## How it works

1. Agent requests information via a question tool
2. A wizard UI replaces the composer with a guided input flow
3. Questions are presented one at a time with input validation
4. On completion, answers are submitted back to the agent

## UI Components

- **WizardDock** — replaces the composer when wizard is active
- **WizardView** — displays the wizard questions and progress
- **Questionnaire normalization** — answers can be normalized/transformed before submission

## Source

`src/features/wizard/`
