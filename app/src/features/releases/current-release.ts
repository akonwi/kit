import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Workspace tabs

- Added responsive workspace tabs for navigating auxiliary surfaces without losing their local state.
- Added browser tabs for Activity, Code Review, and Scratchpad, with a resizable desktop drawer, collapsed rail, mobile overflow sheet, keyboard navigation, and accessible focus handling.
- Expanded the terminal workspace with retained tabs, overflow navigation, responsive layouts, and dedicated Sub-agent, Release Notes, Mermaid, Review, Scratchpad, and Activity surfaces.

## Follow-up controls

- Added browser controls to edit queued follow-ups in the composer or send them immediately as steering messages.
- Composer drafts now persist per session and synchronize across browser tabs.
- Added guarded, idempotent recovery so queued-message edits survive reloads, response loss, and concurrent clients.
- Added RPC methods for safely restoring and promoting queued follow-ups.

## Transcript interactions

- Added a terminal selection menu with Copy and Quote actions; Quote inserts a Markdown blockquote into the composer.
- Added whole-message copying that preserves the original Markdown source.
- Made safe HTTP, HTTPS, and email links in terminal Markdown clickable.
- Improved pointer handling to prevent accidental activation while dragging or interacting with nested controls.

## Fresh headless sessions

- Added \`kit new --web\`, \`kit new --rpc\`, and \`kit new -p\` for starting fresh sessions instead of resuming the latest session.
`;
