import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Composer reference ergonomics

- File and thread reference triggers now open after a brief grace period without leaving the initial \`@\` or \`#\` in the composer.
- Quickly typing \`@@\` or \`##\` cancels the reference picker and leaves one literal trigger without flashing the picker.
- Pending reference interactions now preserve cursor position and draft text safely across loading, focus changes, history, and cancellation.

## File review focus

- Selecting a file from the repository drawer now closes the drawer and immediately activates line, range, and whole-file comment interactions.
`;
