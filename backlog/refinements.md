# Refinements Backlog

These are important but intentionally deferred refinements.

## Session / cwd behavior

- [ ] Define how pi-kit should support mutable cwd during a session
- [ ] Decide whether cwd changes should be persisted as custom session entries
- [ ] Decide how cwd changes affect tool execution and file resolution

## Transcript rendering

- [x] Markdown rendering for user and assistant messages (tree-sitter, concealed syntax)
- [x] Syntax-highlighted code blocks with language injection (ts, tsx, js, jsx)
- [x] Unified theme for user and assistant markdown
- [ ] Refine tool call vs tool result presentation
- [ ] Decide whether thinking should ever be shown and in what form
- [ ] Improve rendering for long tool output (collapse/expand)

## Shell UX

- [ ] Keyboard navigation across transcript, debug panel, overlays, and composer
- [ ] Overlay stack model for pickers/dialogs/menus
- [ ] Resize behavior for dock and optional panels
- [x] Palette `onDismiss` callback for cleanup on escape/dismiss

## Runtime / status

- [ ] Replace placeholder context % with real runtime/session usage
- [ ] Replace placeholder repo summary with real repo/runtime state
- [ ] Decide what bell/speech/runtime indicators belong in the new app
