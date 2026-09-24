# Themes

Kit supports partial custom themes in the native TUI and macOS app. The built-in
TUI `system` theme follows the terminal's colors. Custom themes override selected
semantic roles and inherit every omitted role from the active client fallback, so
a theme does not need to define every color.

## Selecting a theme

Open the command palette and choose `/theme`. Moving through the list previews a
theme immediately. Press Enter to save the selection or Escape to restore the
previous theme.

Kit discovers themes from:

```text
$KIT_HOME/themes/*.json
```

`KIT_HOME` defaults to `~/.kit`. A theme's name is its filename without `.json`;
for example, `~/.kit/themes/nord.json` appears as `nord`. The selected TUI name
is stored in the `theme` field of `$KIT_HOME/settings.json`. Set `KIT_HOME` to an
explicit isolated directory when developing or testing.

The macOS app reads the same directory and lists compatible files in Settings →
Appearance for separate light and dark assignments. Add or edit theme files in
the shared directory directly; the macOS light/dark selections remain app-local
and do not change the TUI selection.

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

## Using themes from another Kit home

Kit uses `$KIT_HOME/themes` directly; with the default home this is
`~/.kit/themes`. There is no automatic theme migration. Existing themes in the
default home are reused in place, and startup does not inspect another home.

When an explicitly isolated development home contains themes you want to copy,
copy them deliberately into the active home. For example:

```sh
source_home="$HOME/.kit-v2"
active_home="${KIT_HOME:-$HOME/.kit}"
if [ "$source_home" != "$active_home" ]; then
  mkdir -p "$active_home/themes"
  find "$source_home/themes" -maxdepth 1 -type f -name '*.json' \
    -exec cp -n '{}' "$active_home/themes/" ';'
fi
```

If a selected theme is missing or malformed, Kit starts with `system` colors and
reports the problem instead of failing startup.
