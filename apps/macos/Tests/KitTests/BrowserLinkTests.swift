import AppKit
import SwiftUI
import Testing
import WebKit
@testable import Kit

/// A link activation as WebKit asks about it, with the keys and mouse
/// buttons it was made with: WebKit's mask, 1 for the left and 4 for the
/// middle.
private final class FakeNavigationAction: WKNavigationAction {
    private let fakeRequest: URLRequest
    private let fakeType: WKNavigationType
    private let fakeModifiers: NSEvent.ModifierFlags
    private let fakeButton: Int
    private let fakeDownload: Bool

    init(_ url: String, type: WKNavigationType = .linkActivated, modifiers: NSEvent.ModifierFlags = [],
         buttons: Int = 1, download: Bool = false, referrer: String? = nil) {
        var request = URLRequest(url: URL(string: url)!)
        if let referrer { request.setValue(referrer, forHTTPHeaderField: "Referer") }
        fakeRequest = request
        fakeType = type
        fakeModifiers = modifiers
        fakeButton = buttons
        fakeDownload = download
        super.init()
    }

    override var request: URLRequest { fakeRequest }
    override var navigationType: WKNavigationType { fakeType }
    override var modifierFlags: NSEvent.ModifierFlags { fakeModifiers }
    override var buttonNumber: Int { fakeButton }
    override var shouldPerformDownload: Bool { fakeDownload }
    // WebKit asks with no target frame for a link that opens a window, as
    // for one in the page's own frame, which can't be faked.
    override var targetFrame: WKFrameInfo? { nil }
}

/// What a link's keys and mouse button ask for: ⌘ or the middle button
/// opens it in a new tab in front, ⇧ with either keeps the new tab behind,
/// and anything else opens it in its own tab.
struct BrowserLinkDispositionTests {
    @Test(arguments: [
        (NSEvent.ModifierFlags(), false, BrowserLinkDisposition.here),
        ([.shift], false, .here),
        ([.option], false, .here),
        ([.command], false, .newTab(selected: true)),
        ([.command, .shift], false, .newTab(selected: false)),
        ([.command, .option], false, .newTab(selected: true)),
        ([], true, .newTab(selected: true)),
        ([.shift], true, .newTab(selected: false)),
    ] as [(NSEvent.ModifierFlags, Bool, BrowserLinkDisposition)])
    func aLinksKeysAndButtonChooseItsTab(modifiers: NSEvent.ModifierFlags, middleButton: Bool,
                                         expected: BrowserLinkDisposition) {
        #expect(BrowserLinkDisposition(modifiers: modifiers, middleButton: middleButton) == expected)
    }
}

/// WebKit's engine opens a link the person ⌘-clicks in a new tab instead
/// of in its own, and leaves every other navigation where it was.
@MainActor struct BrowserLinkPolicyTests {
    @MainActor private final class Page {
        let engine: WebKitBrowserEngine
        let view: WKWebView
        private(set) var opened: [String] = []

        init() throws {
            engine = try #require(WebKitBrowserEngine.make(downloadingTo: FileManager.default.temporaryDirectory)
                as? WebKitBrowserEngine)
            view = try #require(engine.view as? WKWebView)
            engine.onEvent = { [weak self] event in
                switch event {
                case .openLink(let url, let referrer, let select):
                    self?.opened.append("\(url.absoluteString) select=\(select)" + (referrer.map { " from \($0.absoluteString)" } ?? ""))
                case .openedWindow(let child): self?.opened.append("window"); child.close()
                default: break
                }
            }
        }

