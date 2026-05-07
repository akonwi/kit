# Settings

Kit includes an in-app settings surface so users can change active application settings without manually editing `~/.kit/settings.json`.

Current behavior:

- `/settings` opens a modal settings UI
- changes apply immediately when saved
- the modal supports keyboard and mouse interaction
- save failures are shown inline

The current settings UI exposes:

- theme selection
- code review diff view default (`diffs.view`)
- guided questions
- automatic session naming
- automatic pager opening
- automatic retry settings
- bells
- speech enablement
- speech max chars
- speech voice

Settings are persisted to `~/.kit/settings.json`.

## How to access it

Run:

```text
/settings
```
