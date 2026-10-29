// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Kit",
    platforms: [.macOS(.v15)],
    products: [.executable(name: "Kit", targets: ["Kit"])],
    dependencies: [
        .package(path: "Vendor/CodeEditTextView"),
        .package(url: "https://github.com/swiftlang/swift-markdown.git", exact: "0.8.0"),
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter.git", exact: "0.25.0"),
        .package(path: "Vendor/CodeEditSourceEditor"),
        .package(url: "https://github.com/CodeEditApp/CodeEditLanguages.git", exact: "0.1.20"),
        .package(url: "https://github.com/apple/swift-openapi-generator.git", exact: "1.13.1"),
        .package(url: "https://github.com/apple/swift-openapi-runtime.git", exact: "1.12.1"),
        .package(url: "https://github.com/apple/swift-http-types.git", exact: "1.8.0")
    ],
    targets: [
        .executableTarget(name: "Kit", dependencies: [
            .product(name: "CodeEditTextView", package: "CodeEditTextView"),
            .product(name: "Markdown", package: "swift-markdown"),
            .product(name: "SwiftTreeSitter", package: "SwiftTreeSitter"),
            .product(name: "CodeEditSourceEditor", package: "CodeEditSourceEditor"),
            .product(name: "CodeEditLanguages", package: "CodeEditLanguages"),
            .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
            .product(name: "HTTPTypes", package: "swift-http-types"),
            .product(name: "HTTPTypesFoundation", package: "swift-http-types"),
            "GhosttyKit",
            "KitMermaid",
            "KitFigures"
        ], exclude: ["openapi-generator-config.yaml"], resources: [.process("Resources")],
        linkerSettings: [
            .linkedFramework("Carbon"),
            .linkedFramework("CoreText"),
            .linkedFramework("CoreVideo"),
            .linkedFramework("IOSurface"),
            .linkedFramework("Metal"),
            .linkedFramework("QuartzCore"),
            .linkedFramework("UserNotifications"),
            .linkedLibrary("c++")
        ]),
        // libghostty, built by script/build_ghosttykit.sh at a pinned commit.
        .binaryTarget(name: "GhosttyKit", path: "Vendor/Ghostty/GhosttyKit.xcframework"),
        // Kit's Mermaid renderer, ported from the terminal client's (ADR 0046).
        .target(name: "KitMermaid"),
        // Kit's data figures, ported from the terminal client's (ADR 0054).
        .target(name: "KitFigures"),
        .testTarget(name: "KitTests", dependencies: ["Kit", "KitMermaid", "KitFigures"])
    ]
)