        /// The person presses a key in the page, as they do before using a link.
        func personUsesPage() throws {
            let event = try #require(NSEvent.keyEvent(
                with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0, windowNumber: 0, context: nil,
                characters: "", charactersIgnoringModifiers: "", isARepeat: false, keyCode: 63))
            view.keyDown(with: event)
        }

        func decide(_ action: WKNavigationAction) async -> WKNavigationActionPolicy {
            await engine.webView(view, decidePolicyFor: action)
        }

        func close() { engine.close() }
    }

    @Test func aCommandClickedLinkOpensInANewTabInsteadOfHere() async throws {
        let page = try Page()
        defer { page.close() }
        try page.personUsesPage()
        #expect(await page.decide(FakeNavigationAction("https://example.com/a", modifiers: .command,
                                                       referrer: "https://example.com/")) == .cancel)
        try page.personUsesPage()
        #expect(await page.decide(FakeNavigationAction("https://example.com/b", modifiers: [.command, .shift])) == .cancel)
        try page.personUsesPage()
        #expect(await page.decide(FakeNavigationAction("https://example.com/c", buttons: 4)) == .cancel)
        #expect(page.opened == ["https://example.com/a select=true from https://example.com/", "https://example.com/b select=false",
                                "https://example.com/c select=true"])
    }

    @Test func aPlainClickFormOrScriptStaysInItsTab() async throws {
        let page = try Page()
        defer { page.close() }
        try page.personUsesPage()
        #expect(await page.decide(FakeNavigationAction("https://example.com/a")) == .allow)
        #expect(await page.decide(FakeNavigationAction("https://example.com/b", type: .formSubmitted, modifiers: .command))
            == .allow)
        #expect(await page.decide(FakeNavigationAction("https://example.com/c", type: .other, modifiers: .command))
            == .allow)
        #expect(page.opened == [])
    }

    /// A page's script can click a link with ⌘ held, but only the person's
    /// own use of the page opens a tab, and each use opens one at most.
    @Test func onlyThePersonsUseOfThePageOpensATabAndOnlyOne() async throws {
        let page = try Page()
        defer { page.close() }
        #expect(await page.decide(FakeNavigationAction("https://example.com/script", modifiers: .command)) == .allow)
        try page.personUsesPage()
        #expect(await page.decide(FakeNavigationAction("https://example.com/first", modifiers: .command)) == .cancel)
        #expect(await page.decide(FakeNavigationAction("https://example.com/second", modifiers: .command)) == .allow)
        #expect(page.opened == ["https://example.com/first select=true"])
    }

    /// The person's use of one page can't open tabs for the next, as when
    /// a plain click leads to a page whose script clicks a link with ⌘.
    @Test func aNewPageCantUseThePersonsUseOfTheLastOne() async throws {
        let page = try Page()
        defer { page.close() }
        try page.personUsesPage()
        #expect(await page.decide(FakeNavigationAction("https://example.com/next")) == .allow)
        page.engine.webView(page.view, didCommit: nil)
        #expect(await page.decide(FakeNavigationAction("https://example.com/popup", modifiers: .command)) == .allow)
        try page.personUsesPage()
        #expect(await page.decide(FakeNavigationAction("https://example.com/asked", modifiers: .command)) == .cancel)
        #expect(page.opened == ["https://example.com/asked select=true"])
    }

    @Test func aCommandClickedDownloadOrMailLinkIsNotOpenedInATab() async throws {
        let page = try Page()
        defer { page.close() }
        try page.personUsesPage()
        #expect(await page.decide(FakeNavigationAction("https://example.com/notes.txt", modifiers: .command, download: true))
            == .download)
        // Mail links ask to open in Mail, only from a page on screen.
        #expect(await page.decide(FakeNavigationAction("mailto:kit@example.com", modifiers: .command)) == .cancel)
        #expect(page.opened == [])
    }

    /// The link menu's Open Link in New Window is Open Link in New Tab,
    /// which WebKit still performs, and its tab opens behind.
    @Test func theLinkMenusNewTabItemOpensTheLinkBehind() async throws {
        let page = try Page()
        defer { page.close() }
        @MainActor final class WebKitTarget: NSObject {
            var chosen: [NSMenuItem] = []
            @objc func open(_ sender: NSMenuItem) { chosen.append(sender) }
        }
        let target = WebKitTarget()
        let menu = NSMenu()
        let copy = menu.addItem(withTitle: "Copy Link", action: #selector(WebKitTarget.open(_:)), keyEquivalent: "")
        copy.identifier = NSUserInterfaceItemIdentifier("WKMenuItemIdentifierCopyLink")
        let item = menu.addItem(withTitle: "Open Link in New Window", action: #selector(WebKitTarget.open(_:)), keyEquivalent: "")
        item.identifier = NSUserInterfaceItemIdentifier("WKMenuItemIdentifierOpenLinkInNewWindow")
        item.target = target
        let event = try #require(NSEvent.mouseEvent(with: .rightMouseDown, location: .zero, modifierFlags: [], timestamp: 0,
                                                    windowNumber: 0, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
        page.view.willOpenMenu(menu, with: event)
        #expect(menu.items.map(\.title) == ["Copy Link", "Open Link in New Tab"])

        let index = try #require(menu.items.firstIndex(of: item))
        menu.performActionForItem(at: index)
        #expect(target.chosen == [item])
        let link = FakeNavigationAction("https://example.com/menu")
        #expect(page.engine.webView(page.view, createWebViewWith: WKWebViewConfiguration(), for: link,
                                    windowFeatures: WKWindowFeatures()) == nil)
        #expect(page.opened == ["https://example.com/menu select=false"])

        // A window the page opens afterwards is a tab in front, as before.
        _ = page.engine.webView(page.view, createWebViewWith: WKWebViewConfiguration(), for: link,
                                windowFeatures: WKWindowFeatures())
        #expect(page.opened.last == "window")
    }
}

