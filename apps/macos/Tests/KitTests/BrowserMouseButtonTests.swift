import AppKit
import SwiftUI
import Testing
import WebKit
@testable import Kit

/// A browser tab's pane in a window, drawn by WebKit, clicked with a mouse's
/// buttons as AppKit sends them.
@MainActor private final class BrowserMouseHarness {
    let store: SessionStore
    let window: NSWindow
    let folder: URL
    private let host: NSHostingView<AnyView>
    var workspace: WorkspaceState { store.ui.workspace }

    init() throws {
        folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        store = SessionStore(fixture: try Fixture.load())
        store.ui.workspace.makeBrowserEngine = { [folder] in WebKitBrowserEngine.make(downloadingTo: folder) }
        host = NSHostingView(rootView: AnyView(Color.clear))
        window = FocusIsolatedWindow(testContentRect: NSRect(x: 0, y: 0, width: 1200, height: 600))
        window.contentView = host
        window.orderFront(nil)
    }

    /// A page titled `title` that logs the mouse events it gets and keeps
    /// its context menu to itself. `script` runs after.
    static func page(_ title: String, script: String = "", body: String = "") -> URL {
        let html = """
            <!doctype html><html><head><title>\(title)</title></head><body style="margin: 0; height: 100vh">\(body)
            <script>
            window.log = [];
            for (const type of ['mousedown', 'mouseup', 'click', 'auxclick', 'contextmenu']) {
              addEventListener(type, e => log.push(type + ' ' + e.button));
            }
            addEventListener('contextmenu', e => e.preventDefault());
            \(script)
            </script></body></html>
            """
        return URL(string: "data:text/html;base64," + Data(html.utf8).base64EncodedString())!
    }

