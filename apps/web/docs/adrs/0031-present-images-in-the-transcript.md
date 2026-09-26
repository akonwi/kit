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
- persists the validated bytes as a durable session attachment
- returns only a bounded success or error message as model-facing tool content
- marks typed, non-model-facing details as an explicit transcript-image presentation referencing the attachment

The terminal transcript promotes successful `show_image` results out of the consolidated Activity drawer and renders them below the drawer entry. The newest completed call begins expanded and replaces any previously expanded preview. Restored history begins collapsed so opening an image-heavy session does not eagerly decode every image.

At most one preview is expanded at a time. Expanded previews reserve a fixed-height row before decoding, use `fit` sizing, and leave protocol selection on `auto`. Collapsing a preview unmounts the image renderable and releases its native image. Clicking an expanded preview opens the persisted image in a workspace pane rather than launching a native application. The pane follows the same zoom, pan, focus, and close conventions as Mermaid previews; opening the original in a native viewer remains an explicit pane action when a source path is available.

User image-attachment rows use the same image workspace pane when opened from the main transcript.

Transcript projection derives the presented image from validated `show_image` details rather than from model-facing tool content.

Kit separately provides `inspect_image` for cases where the agent must reason about a local image. It accepts a local path, applies the same byte, dimension, pixel, format, and structural validation as user image inputs, persists the validated attachment, and returns a standard model-visible image tool-result block. It does not set the transcript-image presentation marker and is advertised only when the selected model supports image input.

Provider-neutral context estimation assigns each model-visible image a bounded conservative modality allowance instead of treating Base64 transport length as text. Exact provider measurement may replace that fallback. Other tools may continue returning standard image blocks; they are not promoted into the main transcript without an explicit presentation contract.

`show_image` is available when Kit owns the attachment-backed session transcript. Clients consume its durable presentation marker independently of whether the selected model supports image input.

Kit does not parse assistant prose for image-looking file paths. Mentioned paths may become explicit references in a separate feature, but they do not trigger image rendering or filesystem access.

## Consequences

- agents can intentionally present screenshots and generated raster images
- image bytes are persisted with the tool result, so transcript and workspace previews do not depend on the source file remaining available
- agents opt into image context only by calling `inspect_image` or receiving a user image input
- presenting an image does not silently add image content to model context
- model-facing tool calls and results remain bounded because `show_image` accepts paths and returns text only
- terminal rendering degrades through OpenTUI's portable block protocol when native graphics are unavailable
- `inspect_image` uses the stricter model-input limits while `show_image` retains display-oriented limits
- BMP, SVG, and malformed or oversized files are rejected by both image tools

## Related

- `docs/adrs/0003-custom-shell.md`
- `docs/adrs/0014-defer-inline-transcript-images.md`
