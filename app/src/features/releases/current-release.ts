import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Release updates

- Added a startup check for newer stable Kit releases with a persistent header notice.
- Added the \`/release-notes\` command and workspace panel for reading the installed release notes offline.
- The release panel can open the latest available GitHub release when an update is available.

## Scratchpad

- Added the approval-gated \`update_scratchpad\` tool so agents can append to or replace the active session scratchpad.
- Added guarded atomic persistence for concurrent scratchpad updates across Kit processes.
- Improved confirmation dialogs with abort handling and scrollable detail content.

## Authentication

- Added \`/logout\` to remove credentials for one selected provider without clearing all saved authentication.
- Reconciled the active model after logout when another authenticated provider is available.

## Tool approvals

- Tool approval dialogs now show complete command, path, and argument summaries instead of truncating them.
`;