    /// Opens a tab and shows it, then visits each page after the first,
    /// leaving the tab on the last.
    func open(_ pages: [URL]) async throws -> BrowserTab {
        let tab = workspace.openBrowser(pages[0])
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        host.rootView = AnyView(WorkspaceLayout(state: store).environment(\.mica, theme))
        for _ in 0..<8 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }
        try await settled(tab)
        for page in pages.dropFirst() {
            tab.load(page)
            try await eventually { tab.page.url == page }
            try await settled(tab)
        }
        return tab
    }

    /// Waits for the tab's page to finish loading.
    func settled(_ tab: BrowserTab) async throws {
        try await eventually { !tab.loading && tab.url == tab.page.url && !tab.page.title.isEmpty }
    }

    /// The page's view.
    func pageView(_ tab: BrowserTab) throws -> WKWebView { try #require(tab.view as? WKWebView) }

    /// The middle of the page, in window coordinates.
    func pageCentre(_ tab: BrowserTab) throws -> CGPoint {
        let frame = try pageView(tab).convert(try pageView(tab).bounds, to: nil)
        return CGPoint(x: frame.midX, y: frame.midY)
    }

    /// The middle of the toolbar's Back button: the first 26 pt button,
    /// 8 pt in, in the 38 pt toolbar above the page.
    func backButton(_ tab: BrowserTab) throws -> CGPoint {
        let frame = try pageView(tab).convert(try pageView(tab).bounds, to: nil)
        return CGPoint(x: frame.minX + 8 + 13, y: frame.maxY + 19)
    }

    /// A point in the address field, in the middle of the toolbar.
    func addressField(_ tab: BrowserTab) throws -> CGPoint {
        let frame = try pageView(tab).convert(try pageView(tab).bounds, to: nil)
        return CGPoint(x: frame.midX, y: frame.maxY + 19)
    }

    /// Presses and releases mouse button `button` at `point`: 0 is the
    /// left, 1 the right, 2 the middle, 3 back, and 4 forward. The events
    /// are queued and taken from the queue as AppKit takes a person's, so
    /// each is the application's current event while it's sent, and a
    /// control tracking the press finds its release.
    func click(_ button: Int, at point: CGPoint) throws {
        let types: [NSEvent.EventType] = switch button {
        case 0: [.leftMouseDown, .leftMouseUp]
        case 1: [.rightMouseDown, .rightMouseUp]
        default: [.otherMouseDown, .otherMouseUp]
        }
        for type in types {
            let made = try #require(NSEvent.mouseEvent(
                with: type, location: point, modifierFlags: [], timestamp: ProcessInfo.processInfo.systemUptime,
                windowNumber: window.windowNumber, context: nil, eventNumber: 1, clickCount: 1,
                pressure: types.first == type ? 1 : 0))
            let copy = try #require(made.cgEvent?.copy())
            copy.setIntegerValueField(.mouseEventButtonNumber, value: Int64(button))
            let event = try #require(NSEvent(cgEvent: copy))
            #expect(event.buttonNumber == button && event.window === window)
            NSApp.postEvent(event, atStart: false)
        }
        let mask = types.reduce(into: NSEvent.EventTypeMask()) { $0.insert(NSEvent.EventTypeMask(type: $1)) }
        while let next = NSApp.nextEvent(matching: mask, until: Date(timeIntervalSinceNow: 0.1), inMode: .default, dequeue: true) {
            NSApp.sendEvent(next)
        }
    }

    /// What the tab shows and where it can go: its page's title, and
    /// whether it can go back and forward.
    func history(_ tab: BrowserTab) -> String {
        "\(tab.page.title) back=\(tab.page.canGoBack) forward=\(tab.page.canGoForward)"
    }

    /// Clicks `button` at `point` and waits for the tab to show `expected`
    /// and finish loading, then a little longer, so a second navigation
    /// from the same click would show.
    func click(_ button: Int, at point: CGPoint, in tab: BrowserTab, shows expected: String) async throws {
        try click(button, at: point)
        try await eventually { history(tab) == expected && !tab.loading }
        try await Task.sleep(for: .milliseconds(300))
        try await settled(tab)
        #expect(history(tab) == expected)
    }

    /// The mouse events the page has had.
    func log(_ tab: BrowserTab) async throws -> String {
        try #require(try await pageView(tab).evaluateJavaScript("log.join('|')") as? String)
    }

    /// Empties the page's log. A page shown again from WebKit's cache
    /// keeps the clicks it had before.
    func clearLog(_ tab: BrowserTab) async throws {
        _ = try await pageView(tab).evaluateJavaScript("log = []; ''")
    }

    func close() {
        for tab in workspace.panes.compactMap({ workspace.browser($0) }) { tab.close() }
        window.close()
        try? FileManager.default.removeItem(at: folder)
    }
}

/// A mouse's back and forward buttons step through a browser tab's history
/// anywhere in its pane, once per click and on release, as in other
/// browsers; the page gets the click too. Its other buttons are the page's.
///
/// The clicks go through AppKit's event queue, which wakes the loop taking
/// events by stopping the main run loop's current run. Xcode's test runner
/// carries on after a stop; `swift test` runs the main run loop once and
/// exits when it stops, ending the test process mid-run without a summary.
@MainActor @Suite(.enabled(if: ProcessInfo.processInfo.processName == "xctest",
                           "Takes events from AppKit's queue, which ends a `swift test` process; run it with xcodebuild"))
struct BrowserMouseButtonTests {
    private let pages = ["A", "B", "C"].map { BrowserMouseHarness.page($0) }

    @Test func backAndForwardOverThePageStepOncePerClickToTheEndsOfHistory() async throws {
        let harness = try BrowserMouseHarness()
        defer { harness.close() }
        let tab = try await harness.open(pages)
        let page = try harness.pageCentre(tab)
        #expect(harness.history(tab) == "C back=true forward=false")

        try await harness.click(3, at: page, in: tab, shows: "B back=true forward=true")
        try await harness.click(3, at: page, in: tab, shows: "A back=false forward=true")
        try await harness.clearLog(tab)
        try await harness.click(3, at: page, in: tab, shows: "A back=false forward=true")
        // The page had the click that had nowhere to go.
        #expect(try await harness.log(tab) == "mousedown 3|mouseup 3|auxclick 3")

        try await harness.click(4, at: page, in: tab, shows: "B back=true forward=true")
        try await harness.click(4, at: page, in: tab, shows: "C back=true forward=false")
        try await harness.clearLog(tab)
        try await harness.click(4, at: page, in: tab, shows: "C back=true forward=false")
        #expect(try await harness.log(tab) == "mousedown 4|mouseup 4|auxclick 4")
        #expect(tab.url == pages[2])
    }

