import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Code review feedback

- Removed the generic introductory sentence from submitted code-review feedback so agents receive the structured file and line comments directly.
`;
