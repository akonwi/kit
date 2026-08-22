import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Browser-hosted terminal

- Added the experimental \`kit --web-tui\` mode, which runs Kit's complete OpenTUI interface in a browser through Ghostty Web.
- Added browser-native keyboard, mouse, selection, clipboard, notification, bell, theme, resize, and reconnect handling.
- Added optional startup model selection with \`--model provider/model-id\`.
- Added secure host and origin validation, optional Basic authentication, canonical public URLs, and browser coverage across Chromium, Firefox, and WebKit.

## Faster, clearer transcripts

- Enabled viewport culling for long transcripts and activity drawers, substantially reducing rendering work as sessions grow.
- Added compact Bash summaries and contained, scrollable output wells for expanded tool results.
- Improved activity rows with aligned disclosure controls, single-line argument previews, and visible activated skill names.
- Added Markdown rendering for sub-agent output and syntax highlighting for diff and patch code blocks.

## Workspace and pager

- Standardized workspace responsiveness around a shared 125-column split threshold instead of pane-specific minimum widths.
- Moved MCP status into a retained workspace pane with reactive updates.
- Added Copy and Quote selection actions to the pager; quoted text is inserted into pager notes without losing unsaved edits.

## Dialogs

- Confirmation dialogs now size themselves to their content while respecting sensible minimum and maximum widths.
`;