    @Test func backAndForwardOverTheToolbarStepOncePerClick() async throws {
        let harness = try BrowserMouseHarness()
        defer { harness.close() }
        let tab = try await harness.open(pages)
        let back = try harness.backButton(tab)
        let address = try harness.addressField(tab)

        try await harness.click(3, at: address, in: tab, shows: "B back=true forward=true")
        try await harness.click(3, at: back, in: tab, shows: "A back=false forward=true")
        try await harness.click(3, at: address, in: tab, shows: "A back=false forward=true")
        try await harness.click(4, at: address, in: tab, shows: "B back=true forward=true")
        try await harness.click(4, at: back, in: tab, shows: "C back=true forward=false")
        try await harness.click(4, at: address, in: tab, shows: "C back=true forward=false")
        #expect(tab.url == pages[2])
        // The toolbar's own Back button still takes a click.
        try await harness.click(0, at: back, in: tab, shows: "B back=true forward=true")
    }

    @Test func backOverAPageThatFailedToLoadReturnsToTheOneBefore() async throws {
        let harness = try BrowserMouseHarness()
        defer { harness.close() }
        let tab = try await harness.open(Array(pages.prefix(2)))
        // A server that has stopped refuses the connection.
        let server = try LoopbackHTTPServer()
        let gone = try await server.start()
        server.stop()
        tab.load(gone)
        try await eventually { tab.failure != nil }
        #expect(tab.failure?.title == "Can’t reach " + BrowserAddress.hostLabel(gone))

        try harness.click(3, at: try harness.pageCentre(tab))
        try await eventually { tab.failure == nil && harness.history(tab) == "A back=false forward=true" }
        #expect(tab.url == pages[0])
    }

    @Test func theOtherButtonsReachThePageAndLeaveHistoryAlone() async throws {
        let harness = try BrowserMouseHarness()
        defer { harness.close() }
        let tab = try await harness.open(Array(pages.prefix(2)))
        let page = try harness.pageCentre(tab)
        for button in [0, 1, 2] { try harness.click(button, at: page) }
        // The page numbers the right button 2 and the middle 1.
        let expected = "mousedown 0|mouseup 0|click 0|mousedown 2|contextmenu 2|mouseup 2|auxclick 2|mousedown 1|mouseup 1|auxclick 1"
        try await eventually { (try? await harness.log(tab)) == expected }
        #expect(try await harness.log(tab) == expected)
        #expect(harness.history(tab) == "B back=true forward=false")
    }

    /// A mouse's back and forward buttons ask the browser to navigate, as
    /// Kit's Back and Forward do; they aren't the person using the page.
    /// The page's other buttons are, as before.
    @Test func onlyThePagesOwnButtonsLetItStartADownload() async throws {
        let harness = try BrowserMouseHarness()
        defer { harness.close() }
        let page = BrowserMouseHarness.page("Notes", script: "addEventListener('auxclick', () => file.click());",
                                            body: #"<a id="file" href="data:text/plain;base64,aGk=" download="notes.txt">Notes</a>"#)
        let tab = try await harness.open([page])
        let engine = try #require(try tab.agentEngine() as? WebKitBrowserEngine)
        let centre = try harness.pageCentre(tab)

        try harness.click(3, at: centre)
        try await eventually { tab.waitingDownload != nil }
        #expect(tab.waitingDownload == BrowserDownloadRequest(site: ""))
        #expect(engine.answerDownload(allow: false))
        #expect(try FileManager.default.contentsOfDirectory(atPath: harness.folder.path) == [])

        try harness.click(2, at: centre)
        let notes = harness.folder.appending(path: "notes.txt")
        try await eventually { FileManager.default.fileExists(atPath: notes.path) }
        #expect(tab.waitingDownload == nil)
        #expect(try String(contentsOf: notes, encoding: .utf8) == "hi")
    }
}
