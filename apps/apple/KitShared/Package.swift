// swift-tools-version: 6.0
import PackageDescription

// Code shared by Kit's Apple clients: the macOS app (apps/macos) and the
// iOS app (apps/ios). It holds no AppKit or UIKit code.
let package = Package(
    name: "KitShared",
    platforms: [.macOS(.v15), .iOS(.v18)],
    products: [
        .library(name: "KitClient", targets: ["KitClient"]),
        .library(name: "KitMermaid", targets: ["KitMermaid"]),
        .library(name: "KitMermaidScene", targets: ["KitMermaidScene"]),
        .library(name: "KitFigures", targets: ["KitFigures"]),
        .library(name: "KitTranscript", targets: ["KitTranscript"]),
        .library(name: "KitHighlighting", targets: ["KitHighlighting"]),
        .library(name: "KitArdGrammar", targets: ["KitArdGrammar"])
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-openapi-generator.git", exact: "1.13.1"),
        .package(url: "https://github.com/apple/swift-openapi-runtime.git", exact: "1.12.1"),
        .package(url: "https://github.com/apple/swift-http-types.git", exact: "1.8.0"),
        .package(url: "https://github.com/swiftlang/swift-markdown.git", exact: "0.8.0"),
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter.git", exact: "0.25.0"),
        // Untagged: pinned to the commit that added its Swift package.
        .package(url: "https://github.com/akonwi/tree-sitter-ard.git", revision: "cff3da3fe80584e8f506c9867fbf17e1eaa7b76c")
    ],
    targets: [
        // The session client: the generated OpenAPI transport and the
        // projections both apps build on. The server stays authoritative.
        .target(name: "KitClient", dependencies: [
            .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
            .product(name: "HTTPTypes", package: "swift-http-types"),
            .product(name: "HTTPTypesFoundation", package: "swift-http-types")
        ], exclude: ["openapi-generator-config.yaml"]),
        // Kit's Mermaid renderer, ported from the terminal client's (ADR 0046).
        .target(name: "KitMermaid"),
        // A Mermaid layout as shapes, strokes and text to draw: the geometry
        // both apps draw from. Each app supplies its own fonts and colors.
        .target(name: "KitMermaidScene", dependencies: ["KitMermaid"]),
        // Kit's data figures, ported from the terminal client's (ADR 0054).
        .target(name: "KitFigures"),
        // What a transcript shows, apart from how a platform draws it: the
        // Markdown projection, tool call presentation, and session mention links.
        .target(name: "KitTranscript", dependencies: [
            "KitClient", "KitMermaid", "KitFigures",
            .product(name: "Markdown", package: "swift-markdown")
        ]),
        // Code highlighting with tree-sitter. Each app supplies the grammars
        // it bundles through `SyntaxGrammars`.
        .target(name: "KitHighlighting", dependencies: [
            .product(name: "SwiftTreeSitter", package: "SwiftTreeSitter")
        ]),
        // Ard's grammar from tree-sitter-ard, which both apps bundle. Apart
        // from KitHighlighting so only the apps that color Ard link it.
        .target(name: "KitArdGrammar", dependencies: [
            "KitHighlighting",
            .product(name: "SwiftTreeSitter", package: "SwiftTreeSitter"),
            .product(name: "TreeSitterArd", package: "tree-sitter-ard")
        ])
    ]
)
