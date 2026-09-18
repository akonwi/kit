import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor @Suite(.serialized) struct AnnotationPresentationTests {
    @Test func renderDesignSpecimen() async throws {
        let note = try FileAnnotation(id: 1, anchor: .init(kind: .value0, workspaceFile: .init(
            workspaceId: "workspace_preview", path: "Sources/Kit/SessionStore.swift", fileRevision: "file_preview",
            startLine: 41, endLine: 43), workingTreeDiff: nil), body: "Preserve the draft when the server cannot confirm acceptance.",
            source: "func send() {\n    submit(draft)\n}", stale: false, complete: true)
        for dark in [false, true] {
            let theme = MicaTheme(dark: dark)
            let host = NSHostingView(rootView: AnnotationDesignPreview(note: note)
                .environment(\.mica, theme).environment(\.colorScheme, dark ? .dark : .light))
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 720, height: 800),
                styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false; window.contentView = host; window.orderFront(nil)
            defer { window.close() }
            try await Task.sleep(for: .milliseconds(250))
            host.layoutSubtreeIfNeeded()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            #expect(bitmap.pixelsWide >= 720)
            #expect(bitmap.pixelsHigh >= 600)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-annotation-\(dark ? "dark" : "light").png"))
        }
    }
}
