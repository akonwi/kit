# TODO

## Product features

- **In-app settings menu** — Add a settings UI inside the app so users can discover and change configuration without manually editing config files.
- **Support custom themes** — Allow users to customize the app's visual theme, ideally through configurable or user-defined themes.
- **Squash child sessions into parent context** — Add a way to merge a completed child/side-quest session back into its parent as a concise summary, so work can branch into a separate thread and then resume in the parent with the new context incorporated.

## OpenTUI contributions

- **`onPaste` type definition missing from `TextareaProps`** — OpenTUI's Solid reconciler supports `onPaste` on `<textarea>` (OpenCode uses it), but the type definitions in `@opentui/solid` don't declare it. We use `@ts-ignore` as a workaround. PR to add `onPaste?: (event: PasteEvent) => void` to `TextareaProps` in `src/types/elements.d.ts`.
