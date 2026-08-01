import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Workspace panels

- Added a persistent, responsive secondary workspace beside the transcript.
- Moved activity, scratchpad, code review, and sub-agent status into retained workspace panels.
- Added narrow Transcript/secondary tabs and workspace focus cycling with \`F6\`.

## Shell and composer

- Added Ghostty progress signaling and an active-turn marker in the terminal title.
- Queued messages can now be restored directly into the composer with \`Up\`.
- Refined transcript, composer, pager, session explorer, and modal chrome.

## Runtime

- Upgraded the Pi runtime to 0.83 and OpenTUI to 0.4.5.
- Migrated authentication to provider-owned flows backed by Kit's persistent credential store.
- Added support for \`max\` thinking and refreshed authenticated model availability.

## Review reliability

- Made the working-tree review refresh pipeline self-healing after transient Git failures or deferred refreshes.
- Preserved the last good diff instead of replacing it with a misleading empty state.
`;
