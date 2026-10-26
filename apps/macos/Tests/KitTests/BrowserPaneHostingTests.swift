import AppKit
import SwiftUI
import Testing
@testable import Kit

/// A session's workspace shown in a window, with fake browser engines, as
/// its session tab is shown, hidden, and shown again.
@MainActor private final class BrowserWorkspaceHarness {
    let store: SessionStore
    let engines = FakeBrowserEngines()
    let window: NSWindow
    private let host: NSHostingView<AnyView>
    var workspace: WorkspaceState { store.ui.workspace }

    init() throws {
        store = SessionStore(fixture: try Fixture.load())
        store.ui.workspace.makeBrowserEngine = engines.make
        host = NSHostingView(rootView: AnyView(Color.clear))
        window = FocusIsolatedWindow(contentRect: NSRect(x: 0, y: 0, width: 1200, height: 600), styleMask: [.borderless],
                          backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
    }

    /// Shows the session's workspace, as when its session tab is selected.
    func showSession() async throws {
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        host.rootView = AnyView(WorkspaceLayout(state: store).environment(\.mica, theme))
        try await settle()
    }

    /// Shows something else, as when another session tab is selected.
    func hideSession() async throws {
        host.rootView = AnyView(Color.clear)
        try await settle()
    }

    func settle() async throws {
        for _ in 0..<8 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }
    }

    /// The pages in the window, in the order their tabs were opened.
    func pagesInWindow(_ tabs: [BrowserTab]) -> [Bool] {
        tabs.map { tab in engines.made.first { $0.state.url == tab.url }?.view.window === window }
    }

    /// The address fields on screen.
    var addressFields: [NSTextField] {
        func descendants(_ view: NSView) -> [NSView] { [view] + view.subviews.flatMap(descendants) }
        return descendants(host).compactMap { $0 as? NSTextField }.filter(\.isEditable)
    }

    func close() { window.close() }
}

@MainActor struct BrowserPaneHostingTests {
    @Test func onlyTheTabsOnScreenPutTheirPagesInTheWindow() async throws {
        let harness = try BrowserWorkspaceHarness()
        defer { harness.close() }
        let workspace = harness.workspace
        let tabs = (0..<4).map { workspace.openBrowser(URL(string: "https://example.com/\($0)")!) }
        try await harness.showSession()
        #expect(harness.pagesInWindow(tabs) == [false, false, false, true])
        #expect(harness.engines.made.allSatisfy { !$0.closed })

        workspace.select(.browser(tabs[1].id))
        try await harness.settle()
        #expect(harness.pagesInWindow(tabs) == [false, true, false, false])
        // Showing a tab again shows its page as it was: no new engine or load.
        #expect(harness.engines.made.count == 4)
        #expect(harness.engines.made.map(\.loads.count) == [1, 1, 1, 1])
        #expect(harness.engines.made.map(\.reloads) == [0, 0, 0, 0])
    }

    @Test func switchingBackToASessionKeepsItsPagesAndShowsOnlyTheVisibleOnes() async throws {
        let harness = try BrowserWorkspaceHarness()
        defer { harness.close() }
        let workspace = harness.workspace
        let tabs = (0..<6).map { workspace.openBrowser(URL(string: "https://example.com/\($0)")!) }
        workspace.split(.browser(tabs[5].id), .right)
        workspace.select(.browser(tabs[2].id))
        try await harness.showSession()
        let engines = harness.engines.made.map(ObjectIdentifier.init)
        let views = harness.engines.made.map { ObjectIdentifier($0.view) }
        #expect(harness.pagesInWindow(tabs) == [false, false, true, false, false, true])

        try await harness.hideSession()
        #expect(harness.pagesInWindow(tabs) == [false, false, false, false, false, false])
        try await harness.showSession()

        #expect(harness.pagesInWindow(tabs) == [false, false, true, false, false, true])
        #expect(harness.engines.made.map(ObjectIdentifier.init) == engines)
        #expect(harness.engines.made.map { ObjectIdentifier($0.view) } == views)
        #expect(harness.engines.made.map(\.loads.count) == [1, 1, 1, 1, 1, 1])
        #expect(harness.engines.made.map(\.reloads) == [0, 0, 0, 0, 0, 0])
        #expect(harness.engines.made.allSatisfy { !$0.closed })
        #expect(tabs.map(\.url) == (0..<6).map { URL(string: "https://example.com/\($0)")! })
        #expect(workspace.panes.compactMap(workspace.browser).map(\.id) == tabs.map(\.id))
    }

    @Test func aRestoredTabLoadsItsPageWhenFirstShown() async throws {
        let harness = try BrowserWorkspaceHarness()
        defer { harness.close() }
        let workspace = harness.workspace
        let shown = "browser:" + UUID().uuidString.lowercased()
        let hidden = "browser:" + UUID().uuidString.lowercased()
        workspace.restore(TileArrangement(root: .tile(.init(panes: ["conversation", shown, hidden], selected: shown)),
                                          focused: 0, zoomed: false,
                                          browsers: [shown: "https://example.com/shown", hidden: "https://example.com/hidden"]))
        try await harness.showSession()
        // Loads start after the view update that shows the page.
        try await harness.settle()
        #expect(harness.engines.made.map(\.loads) == [[URL(string: "https://example.com/shown")!]])

        let tab = try #require(workspace.panes.compactMap(workspace.browser).last)
        workspace.select(.browser(tab.id))
        try await harness.settle()
        #expect(harness.engines.made.map(\.loads) == [[URL(string: "https://example.com/shown")!],
                                                      [URL(string: "https://example.com/hidden")!]])
        #expect(harness.engines.made[1].view.window === harness.window)
    }
}

@MainActor struct BrowserAddressDraftTests {
    @Test func anAddressTypedInABlankTabIsKeptWhileTheTabIsHidden() async throws {
        let harness = try BrowserWorkspaceHarness()
        defer { harness.close() }
        let workspace = harness.workspace
        let page = workspace.openBrowser(URL(string: "https://example.com/page")!)
        let blank = workspace.openBrowser()
        try await harness.showSession()
        let field = try #require(harness.addressFields.first)
        #expect(harness.window.makeFirstResponder(field))
        let editor = try #require(field.currentEditor() as? NSTextView)
        editor.insertText("localhost:5173", replacementRange: editor.selectedRange())
        try await harness.settle()

        workspace.select(.browser(page.id))
        try await harness.settle()
        workspace.select(.browser(blank.id))
        try await harness.settle()
        #expect(harness.addressFields.map(\.stringValue) == ["localhost:5173"])
    }
}

@MainActor struct BrowserAddressFocusTests {
    @Test func aRequestToFocusTheAddressIsAnsweredOnce() {
        let tab = BrowserTab(makeEngine: FakeBrowserEngines().make)
        #expect(!tab.takeAddressFocusRequest())
        tab.requestAddressFocus()
        #expect(tab.takeAddressFocusRequest())
        // A toolbar shown again, as after switching tabs, doesn't take it again.
        #expect(!tab.takeAddressFocusRequest())
        tab.requestAddressFocus()
        #expect(tab.takeAddressFocusRequest())
    }
}
