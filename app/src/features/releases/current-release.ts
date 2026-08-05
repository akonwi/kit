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

## External plugins: migration required

- External plugins now use the language-neutral JSON-RPC v1 protocol over stdio. The previous direct TypeScript loader and \`@akonwi/kit/plugin\` package export have been removed.
- Move each \`~/.kit/plugins/name.ts\` or \`<project>/.kit/plugins/name.ts\` plugin into its own installation directory containing \`plugin.json\`, then point the manifest's stdio \`command\` and \`args\` at your plugin process.
- The process must reserve stdout for UTF-8 newline-delimited JSON-RPC. Write diagnostics to stderr, answer \`initialize\` with \`{ "protocolVersion": 1 }\`, and handle \`shutdown\`.
- Replace PluginAPI registrations with the corresponding \`kit/commands/*\`, \`kit/tools/*\`, chrome, subagent, system-prompt, UI, and interception methods. Plugins receive public events as \`kit/events/<event-name>\` notifications without subscribing.
- Command and contribution ids are plugin-local and become \`<plugin-id>.<local-id>\` canonically. Slash commands are presented by local id, while keybindings use the canonical id. Tool names become \`<plugin-id>__<local_id>\` for the model.
- Kit no longer installs or bundles plugin dependencies. The manifest command must already be runnable. See \`docs/features/plugins.md\` for setup and \`docs/plugin-protocol/v1.md\` plus the bundled JSON Schemas for the complete contract.
`;
