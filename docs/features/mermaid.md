# Mermaid diagrams

Kit renders fenced `mermaid` Markdown blocks as terminal-native Unicode when they fit the automatic inline budget. The shared Markdown renderer applies this behavior consistently across transcripts, pending output, pager content, release notes, and confirmation dialogs.

## Inline rendering

Inline diagrams use the dedicated ASCII preset from pinned `@mermanjs/web` in a bounded worker. Rendering is cancellable and guarded by source, graph-complexity, queue, timeout, and output-geometry limits. Incomplete streaming fences and failed, malformed, unsupported, or oversized output preserve the original source.

Diagram output is never wrapped because wrapping breaks its geometry.

## Visual fallback

A complete diagram that exceeds the inline source or graph-complexity budget shows its source with an **Open diagram** action. The action opens a temporary workspace panel rather than silently increasing automatic rendering limits.

The visual preview is produced locally and without network access:

1. Merman's render-only WASM preset creates a bounded, `resvg-safe` SVG.
2. `resvg-wasm` rasterizes the SVG to a bounded PNG using the active Kit theme and a bundled Inter font.
3. OpenTUI's image component displays the PNG using its automatic Kitty, Sixel, or Unicode-block protocol selection.

The panel supports zooming, two-dimensional panning, and opening the inert PNG through the system's default application. It does not depend on Glimpse or a remote Mermaid service.

## Safety boundaries

Automatic and user-initiated rendering remain isolated in workers with separate limits. Visual rendering rejects external image references and bounds source bytes, graph size, SVG bytes, image dimensions, pixel count, PNG bytes, and execution time. Generated external previews are private files under the operating system's temporary directory.
