// swift-tools-version: 6.0
import PackageDescription

// Code shared by Kit's Apple clients: the macOS app (apps/macos) and the
// iOS app (apps/ios). It holds no AppKit or UIKit code.
let package = Package(
    name: "KitShared",
    platforms: [.macOS(.v15), .iOS(.v18)],
    products: [
        .library(name: "KitMermaid", targets: ["KitMermaid"]),
        .library(name: "KitFigures", targets: ["KitFigures"])
    ],
    targets: [
        // Kit's Mermaid renderer, ported from the terminal client's (ADR 0046).
        .target(name: "KitMermaid"),
        // Kit's data figures, ported from the terminal client's (ADR 0054).
        .target(name: "KitFigures")
    ]
)
