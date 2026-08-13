import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Remote browser mode

- Added \`kit --web\` for controlling a Kit session from desktop and mobile browsers.
- Included a responsive transcript, composer, scratchpad, command palette, model controls, attachments, guided interactions, plugin chrome, and multi-client synchronization.
- Added HTTP Basic authentication and explicit Host/Origin allowlists for trusted remote access through HTTPS tunnels or reverse proxies.

## Sessions and headless modes

- Aligned interactive, print, RPC, and web startup behavior: each mode resumes the latest session for the current directory by default.
- Standardized \`--session\` and \`--no-session\` so conversations, scratchpads, handoffs, and sub-agent state can remain fully in memory.
- Added user and project external-plugin support to print and RPC modes, including tools, interceptors, sub-agents, system prompts, and lifecycle events.

## Release notes

- Added a reverse-chronological release history with publication dates and incremental pagination.
- Available updates now display their actual GitHub release notes, and release headings can open on GitHub.

## Scratchpads

- Added a session-scoped \`edit_scratchpad\` tool so scratchpad edits continue targeting the active session after handoffs.
`;
