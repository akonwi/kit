import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Headless modes

- Added persistent sessions to print mode, including support for continuing an existing session with \`--session\`.
- Print mode now creates and saves a session by default; use \`--no-session\` for an in-memory run.
- Added exact provider/model selection to print mode with \`--model <provider>/<model-id>\`.
- Replaced \`--mode rpc\` with the dedicated \`--rpc\` flag and made interactive, print, and RPC modes mutually exclusive.

## Pull requests

- Made pull request details in the footer clickable so the active pull request opens directly in the browser.
`;
