# Themes

Kit supports partial custom themes in the native TUI. The built-in `system`
theme follows the terminal's colors. Custom themes override selected semantic
roles and inherit every omitted role from `system`, so a theme does not need to
define every color.

## Selecting a theme

Open the command palette and choose `/theme`. Moving through the list previews a
theme immediately. Press Enter to save the selection or Escape to restore the
previous theme.

Kit discovers themes from:

```text
$KIT_HOME/themes/*.json
```

`KIT_HOME` defaults to `~/.kit-v2` during rewrite development. A theme's name is
its filename without `.json`; for example, `~/.kit-v2/themes/nord.json` appears
as `nord`. The selected name is stored in the `theme` field of
`$KIT_HOME/settings.json`.

## Theme format

Theme files are JSON objects with optional `tokens` and `syntaxPalette` objects:

```json
{
  "tokens": {
    "bg": "#10151c",
    "bgSurface": "#18202a",
    "textPrimary": "#d8dee9",
    "textSecondary": "#a7b0c0",
    "borderAccent": "#88c0d0",
    "warningText": "#ebcb8b",
    "errorText": "#bf616a"
  },
  "syntaxPalette": {
    "comment": "#687487",
    "keyword": "#b48ead",
    "string": "#a3be8c",
    "function": "#88c0d0"
  }
}
```

Colors may use `#RGB`, `#RGBA`, `#RRGGBB`, or `#RRGGBBAA`. The value
`transparent` is accepted only for roles that permit transparency. Invalid
individual values are ignored while valid values continue to apply. Unknown
roles and top-level fields are preserved for forward compatibility.

Common semantic tokens include:

- surfaces: `bg`, `bgSurface`, `bgMuted`, `bgAccent`, `bgTransparent`
- text: `textPrimary`, `textSecondary`, `textMuted`, `textPlaceholder`
- borders: `borderDefault`, `borderFocused`, `borderAccent`
- status: `metaText`, `toolText`, `warningText`, `errorText`
- pickers: `pickerBg`, `pickerBorder`, `pickerFocusedBg`,
  `pickerFocusedText`, `pickerItemText`
- transcripts: `userText`, `assistantText`, `subagentText`, `reviewText`
- diffs: `diffAddedBg`, `diffRemovedBg`, `diffAddedContentBg`,
  `diffRemovedContentBg`

Syntax roles include `text`, `heading`, `bold`, `italic`, `link`, `comment`,
`string`, `number`, `keyword`, `keywordType`, `function`, `operator`, `variable`,
`member`, `builtin`, `type`, `punctuation`, `tag`, and `attribute`.

## Migrating themes from an older Kit installation

Migration is an explicit, user-managed operation. Normal Kit startup does not
read or modify `~/.kit`.

Before migrating, quit Kit and back up both homes. Then copy theme files from the
old home into the current home without overwriting themes you have already
created:

```sh
old_home="$HOME/.kit"
new_home="${KIT_HOME:-$HOME/.kit-v2}"

mkdir -p "$new_home/themes"
chmod 700 "$new_home" "$new_home/themes"
find "$old_home/themes" -maxdepth 1 -type f -name '*.json' \
  -exec cp -n '{}' "$new_home/themes/" ';'
chmod 600 "$new_home/themes/"*.json 2>/dev/null || true
```

To carry over the selected theme while preserving all other current settings,
use `jq` to merge only the legacy `theme` field:

```sh
old_home="$HOME/.kit"
new_home="${KIT_HOME:-$HOME/.kit-v2}"
old_settings="$old_home/settings.json"
new_settings="$new_home/settings.json"

mkdir -p "$new_home"
chmod 700 "$new_home"
selected=$(jq -r 'if (.theme | type) == "string" then .theme else "system" end' \
  "$old_settings")

if [ -f "$new_settings" ]; then
  tmp=$(mktemp "$new_home/settings.json.XXXXXX")
  jq --arg theme "$selected" '.theme = $theme' "$new_settings" >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$new_settings"
else
  jq -n --arg theme "$selected" '{theme: $theme}' >"$new_settings"
  chmod 600 "$new_settings"
fi
```

Alternatively, copy only the theme files, start Kit, and select the desired theme
through `/theme`. This avoids editing `settings.json` manually and is the
recommended approach when the old settings file contains unrelated options.

If a selected theme is missing or malformed, Kit starts with `system` colors and
reports the problem instead of failing startup.
