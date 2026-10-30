# KitShared

Swift code shared by Kit's Apple clients, the macOS app (`apps/macos`) and the
iOS app. It holds no AppKit or UIKit code.

- `KitClient` is the session client: the generated OpenAPI transport and types
  in `GeneratedSources`, and the projections, validation, pairing, and device
  tokens both apps build on. Regenerate the transport after changing
  `api/kit-session.openapi.json`:

  ```sh
  apps/apple/KitShared/script/generate_openapi.sh
  apps/apple/KitShared/script/generate_openapi.sh --check
  ```

  Connecting to a server over SSH runs the system `ssh`, so that connector is
  compiled only for macOS; elsewhere an SSH server reports that it can only be
  reached from a Mac. See [`docs/api-development.md`](../../../docs/api-development.md).
- `KitMermaid` draws Mermaid diagrams as text, a port of the terminal client's
  renderer ([ADR 0046](../../../docs/adrs/0046-draw-mermaid-diagrams-with-kits-own-renderer.md)).
- `KitFigures` reads and lays out Kit's data figures, a port of the terminal
  client's ([ADR 0054](../../../docs/adrs/0054-draw-data-figures-from-fenced-markdown.md)).
- `KitTranscript` is what a transcript shows, apart from how each app draws
  it: the Markdown projection (`MarkdownContent`), tool call presentation
  (`ToolPresentation`), and session mention links (`SessionMentions`).

The macOS app's tests cover these modules (client, Mermaid, figure, Markdown, and tool presentation tests);
run them as the macOS README describes. Check that the package still builds
for iOS with:

```sh
cd apps/apple/KitShared
xcodebuild -scheme KitShared-Package -destination 'generic/platform=iOS Simulator' build
```
