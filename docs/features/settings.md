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

## Keybindings

Some shell keybindings can be customized in `~/.kit/settings.json` under `keybindings`. Values may be a key string, an array of key strings, or `false`/`null` to disable the binding.

```json
{
  "keybindings": {
    "command-palette.open": "ctrl+space",
    "composer.clear-or-quit": ["ctrl+c", "ctrl+q"],
    "composer.restore-or-recall": false
  }
}
```

The key syntax is OpenTUI keymap syntax such as `ctrl+p`, `shift+tab`, `return`/`enter`, `escape`, `up`, or multi-key sequences like `gg` when a feature layer supports them.

## How to access it

Run:

```text
/settings
```
