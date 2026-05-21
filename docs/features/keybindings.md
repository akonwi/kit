# Keybindings

Kit supports user-configurable keybindings for core workflow surfaces through `~/.kit/settings.json`.

Configure bindings under `keybindings` with command ids as keys. Values can be:

- a key string: `"ctrl+space"`
- an array of key strings: `["ctrl+c", "ctrl+q"]`
- `false` or `null` to disable the default binding

```json
{
  "keybindings": {
    "command-palette.open": "ctrl+space",
    "composer.clear-or-quit": ["ctrl+c", "ctrl+q"],
    "composer.restore-or-recall": false
  }
}
```

Key strings use OpenTUI Keymap syntax, such as `ctrl+p`, `shift+tab`, `return`/`enter`, `escape`, `up`, or `gg`.

Invalid key strings and same-layer conflicts are ignored and reported as warning toasts. If multiple bindings in one layer collide, the first binding wins. Cross-layer overlaps are allowed and use normal layer precedence.

## Configurable command catalog

Only user-facing workflow commands are listed here. Setup, configuration, plugin infrastructure, auth, and fatal/error screens may use fixed internal shortcuts that are not part of the user customization surface.

### App

| Command id | Default keys | Description |
| --- | --- | --- |
| `command-palette.open` | `ctrl+p` | Open command palette |

### Command palette

| Command id | Default keys | Description |
| --- | --- | --- |
| `command-palette.move-up` | `up` | Move selection up |
| `command-palette.move-down` | `down` | Move selection down |
| `command-palette.complete` | `tab` | Complete selection |
| `command-palette.select` | `return` | Run selected command |
| `command-palette.close` | `escape` | Close command palette |

### Composer

| Command id | Default keys | Description |
| --- | --- | --- |
| `composer.clear-or-quit` | `ctrl+c` | Clear input or quit |
| `composer.abort` | `escape` | Abort response |
| `composer.steer` | `return` | Steer with queued follow-ups |
| `composer.bash-history-older` | `up` | Recall previous bash command |
| `composer.bash-history-newer` | `down` | Recall next bash command |
| `composer.restore-or-recall` | `up` | Restore queued follow-ups or recall previous message |

### Inline picker

| Command id | Default keys | Description |
| --- | --- | --- |
| `picker.move-up` | `up` | Move selection up |
| `picker.move-down` | `down` | Move selection down |
| `picker.select` | `return` | Insert/select current item |
| `picker.close` | `escape` | Close picker |

### Pager

| Command id | Default keys | Description |
| --- | --- | --- |
| `pager.previous-section` | `left`, `h` | Show previous pager section |
| `pager.next-section` | `right`, `l` | Show next pager section |
| `pager.scroll-up` | `up`, `k` | Scroll pager up |
| `pager.scroll-down` | `down`, `j` | Scroll pager down |
| `pager.edit-note` | `n`, `i` | Edit note for current pager section |
| `pager.submit-feedback` | `ctrl+return` | Submit pager feedback |
| `pager.close` | `escape`, `q` | Close pager |
| `pager.back` | `escape` | Return to pager navigation |

### Guided questions

| Command id | Default keys | Description |
| --- | --- | --- |
| `guided-questions.previous` | `shift+tab` | Go to previous question |
| `guided-questions.cancel` | `escape` | Cancel guided questions |
| `guided-questions.move-up` | `up` | Move to previous option |
| `guided-questions.move-down` | `down` | Move to next option |
| `guided-questions.select` | `return` | Select focused option |
| `guided-questions.toggle-option` | `space` | Toggle focused option |
| `guided-questions.confirm-multiselect` | `return` | Confirm selected options |
| `guided-questions.submit-text` | `return` | Submit text answer |
| `guided-questions.back` | `escape` | Return to option selection |

### Review

| Command id | Default keys | Description |
| --- | --- | --- |
| `review.close` | `escape` | Close code review |
| `review.move-file-up` | `up`, `k` | Move to previous file |
| `review.move-file-down` | `down`, `j` | Move to next file |
| `review.focus-file` | `return` | Focus selected change group |
| `review.toggle-file` | `space` | Collapse or expand selected file |
| `review.file-note` | `f` | Edit file note |
| `review.clear-file-note` | `x` | Clear file note |
| `review.toggle-view` | `v` | Toggle diff view |
| `review.submit` | `s` | Attach review notes |
| `review.back` | `escape` | Return to file list |
| `review.previous-change` | `shift+tab` | Move to previous change group |
| `review.next-change` | `tab` | Move to next change group |
| `review.move-line-up` | `up`, `k` | Move line cursor up |
| `review.move-line-down` | `down`, `j` | Move line cursor down |
| `review.toggle-section` | `space` | Collapse or expand skipped section |
| `review.comment-line` | `return` | Comment selected line |
| `review.start-range` | `ctrl+return` | Start range selection |
| `review.clear-line-note` | `x` | Clear line note |
| `review.close-editor` | `escape` | Close note editor |

### Session explorer

| Command id | Default keys | Description |
| --- | --- | --- |
| `session-explorer.close` | `escape`, `ctrl+c` | Close session explorer |
| `session-explorer.select` | `return` | Switch to selected session |
| `session-explorer.move-up` | `up`, `k` | Move to previous session |
| `session-explorer.move-down` | `down`, `j` | Move to next session |
| `session-explorer.page-up` | `pageup` | Scroll sessions up |
| `session-explorer.page-down` | `pagedown` | Scroll sessions down |
| `session-explorer.rename` | `r` | Rename selected session |
| `session-explorer.delete` | `ctrl+d` | Delete selected session |
| `session-explorer.squash` | `s` | Squash selected session |
| `session-explorer.rename-save` | `return` | Save session name |
| `session-explorer.rename-cancel` | `escape`, `ctrl+c` | Cancel session rename |
| `session-explorer.confirm` | `return` | Confirm session action |
| `session-explorer.cancel` | `escape`, `ctrl+c` | Cancel session action |

### MCP and debug

| Command id | Default keys | Description |
| --- | --- | --- |
| `mcp-status.close` | `escape`, `return` | Close MCP status |
| `mcp-authorization-url.continue` | `return`, `escape` | Continue after MCP authorization |
| `debug.close` | `return`, `escape` | Close debug view |
