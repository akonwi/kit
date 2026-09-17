// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Kit",
    platforms: [.macOS(.v15)],
    products: [.executable(name: "Kit", targets: ["Kit"])],
    dependencies: [
        .package(url: "https://github.com/swiftlang/swift-markdown.git", exact: "0.8.0"),
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter.git", exact: "0.25.0"),
        .package(url: "https://github.com/CodeEditApp/CodeEditSourceEditor.git", exact: "0.15.2"),
        .package(url: "https://github.com/CodeEditApp/CodeEditLanguages.git", exact: "0.1.20")
    ],
    targets: [
        .executableTarget(name: "Kit", dependencies: [
            .product(name: "Markdown", package: "swift-markdown"),
            .product(name: "SwiftTreeSitter", package: "SwiftTreeSitter"),
            .product(name: "CodeEditSourceEditor", package: "CodeEditSourceEditor"),
            .product(name: "CodeEditLanguages", package: "CodeEditLanguages")
        ], resources: [.process("Resources")]),
        .testTarget(name: "KitTests", dependencies: ["Kit"])
    ]
)
