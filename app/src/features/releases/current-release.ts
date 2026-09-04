import { version } from "../../../package.json";

export const CURRENT_VERSION = version;

/** Release notes bundled into the binary so the installed release is readable offline. */
export const CURRENT_RELEASE_NOTES = `
## Image previews

- Agents can now present generated or captured PNG, JPEG, WebP, and GIF images directly in the transcript.
- Selecting a presented image or image attachment opens it in a dedicated workspace pane with zoom, pan, reset, close, and native-app controls.
- Image previews validate file type, dimensions, and size before rendering.

## Provider updates

- Updated the Pi provider runtime and bundled model catalogs to 0.85.0.
- Includes expanded provider and model support plus improvements to reasoning, tool calls, streaming, retries, and authentication.
`;