/// A link opened in a new tab joins the tile of the tab it was opened from,
/// after that tab and the tabs it opened before.
@MainActor struct BrowserLinkWorkspaceTests {
    private func workspace(_ engines: FakeBrowserEngines) -> WorkspaceState {
        let workspace = WorkspaceState()
        workspace.makeBrowserEngine = engines.make
        return workspace
    }

    private func url(_ path: String) -> URL { URL(string: "https://example.com/" + path)! }

    @Test func aLinkOpenedInFrontIsTheNextTabAndSelected() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let opener = workspace.openBrowser(url("home"))
        workspace.open(.scratchpad)
        engines.made[0].send(.openLink(url("a"), referrer: url("home"), select: true))
        let strip = try #require(workspace.tiles[workspace.focusedTile]?.panes)
        #expect(strip.count == 4)
        #expect(Array(strip[0...1]) == [.conversation, .browser(opener.id)])
        #expect(strip[3] == .scratchpad)
        let opened = try #require(workspace.browser(strip[2]))
        #expect(workspace.selected == strip[2])
        #expect(opened.url == url("a"))
        #expect(opened.opener == .user)
        #expect(engines.made[1].loads == [url("a")])
        #expect(engines.made[1].referrers == [url("home")])
    }

    @Test func linksOpenedBehindFollowInTheOrderTheyWereOpenedAndLoadAtOnce() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let opener = workspace.openBrowser(url("home"))
        workspace.open(.scratchpad)
        workspace.select(.browser(opener.id))
        engines.made[0].send(.openLink(url("a"), referrer: nil, select: false))
        engines.made[0].send(.openLink(url("b"), referrer: nil, select: false))
        let strip = try #require(workspace.tiles[workspace.focusedTile]?.panes)
        #expect(workspace.selected == .browser(opener.id))
        #expect(strip.map { workspace.browser($0)?.url?.lastPathComponent ?? $0.title }
            == ["Agent", "home", "a", "b", WorkspacePane.scratchpad.title])
        #expect(engines.made.dropFirst().map(\.loads) == [[url("a")], [url("b")]])
        // A page loading behind is laid out as it would be seen.
        #expect(engines.made.dropFirst().map(\.view.frame.size) == [BrowserTab.unshownPageSize, BrowserTab.unshownPageSize])

        // A window the page opens joins the same run.
        let child = FakeBrowserEngine()
        child.state.url = url("window")
        engines.made[0].send(.openedWindow(child))
        let after = try #require(workspace.tiles[workspace.focusedTile]?.panes)
        #expect(after.compactMap { workspace.browser($0)?.url?.lastPathComponent } == ["home", "a", "b", "window"])
    }

    /// Tabs a page opens follow the tabs it opened before and theirs, so
    /// each page's tabs stay in the order they were opened.
    @Test func tabsOpenedFromOpenedTabsStayInOrder() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let home = workspace.openBrowser(url("home"))
        func open(_ path: String, from engine: Int) { engines.made[engine].send(.openLink(url(path), referrer: nil, select: false)) }
        open("a", from: 0)
        open("a1", from: 1)
        open("b", from: 0)
        open("a2", from: 1)
        let strip = try #require(workspace.tiles[workspace.focusedTile]?.panes)
        #expect(strip.compactMap { workspace.browser($0)?.url?.lastPathComponent } == ["home", "a", "a1", "a2", "b"])
        #expect(workspace.selected == .browser(home.id))
    }

    /// A tab opened for a link that only downloads a file closes, as in
    /// Safari, returning to the tab that opened it; the download goes on
    /// and reports, and then the tab's page ends.
    @Test func aTabOpenedOnlyForADownloadClosesAndTheDownloadReports() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let feedback = SessionFeedback()
        workspace.feedback = feedback
        let opener = workspace.openBrowser(url("home"))
        workspace.open(.scratchpad)
        workspace.select(.browser(opener.id))
        engines.made[0].send(.openLink(url("report.pdf"), referrer: nil, select: true))
        let download = engines.made[1]
        #expect(workspace.selected != .browser(opener.id))

        download.send(.onlyDownloaded)
        #expect(workspace.tiles[workspace.focusedTile]?.panes == [.conversation, .browser(opener.id), .scratchpad])
        #expect(workspace.selected == .browser(opener.id))
        #expect(!download.closed)

        download.send(.downloadFinished(file: URL(filePath: "/Users/me/Downloads/report.pdf"), source: url("report.pdf")))
        #expect(feedback.items.last?.title == "Downloaded report.pdf")
        #expect(download.closed)
    }

    /// A tab the person opened keeps the address they asked for, even when
    /// it only downloads a file.
    @Test func aTabThePersonOpenedStaysWhenItOnlyDownloads() {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let tab = workspace.openBrowser(url("report.pdf"))
        engines.made[0].send(.onlyDownloaded)
        #expect(workspace.panes == [.conversation, .browser(tab.id)])
        #expect(workspace.selected == .browser(tab.id))
    }

    @Test func aLinkFromAClosedTabOpensNothing() {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let tab = workspace.openBrowser(url("home"))
        let engine = engines.made[0]
        workspace.close(.browser(tab.id))
        engine.send(.openLink(url("a"), referrer: nil, select: true))
        #expect(workspace.panes == [.conversation])
        #expect(engines.made.count == 1)
    }
}

