# KitShared

Swift code shared by Kit's Apple clients, the macOS app (`apps/macos`) and the
iOS app. It holds no AppKit or UIKit code and depends on nothing beyond
Foundation.

- `KitMermaid` draws Mermaid diagrams as text, a port of the terminal client's
  renderer ([ADR 0046](../../../docs/adrs/0046-draw-mermaid-diagrams-with-kits-own-renderer.md)).
- `KitFigures` reads and lays out Kit's data figures, a port of the terminal
  client's ([ADR 0054](../../../docs/adrs/0054-draw-data-figures-from-fenced-markdown.md)).

The macOS app's tests cover these modules (`MermaidFixtureTests`,
`FigureFixtureTests`, and their presentation tests); run them as the macOS
README describes.
