// swift-tools-version: 6.2
import PackageDescription

// Spike: can SwiftUI + AppKit fully realize the Paper "Kit App" design, with
// libghostty terminals and WKWebView browsers living inside native tiles?
// Throwaway; not part of the shipped app.
let package = Package(
    name: "KitNativeSpike",
    platforms: [.macOS(.v15)],
    products: [.executable(name: "KitNativeSpike", targets: ["KitNativeSpike"])],
    targets: [
        // Built by script/build_ghosttykit.sh; not committed.
        .binaryTarget(name: "GhosttyKit", path: "Vendor/GhosttyKit.xcframework"),
        .executableTarget(
            name: "KitNativeSpike",
            dependencies: ["GhosttyKit"],
            swiftSettings: [.defaultIsolation(MainActor.self)],
            linkerSettings: [
                .linkedFramework("AppKit"),
                .linkedFramework("Carbon"),
                .linkedFramework("CoreText"),
                .linkedFramework("CoreVideo"),
                .linkedFramework("IOSurface"),
                .linkedFramework("Metal"),
                .linkedFramework("QuartzCore"),
                .linkedFramework("UserNotifications"),
                .linkedFramework("WebKit"),
                .linkedLibrary("c++"),
            ]
        ),
    ],
    swiftLanguageModes: [.v5]
)