/// Copy Address copies the address the field shows, without a user name
/// or password, and the field confirms it briefly.
@MainActor struct BrowserCopyAddressTests {
    @Test func copyAddressCopiesTheFieldsAddressWithoutCredentials() throws {
        let engines = FakeBrowserEngines()
        let tab = BrowserTab(url: URL(string: "https://kit:secret@example.com/pr/12?tab=files#diff")!,
                             makeEngine: engines.make)
        let pasteboard = NSPasteboard(name: NSPasteboard.Name("kit-test-" + UUID().uuidString))
        defer { pasteboard.releaseGlobally() }
        tab.copyAddress(to: pasteboard)
        #expect(pasteboard.string(forType: .string) == "https://example.com/pr/12?tab=files#diff")
        #expect(tab.addressCopies == 1)
    }

    @Test func aFailedAddressIsTheOneCopied() {
        let engines = FakeBrowserEngines()
        let tab = BrowserTab(makeEngine: engines.make)
        tab.load(URL(string: "https://example.com")!)
        engines.made[0].send(.failed(url: URL(string: "https://down.example.com/page"), message: "Offline"))
        let pasteboard = NSPasteboard(name: NSPasteboard.Name("kit-test-" + UUID().uuidString))
        defer { pasteboard.releaseGlobally() }
        tab.copyAddress(to: pasteboard)
        #expect(pasteboard.string(forType: .string) == "https://down.example.com/page")
    }

    @Test func aBlankTabHasNoAddressToCopy() {
        let engines = FakeBrowserEngines()
        let tab = BrowserTab(makeEngine: engines.make)
        let pasteboard = NSPasteboard(name: NSPasteboard.Name("kit-test-" + UUID().uuidString))
        defer { pasteboard.releaseGlobally() }
        pasteboard.clearContents()
        pasteboard.setString("before", forType: .string)
        #expect(tab.copyableAddress == nil)
        tab.copyAddress(to: pasteboard)
        #expect(pasteboard.string(forType: .string) == "before")
        #expect(tab.addressCopies == 0)
    }
}

