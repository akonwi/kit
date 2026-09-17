import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor struct WorkspaceNavigationTests {
    @Test func splitSelectionMovementAndClose() {
        let workspace = WorkspaceState(demo: false)
        #expect(workspace.groups == [[.conversation]])
        workspace.open(.file("main.swift"))
        workspace.open(.agent("reviewer"))
        workspace.moveToOtherGroup(.file("main.swift"))
        #expect(workspace.groups == [[.conversation, .agent("reviewer")], [.file("main.swift")]])
        #expect(workspace.selections == [.agent("reviewer"), .file("main.swift")])
        #expect(workspace.isVisible(.agent("reviewer")))
        workspace.select(.conversation)
        workspace.open(.file("main.swift"))
        #expect(workspace.focusedGroup == 1)
        #expect(workspace.panes == [.conversation, .file("main.swift"), .agent("reviewer")])
        workspace.close(.file("main.swift"))
        #expect(workspace.groups == [[.conversation, .agent("reviewer")]])
        #expect(workspace.selections == [.conversation])
        workspace.close(.agent("reviewer"))
        workspace.close(.conversation)
        #expect(workspace.groups == [[.conversation]])
        #expect(workspace.selected == .conversation)
    }

    @Test func joiningPreservesDraftsAndAgentFirst() {
        let workspace = WorkspaceState(demo: false)
        workspace.open(.scratchpad)
        workspace.scratchpad = "Retained draft"
        workspace.moveToOtherGroup(.conversation)
        #expect(workspace.groups == [[.scratchpad], [.conversation]])
        workspace.joinGroups(selecting: .scratchpad)
        #expect(workspace.groups == [[.conversation, .scratchpad]])
        #expect(workspace.selected == .scratchpad)
        #expect(workspace.scratchpad == "Retained draft")
    }

    @Test func workspaceRendersSharedComposerAndNativeDivider() async throws {
        let store = SessionStore(fixture: try Fixture.load())
        store.ui.workspace.open(.scratchpad)
        store.ui.workspace.moveToOtherGroup(.scratchpad)
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let host = NSHostingView(rootView: WorkspaceLayout(state: store).environment(\.mica, theme).environment(\.colorScheme, .dark))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1100, height: 750), styleMask: [.titled], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<15 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(30)) }
        func splits(_ view: NSView) -> [NSSplitView] {
            (view as? NSSplitView).map { [$0] } ?? view.subviews.flatMap(splits)
        }
        let split = try #require(splits(host).first)
        #expect(split.subviews.count == 2)
        #expect(abs(split.subviews[0].frame.width - split.subviews[1].frame.width) < 2)
        split.setPosition(660, ofDividerAt: 0)
        try await Task.sleep(for: .milliseconds(100))
        #expect(store.ui.workspace.splitFraction > 0.59)
        func descendants(_ view: NSView) -> [NSView] { [view] + view.subviews.flatMap(descendants) }
        let editors = descendants(host).filter { String(describing: type(of: $0)) == "ForwardingScrollView" }
        store.ui.workspace.moveToOtherGroup(.scratchpad)
        host.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(100))
        let retained = Set(descendants(host).map(ObjectIdentifier.init))
        #expect(!editors.isEmpty)
        #expect(editors.allSatisfy { retained.contains(ObjectIdentifier($0)) })
        store.ui.workspace.moveToOtherGroup(.scratchpad)
        host.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(100))
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        try #require(bitmap.representation(using: .png, properties: [:])).write(to: URL(fileURLWithPath: "/tmp/kit-workspace-refresh.png"))
    }
}
