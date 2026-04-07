# Wizard / Guided Questions

## Status

Partially implemented, not fully wired into the active shell flow.

## Goal

Provide structured multi-step input when the agent needs guided answers rather
than a single freeform prompt.

## Current foundation

Wizard-related modules already exist:

- `src/features/wizard/tool.ts`
- `src/features/wizard/wizard-controller.ts`
- `src/features/wizard/types.ts`
- `src/shell/WizardView.tsx`
- `src/shell/WizardDock.tsx`

## Intended behavior

1. The agent requests guided input through a dedicated tool
2. The shell activates a wizard-specific surface
3. Questions are answered in a structured flow
4. The result is submitted back to the agent as structured data

## Current caveat

The wizard is not currently part of the minimum working loop. The foundational
pieces exist, but the end-to-end activation and shell integration still need to
be rebuilt cleanly on the current architecture.