/// A browser tab's context menu offers Copy Address, available once the tab
/// has an address.
@MainActor struct BrowserTabMenuTests {
    private func entries(_ pane: WorkspacePane, in store: SessionStore) async throws -> [String] {
        let size = NSSize(width: 200, height: 40)
        let view = Color.clear.frame(width: size.width, height: size.height).contentShape(Rectangle())
            .contextMenu { WorkspaceTabActions(state: store, workspace: store.ui.workspace, pane: pane) }
            .environment(\.mica, ThemeConfiguration.decode("").theme(dark: true))
        let host = NSHostingView(rootView: view)
        let window = NSWindow(testContentRect: NSRect(origin: .zero, size: size))
        defer { window.close() }
        window.contentView = host
        window.orderFront(nil)
        for _ in 0..<5 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }
        let event = try #require(NSEvent.mouseEvent(with: .rightMouseDown, location: NSPoint(x: 100, y: 20), modifierFlags: [],
                                                    timestamp: 0, windowNumber: window.windowNumber, context: nil,
                                                    eventNumber: 0, clickCount: 1, pressure: 1))
        let menu = try #require(host.menu(for: event))
        return menu.items.map { $0.isSeparatorItem ? "—" : $0.isEnabled ? $0.title : "\($0.title) (disabled)" }
    }

    @Test func copyAddressIsAvailableOnlyOnceTheTabHasAnAddress() async throws {
        let store = SessionStore(fixture: try Fixture.load())
        let workspace = store.ui.workspace
        let engines = FakeBrowserEngines()
        workspace.makeBrowserEngine = engines.make
        let blank = workspace.openBrowser()
        let page = workspace.openBrowser(URL(string: "https://example.com")!)
        #expect(try await entries(.browser(blank.id), in: store)
            == ["Copy Address (disabled)", "—", "Split Right", "Split Down", "—", "Close Tab"])
        #expect(try await entries(.browser(page.id), in: store)
            == ["Copy Address", "—", "Split Right", "Split Down", "—", "Close Tab"])
    }
}

/// Links in a real WebKit page served from a loopback server, clicked as
/// AppKit delivers a person's mouse: ⌘ or the middle button opens the link
/// in a new tab after this one, and a plain click goes there in this tab.
@MainActor struct BrowserLinkClickTests {
    @MainActor private struct Page {
        let server: LoopbackHTTPServer
        let root: URL
        let folder: URL
        let workspace: WorkspaceState
        let tab: BrowserTab
        let view: NSView
        let stage: BrowserAgentStage

        var tabs: [BrowserTab] { workspace.panes.compactMap { workspace.browser($0) } }

        func close() {
            stage.release()
            for tab in tabs { tab.close() }
            server.stop()
            try? FileManager.default.removeItem(at: folder)
        }

        /// The request headers the server got for `path`.
        func request(_ path: String) -> String? {
            server.requests.first { $0.hasPrefix("GET \(path) ") }
        }

