import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Full-file review workflows

- Added repository file browsing through the workspace file finder and an in-pane directory drawer.
- Unchanged files now open in deduplicated workspace tabs with syntax highlighting and secure, bounded file reads.
- Added line, range, and whole-file comments for normal files, with revision-aware drafts and structured feedback attachments.
- Code Review can open changed files in full-file tabs while remaining focused on diffs.

## Workspace navigation

- Code Review and Scratchpad are now persistent default workspace tabs that initialize lazily while the secondary surface remains minimized by default.
- Added a header toggle for collapsing or expanding Code Review's changed-files tree in wide and narrow layouts.
- Clicking sub-agent tool-call names now opens the matching conversation or Activity workspace pane.
- Workspace panes and repository-scoped review drafts now follow session repository changes safely.

## Default model preference

- Added a filterable default-model setting with an Automatic option.
- New interactive, print, RPC, and web sessions use the configured default model unless an explicit \`--model\` override is supplied.
- Existing sessions continue using their saved model.
`;
