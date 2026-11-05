// swift-tools-version: 6.0
import PackageDescription

// The tree-sitter grammars the iOS app highlights code with. The macOS app
// gets its grammars from CodeEditLanguages, which ships only for macOS.
let package = Package(
    name: "KitGrammars",
    platforms: [.macOS(.v15), .iOS(.v18)],
    products: [
        .library(name: "KitGrammars", targets: ["KitGrammars"])
    ],
    dependencies: [
        .package(path: "../../apple/KitShared"),
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter.git", exact: "0.25.0"),
        .package(url: "https://github.com/tree-sitter/tree-sitter-bash.git", exact: "0.25.1"),
        .package(url: "https://github.com/tree-sitter/tree-sitter-go.git", exact: "0.25.0"),
        .package(url: "https://github.com/tree-sitter/tree-sitter-json.git", exact: "0.24.8"),
        .package(url: "https://github.com/tree-sitter/tree-sitter-python.git", exact: "0.23.6"),
        .package(url: "https://github.com/tree-sitter/tree-sitter-javascript.git", exact: "0.23.1"),
        .package(url: "https://github.com/tree-sitter/tree-sitter-typescript.git", exact: "0.23.2"),
        .package(url: "https://github.com/alex-pinkus/tree-sitter-swift.git", exact: "0.7.4-with-generated-files"),
        .package(url: "https://github.com/tree-sitter/tree-sitter-ruby.git", exact: "0.23.1"),
        .package(url: "https://github.com/tree-sitter/tree-sitter-css.git", exact: "0.23.2"),
        // Untagged: pinned to the commit that added its Swift package.
        .package(url: "https://github.com/akonwi/tree-sitter-ard.git", revision: "cff3da3fe80584e8f506c9867fbf17e1eaa7b76c")
    ],
    targets: [
        .target(name: "KitGrammars", dependencies: [
            .product(name: "KitHighlighting", package: "KitShared"),
            .product(name: "SwiftTreeSitter", package: "SwiftTreeSitter"),
            .product(name: "TreeSitterBash", package: "tree-sitter-bash"),
            .product(name: "TreeSitterGo", package: "tree-sitter-go"),
            .product(name: "TreeSitterJSON", package: "tree-sitter-json"),
            .product(name: "TreeSitterPython", package: "tree-sitter-python"),
            .product(name: "TreeSitterJavaScript", package: "tree-sitter-javascript"),
            .product(name: "TreeSitterTypeScript", package: "tree-sitter-typescript"),
            .product(name: "TreeSitterSwift", package: "tree-sitter-swift"),
            .product(name: "TreeSitterRuby", package: "tree-sitter-ruby"),
            .product(name: "TreeSitterCSS", package: "tree-sitter-css"),
            .product(name: "TreeSitterArd", package: "tree-sitter-ard")
        ]),
        .testTarget(name: "KitGrammarsTests", dependencies: [
            "KitGrammars",
            .product(name: "KitHighlighting", package: "KitShared")
        ])
    ]
)
