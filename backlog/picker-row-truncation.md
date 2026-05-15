# Picker Row Truncation (Alternative to Detail Panel)

If the `Picker.Detail` approach (focused-item description panel) doesn't feel right,
an alternative is to enforce max lengths on both name and description directly in the
row, keeping the palette visually uniform.

## Idea

- Truncate `name` at a fixed max character count (e.g. 40 chars) with a trailing `…`
- Truncate `description` at a fixed max (e.g. 50 chars) with a trailing `…`
- Both shown inline on a single row — no detail panel needed

## Trade-offs

- Simpler layout, very uniform look
- Loses some description content for discovery
- Easy to tune the limits per-surface (command palette vs inline picker)
