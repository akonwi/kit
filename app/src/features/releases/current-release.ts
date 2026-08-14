import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Web code review

- Added a responsive code-review surface to \`kit --web\` for reviewing working-tree changes from desktop and mobile browsers.
- Browse changed files, switch between unified and split diffs, wrap long lines, and inspect file-level change metadata.
- Add, edit, and delete local line or range notes, then submit them as structured review feedback to the Kit agent.
- Added guarded review submission with stale-session validation, payload limits, idempotent retries, and race protection.

## TUI mouse interactions

- Made the session title, model, and thinking level in the header clickable to open their existing commands.
- Added pointer cursors and release-based activation for actionable header and footer contributions.
- Prevented drag activation and click-through while overlays or pickers are open.
`;
