# ADR 0013: Native source editing and code highlighting

Status: Accepted

## Decision

Use CodeEditSourceEditor as the selected foundation for the native macOS file
pane, behind a Kit-owned document adapter. The initial pane may be read-only;
editing uses the same component. Adoption must preserve server ownership of file
loading/saving and explicitly handle external text changes, undo, conflicts,
selection, and view lifecycle.

Use SwiftTreeSitter directly for Markdown code-block highlighting. Swift Markdown
owns document parsing; fenced source code is passed to a registered Tree-sitter
grammar and its highlighting queries. Kit maps named captures to its existing
`syntaxPalette` roles and renders native attributed text. Code blocks retain
content-driven height, text selection, and copying without embedding an editor.

Pin grammar versions and ship their query resources with the app. Use explicit
language aliases and plain-text fallback for unsupported or unlabeled fences.
Parsing/highlight failure must preserve all source text. Cache theme-independent
capture ranges and apply the active theme when rendering.

## Rationale

The file pane needs an eventual editing path with native selection, undo, and
editor layout. CodeEditSourceEditor provides that foundation with configurable
chrome. Transcript code blocks need a smaller rendering boundary and control over
syntax colors, without per-block editor state or nested editing behavior.

Kit's function, variable, operator, and other syntax roles must remain distinct.
The CodeEditSourceEditor integration requires extending its stock capture-to-theme
mapping, choosing a policy for custom grammar registration, and resolving its
resource build requirements. Shared text storage is the intended integration
point for external document updates; its String binding is not a document reload
API. Selection updates must also be verified against the selected release.

## Consequences

The two surfaces can share grammar/query conventions and semantic theme roles,
but do not need to share a text view. The macOS client uses CodeEditLanguages as the
shared grammar resource provider, with CodeEditSourceEditor behind a read-only
snapshot adapter in the file pane. Production editing remains gated on the
integration prerequisites above. The selected direction is not a
claim of production readiness or full grammar/theme parity.

The [client implementation reference](../design/macos-client.md) records editor
integration constraints. The [macOS design reference](../design/macos-design-language.md)
governs presentation.
