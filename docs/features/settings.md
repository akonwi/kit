# Settings

## Status

Available now.

## Goal

Provide an in-app settings surface for Kit so users can change active app settings without manually editing `~/.kit/settings.json`.

## Current behavior

Kit exposes a `/settings` command that opens a modal settings UI.

The current modal:

- is centered and overlay-based
- applies changes immediately
- supports keyboard and mouse interaction
- only surfaces save failures inline

## Settings currently exposed

### General

- `guidedQuestions`
- `sessionNaming`
- `pager`

### Notifications

- `bells`
- `speech.enabled`
- `speech.maxChars`
- `speech.voice`

## Notes

- settings are persisted to `~/.kit/settings.json`
- speech settings are normalized in the UI to an object shape even if the stored value started as a boolean
- speech sub-fields stay visible but are disabled when speech is off
- the current UI is functional but expected to evolve visually

## Source

- `src/features/settings/`
- `src/settings.ts`
