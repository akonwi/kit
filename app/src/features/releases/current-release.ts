import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Code review

- Corrected long-line wrapping in unified, split, and raw diff views.
- Removed blank rows caused by parser-added line endings and kept ordinary diff lines at single-row height.
- Improved resize behavior, scrollbar-safe sizing, full-width content, and clickable comment markers.

## Ard syntax highlighting

- Updated the bundled Ard Tree-sitter grammar and highlight queries.
- Added support for newer Ard syntax including \`defer\`, \`select\`, \`unsafe\`, rune literals, brace escapes, extern bindings, and \`!=\`.
`;
