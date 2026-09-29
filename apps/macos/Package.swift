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
            .product(name: "HTTPTypesFoundation", package: "swift-http-types")
        ], exclude: ["openapi-generator-config.yaml"], resources: [.process("Resources")]),
        .testTarget(name: "KitTests", dependencies: ["Kit"])
    ]
)