        /// Clicks the link, or the one that opens a window, with mouse
        /// button `button`, holding `modifiers`.
        func clickLink(window opening: Bool = false, download: Bool = false, button: Int = 0,
                       modifiers: NSEvent.ModifierFlags = []) throws {
            let window = try #require(view.window)
            let types: [NSEvent.EventType] = button == 0 ? [.leftMouseDown, .leftMouseUp] : [.otherMouseDown, .otherMouseUp]
            for (number, type) in types.enumerated() {
                let made = try #require(NSEvent.mouseEvent(
                    with: type, location: view.convert(CGPoint(x: 100, y: download ? 250 : opening ? 150 : 50), to: nil), modifierFlags: modifiers,
                    timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber, context: nil,
                    eventNumber: number + 1, clickCount: 1, pressure: number == 0 ? 1 : 0))
                let copy = try #require(made.cgEvent?.copy())
                copy.setIntegerValueField(.mouseEventButtonNumber, value: Int64(button))
                let event = try #require(NSEvent(cgEvent: copy))
                switch type {
                case .leftMouseDown: view.mouseDown(with: event)
                case .leftMouseUp: view.mouseUp(with: event)
                case .otherMouseDown: view.otherMouseDown(with: event)
                default: view.otherMouseUp(with: event)
                }
            }
        }
    }

    private func open() async throws -> Page {
        func page(_ title: String, _ body: String = "") -> LoopbackHTTPServer.Response {
            .init(body: "<!doctype html><html><head><title>\(title)</title></head><body>\(body)</body></html>")
        }
        let server = try LoopbackHTTPServer(routes: [
            "/": page("Links", #"""
                <a href="/next" style="position: absolute; left: 0; top: 0; width: 300px; height: 100px">Next</a>
                <a href="/window" target="_blank" style="position: absolute; left: 0; top: 100px; width: 300px; height: 100px">Window</a>
                <a href="/report.txt" style="position: absolute; left: 0; top: 200px; width: 300px; height: 100px">Report</a>
                """#),
            "/next": page("Next"),
            "/window": page("Window"),
            "/report.txt": BrowserDownloadTests.attachment,
        ])
        let root = try await server.start()
        let folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let workspace = WorkspaceState()
        workspace.makeBrowserEngine = { WebKitBrowserEngine.make(downloadingTo: folder) }
        let tab = workspace.openBrowser(root.appending(path: "/"))
        let view = try tab.agentEngine().view
        let stage = try #require(BrowserAgentStage.stage(view, size: BrowserTab.unshownPageSize))
        let opened = Page(server: server, root: root, folder: folder, workspace: workspace, tab: tab, view: view, stage: stage)
        try await eventually { !tab.loading && tab.page.title == "Links" }
        return opened
    }

    @Test func aCommandClickedLinkOpensInFrontAndThisTabStays() async throws {
        let page = try await open()
        defer { page.close() }
        try page.clickLink(modifiers: .command)
        try await eventually { page.tabs.count == 2 && page.tabs[1].page.title == "Next" }
        #expect(page.workspace.selected == .browser(page.tabs[1].id))
        #expect(page.tab.page.title == "Links")
        #expect(page.tabs[1].url == page.root.appending(path: "/next"))
        // The site hears which page linked to it, as from a plain click.
        #expect(page.request("/next")?.contains("\r\nReferer: \(page.root.absoluteString)/\r\n") == true)
    }

    @Test func aCommandClickedLinkToAFileDownloadsItAndItsTabCloses() async throws {
        let page = try await open()
        defer { page.close() }
        let feedback = SessionFeedback()
        page.workspace.feedback = feedback
        try page.clickLink(download: true, modifiers: .command)
        try await eventually { feedback.items.last?.title == "Downloaded report.txt" }
        #expect(try String(contentsOf: page.folder.appending(path: "report.txt"), encoding: .utf8) == "quarterly numbers")
        #expect(page.tabs.map(\.id) == [page.tab.id])
        #expect(page.workspace.selected == .browser(page.tab.id))
    }

    @Test func aShiftCommandClickedLinkThatOpensAWindowOpensBehind() async throws {
        let page = try await open()
        defer { page.close() }
        try page.clickLink(window: true, modifiers: [.command, .shift])
        try await eventually { page.tabs.count == 2 && page.tabs[1].page.title == "Window" }
        #expect(page.workspace.selected == .browser(page.tab.id))
        #expect(page.tab.page.title == "Links")
    }

    @Test func aPlainClickOnALinkThatOpensAWindowOpensItInFront() async throws {
        let page = try await open()
        defer { page.close() }
        try page.clickLink(window: true)
        try await eventually { page.tabs.count == 2 && page.tabs[1].page.title == "Window" }
        #expect(page.workspace.selected == .browser(page.tabs[1].id))
    }

    @Test func aShiftMiddleClickedLinkOpensBehind() async throws {
        let page = try await open()
        defer { page.close() }
        try page.clickLink(button: 2, modifiers: .shift)
        try await eventually { page.tabs.count == 2 && page.tabs[1].page.title == "Next" }
        #expect(page.workspace.selected == .browser(page.tab.id))
        #expect(page.tab.page.title == "Links")
    }

    @Test func aPlainClickGoesThereInThisTab() async throws {
        let page = try await open()
        defer { page.close() }
        try page.clickLink()
        try await eventually { page.tab.page.title == "Next" && !page.tab.loading }
        #expect(page.tabs.count == 1)
    }
}

/// The address field confirms a copy with "✓ Copied" at its end, briefly.
@MainActor struct BrowserCopyConfirmationTests {
    /// What the workspace draws, as its bitmap's bytes.
    private struct Drawing: Equatable {
        let bytes: [UInt8]
        let bytesPerRow: Int
        let bytesPerPixel: Int
        let scale: CGFloat

        @MainActor init(_ view: NSView) throws {
            let bitmap = try #require(view.bitmapImageRepForCachingDisplay(in: view.bounds))
            view.cacheDisplay(in: view.bounds, to: bitmap)
            try #require(bitmap.bitsPerSample == 8 && !bitmap.isPlanar)
            let data = try #require(bitmap.bitmapData)
            bytes = Array(UnsafeBufferPointer(start: data, count: bitmap.bytesPerRow * bitmap.pixelsHigh))
            bytesPerRow = bitmap.bytesPerRow
            bytesPerPixel = bitmap.bitsPerPixel / 8
            scale = CGFloat(bitmap.pixelsWide) / view.bounds.width
        }

        /// The smallest rectangle, in points from the top-left corner, that
        /// holds every pixel drawn differently in `other`.
        func changes(from other: Drawing) -> CGRect? {
            var box: CGRect?
            for index in bytes.indices where bytes[index] != other.bytes[index] {
                let x = (index % bytesPerRow) / bytesPerPixel, y = index / bytesPerRow
                let pixel = CGRect(x: CGFloat(x) / scale, y: CGFloat(y) / scale, width: 1 / scale, height: 1 / scale)
                box = box?.union(pixel) ?? pixel
            }
            return box
        }
    }

    @Test func theAddressFieldSaysCopiedAtItsEndThenGoesBackToTheAddress() async throws {
        let store = SessionStore(fixture: try Fixture.load())
        let engines = FakeBrowserEngines()
        store.ui.workspace.makeBrowserEngine = engines.make
        let tab = store.ui.workspace.openBrowser(URL(string: "https://example.com/pr/12")!)
        let host = NSHostingView(rootView: WorkspaceLayout(state: store)
            .environment(\.mica, ThemeConfiguration.decode("").theme(dark: true)))
        let window = FocusIsolatedWindow(testContentRect: NSRect(x: 0, y: 0, width: 1000, height: 500))
        window.colorSpace = .sRGB
        defer { window.close(); tab.close() }
        window.contentView = host
        window.orderFront(nil)
        func settle() async throws { for _ in 0..<10 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) } }
        try await settle()
        let before = try Drawing(host)

        let pasteboard = NSPasteboard(name: NSPasteboard.Name("kit-test-" + UUID().uuidString))
        defer { pasteboard.releaseGlobally() }
        tab.copyAddress(to: pasteboard)
        try await eventually {
            host.layoutSubtreeIfNeeded()
            return (try? Drawing(host)).map { $0 != before } ?? false
        }
        let confirmation = try #require(try Drawing(host).changes(from: before))
        // The toolbar is the 38 pt above the page. The field ends before
        // Annotate, Copy Address, and Open in Default Browser, 26 pt each
        // with 2 pt before each, and the toolbar's 8 pt edge; its text is
        // 10 pt in.
        let page = tab.view.convert(tab.view.bounds, to: host)
        let fieldTextEnd = page.maxX - 8 - 3 * (26 + 2) - 10
        #expect(confirmation.minY >= page.minY - 38 && confirmation.maxY <= page.minY)
        #expect(confirmation.maxX <= fieldTextEnd + 1 && confirmation.maxX >= fieldTextEnd - 4)
        // A check mark and "Copied" at 11 pt.
        #expect((40...70).contains(confirmation.width))

        try await eventually {
            host.layoutSubtreeIfNeeded()
            return (try? Drawing(host)) == before
        }
    }
}
