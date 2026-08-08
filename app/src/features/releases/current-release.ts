import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Mermaid diagrams

- Render simple fenced \`mermaid\` diagrams as terminal-native Unicode.
- Open complex diagrams in a workspace preview with zooming, panning, and system-app export.
- Render locally with bounded workers, resource limits, caching, cancellation, and source fallback.

## Headless RPC mode

- Added \`kit --mode rpc\` for controlling Kit as a long-lived JSON-lines subprocess.
- Supports prompts, steering, follow-ups, cancellation, sessions, model selection, thinking levels, and streaming lifecycle events.
- Added persistent, existing-session, and ephemeral \`--no-session\` operation.

## Scratchpad

- Exposed each session scratchpad as a normal Markdown file editable through the standard \`read\`, \`edit\`, and \`write\` tools.
- Kept panel edits and agent file changes synchronized with atomic persistence and conflict protection.

## Code review

- Saved review comments now appear immediately as composer attachments.
- Pressing \`s\` submits the attached review immediately.
- Preserved newer drafts and attachments when submissions overlap or fail.

## External plugins: migration required

- Replaced direct TypeScript plugins with the language-neutral JSON-RPC v1 protocol over stdio.
- External plugins now use a \`plugin.json\` manifest and may be implemented in any language.
- Added commands, tools, chrome contributions, sub-agents, system prompts, events, UI requests, cancellation, and tool interception to the protocol.
- Removed the legacy \`*.ts\` loader and \`@akonwi/kit/plugin\` export.
- See \`docs/features/plugins.md\` and \`docs/plugin-protocol/v1.md\` for migration instructions.

## UI improvements

- Confirmation-dialog messages now render Markdown, including formatted code blocks.
- Fixed clickable plugin header and footer contributions.
`;
