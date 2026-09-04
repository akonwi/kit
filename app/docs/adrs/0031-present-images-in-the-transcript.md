# 0031: Present explicit images in the transcript

## Status
Accepted

## Context

Kit already carries image content in user messages and tool results, while the terminal transcript renders user images as summary rows and ignores image blocks in tool results. ADR 0014 deferred inline rendering until the shell had renderer-aware image sizing, terminal capability handling, and safe transcript composition.

OpenTUI now provides an image renderable with explicit layout dimensions and automatic Kitty, Sixel, and Unicode-block protocol selection. Kit already uses this renderable for Mermaid previews.

Agents also need an intentional way to present generated artifacts such as browser screenshots. Inferring image paths from assistant prose would be ambiguous and could cause surprising filesystem reads.

## Decision

Kit provides a built-in `show_image` tool that accepts a local file path and an optional short caption. It does not accept raw or base64 data.

The tool:

- resolves relative paths against the active session working directory
- reads and validates PNG, JPEG, WebP, or GIF bytes
- enforces encoded-byte and decoded-pixel limits
- returns the validated bytes as a standard image tool-result block
- marks its typed details as an explicit transcript-image presentation

The terminal transcript promotes successful `show_image` results out of the consolidated Activity drawer and renders them below the drawer entry. The newest completed call begins expanded and replaces any previously expanded preview. Restored history begins collapsed so opening an image-heavy session does not eagerly decode every image.

At most one preview is expanded at a time. Expanded previews reserve a fixed-height row before decoding, use `fit` sizing, and leave protocol selection on `auto`. Collapsing a preview unmounts the image renderable and releases its native image. Clicking an expanded preview opens the persisted image in a workspace pane rather than launching a native application. The pane follows the same zoom, pan, focus, and close conventions as Mermaid previews; opening the original in a native viewer remains an explicit pane action when a source path is available.

User image-attachment rows use the same image workspace pane when opened from the main transcript.

Other tools may continue returning standard image blocks. They are not promoted into the main transcript unless they opt into a future explicit presentation contract.

`show_image` is available only when Kit owns an interactive transcript. Headless print and RPC hosts exclude it until their clients have a bounded media retrieval and presentation contract.

Kit does not parse assistant prose for image-looking file paths. Mentioned paths may become explicit references in a separate feature, but they do not trigger image rendering or filesystem access.

## Consequences

- agents can intentionally present screenshots and generated raster images
- image bytes are persisted with the tool result, so transcript and workspace previews do not depend on the source file remaining available
- image inspection stays inside Kit by default while retaining an explicit external-open action
- model-facing tool calls remain small because `show_image` accepts paths only
- terminal rendering degrades through OpenTUI's portable block protocol when native graphics are unavailable
- headless hosts do not advertise `show_image`, avoiding large inline image records and a presentation capability they cannot fulfill
- BMP, SVG, and malformed or oversized files are rejected by `show_image`

## Related

- `docs/adrs/0003-custom-shell.md`
- `docs/adrs/0014-defer-inline-transcript-images.md`
