# TODO

## OpenTUI contributions

- **`onPaste` type definition missing from `TextareaProps`** — OpenTUI's Solid reconciler supports `onPaste` on `<textarea>` (OpenCode uses it), but the type definitions in `@opentui/solid` don't declare it. We use `@ts-ignore` as a workaround. PR to add `onPaste?: (event: PasteEvent) => void` to `TextareaProps` in `src/types/elements.d.ts`.
