import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Model interaction dock

- Confirmations, text input, selections, and guided questions now appear in a bottom dock instead of covering the transcript with a modal.
- The transcript remains visible and scrollable while an interaction is active, and composer drafts and cursor position are restored afterward.
- Interaction requests are queued safely with consistent cancellation, focus, and keyboard behavior.

## Transcript clarity

- Assistant reasoning now remains visible in the main transcript throughout tool use instead of appearing only inside Activity.
- Tool calls and results remain consolidated in Activity drawers without changing the transcript structure when a turn completes.

## Review and remote foundations

- Clicking an unchanged section in Code Review now expands it directly.
- Extracted shared protocol, host, and session-client contracts to keep terminal and web clients aligned as remote-session support evolves.
`;
