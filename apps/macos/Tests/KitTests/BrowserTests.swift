import AppKit
import Foundation
import Testing
@testable import Kit

/// A browser engine that records what it's asked and reports what a test sends.
@MainActor final class FakeBrowserEngine: BrowserEngine {
    var view = NSView()
    var onEvent: (@MainActor (BrowserEngineEvent) -> Void)?
    var interceptsNavigation: (@MainActor (URL) -> Bool)?
    var state = BrowserPageState()
    private(set) var loads: [URL] = []
    private(set) var reloads = 0
    private(set) var closed = false

    func load(_ url: URL) { loads.append(url); state.url = url }
    func goBack() {}
    func goForward() {}
    func reload() { reloads += 1 }
    func stop() {}
    func close() { closed = true }

    func send(_ event: BrowserEngineEvent) { onEvent?(event) }
    /// The page goes to `url`, as by a link or redirect; false when the
    /// tab stopped it.
    func navigate(_ url: URL) -> Bool {
        if interceptsNavigation?(url) == true { return false }
        change { $0.url = url }
        return true
    }
    func change(_ update: (inout BrowserPageState) -> Void) {
        update(&state)
        send(.changed(state))
    }
}

/// Makes fake engines and keeps them for inspection.
@MainActor final class FakeBrowserEngines {
    private(set) var made: [FakeBrowserEngine] = []
    var make: @MainActor () -> any BrowserEngine {
        { [unowned self] in
            let engine = FakeBrowserEngine()
            self.made.append(engine)
            return engine
        }
    }
}

struct BrowserAddressTests {
    @Test(arguments: [
        ("localhost:5173", "http://localhost:5173"),
        ("localhost:5173/settings/profile", "http://localhost:5173/settings/profile"),
        ("127.0.0.1:8080", "http://127.0.0.1:8080"),
        ("printer.local/status", "http://printer.local/status"),
        ("app.localhost:3000", "http://app.localhost:3000"),
        ("[::1]:3000", "http://[::1]:3000"),
        ("example.com", "https://example.com"),
        ("docs.example.com/guide?page=2#setup", "https://docs.example.com/guide?page=2#setup"),
        ("10.0.0.4:8000", "https://10.0.0.4:8000"),
        ("  https://example.com/a  ", "https://example.com/a"),
        ("bücher.de", "https://xn--bcher-kva.de"),
    ])
    func hostsGetASchemeAndAddressesKeepTheirs(input: String, expected: String) {
        #expect(BrowserAddress.url(from: input)?.absoluteString == expected)
    }

    @Test(arguments: ["", "foo bar", "foo", "how do I center a div", "ftp://example.com", "mailto:a@example.com",
                      "javascript:alert(1)", "file:///etc/hosts", "https://", "user@example.com", "example.123",
                      "localhost:99999", "-bad.example.com"])
    func textThatIsNotAWebAddressIsRefused(input: String) {
        #expect(BrowserAddress.url(from: input) == nil)
    }

    @Test func savedAndListedAddressesLeaveOutCredentials() {
        let url = URL(string: "https://ada:secret@example.com/a?b=1")!
        #expect(BrowserAddress.withoutCredentials(url).absoluteString == "https://example.com/a?b=1")
    }

    @Test func theFieldShowsTheHostThenTheRestWithoutSchemeOrBarePath() {
        let page = BrowserAddress.display(URL(string: "http://localhost:5173/settings/profile?tab=1#name")!)
        #expect(page.host == "localhost:5173")
        #expect(page.rest == "/settings/profile?tab=1#name")
        let root = BrowserAddress.display(URL(string: "https://example.com/")!)
        #expect(root.host == "example.com")
        #expect(root.rest == "")
    }
}

struct BrowserDownloadsTests {
    @Test func aNameAlreadyTakenGetsTheNextNumber() {
        let folder = URL(filePath: "/Users/me/Downloads", directoryHint: .isDirectory)
        let taken: Set<String> = ["report-q3.pdf", "report-q3 2.pdf", "notes"]
        let exists = { (url: URL) in taken.contains(url.lastPathComponent) }
        #expect(BrowserDownloads.destination(for: "report-q3.pdf", in: folder, exists: exists).path
            == "/Users/me/Downloads/report-q3 3.pdf")
        #expect(BrowserDownloads.destination(for: "notes", in: folder, exists: exists).lastPathComponent == "notes 2")
        #expect(BrowserDownloads.destination(for: "new.zip", in: folder, exists: exists).lastPathComponent == "new.zip")
    }

    @Test func suggestedNamesStayInTheFolderAndVisible() {
        #expect(BrowserDownloads.sanitized("../../etc/passwd") == "passwd")
        #expect(BrowserDownloads.sanitized(".hidden") == "hidden")
        #expect(BrowserDownloads.sanitized("  ") == "download")
        #expect(BrowserDownloads.sanitized("a:b.txt") == "a-b.txt")
        let long = BrowserDownloads.sanitized(String(repeating: "é", count: 200) + ".pdf")
        #expect(long.utf8.count <= BrowserDownloads.maximumNameBytes)
        #expect(long.hasSuffix("é.pdf"))
    }
}

@MainActor struct BrowserTabTests {
    @Test func aTabIsNamedByItsPageThenItsHostThenAsNew() {
        let engines = FakeBrowserEngines()
        let tab = BrowserTab(makeEngine: engines.make)
        #expect(tab.title == "New tab")
        tab.load(URL(string: "http://localhost:5173/settings")!)
        #expect(tab.title == "localhost:5173")
        engines.made[0].change { $0.title = "Settings · Acme"; $0.loading = true; $0.progress = 0.4 }
        #expect(tab.title == "Settings · Acme")
        #expect(tab.loading)
        #expect(tab.page.progress == 0.4)
    }

    @Test func textThatIsNotAnAddressShowsWhyWithoutLoading() {
        let engines = FakeBrowserEngines()
        let tab = BrowserTab(makeEngine: engines.make)
        tab.navigate("foo bar")
        #expect(tab.failure == BrowserFailure(title: "Can’t open ‘foo bar’", detail: "That isn’t a web address.", retry: nil))
        #expect(tab.url == nil)
        #expect(tab.title == "New tab")
        #expect(engines.made.isEmpty)
    }

    @Test func aFailedLoadNamesTheHostAndTryAgainLoadsItAgain() {
        let engines = FakeBrowserEngines()
        let tab = BrowserTab(makeEngine: engines.make)
        tab.navigate("localhost:5173")
        let engine = engines.made[0]
        let url = URL(string: "http://localhost:5173")!
        #expect(engine.loads == [url])
        engine.send(.failed(url: url, message: "Could not connect to the server."))
        #expect(tab.failure == BrowserFailure(title: "Can’t reach localhost:5173",
                                              detail: "Could not connect to the server.", retry: url))
        #expect(tab.title == "localhost:5173")
        tab.reload()
        #expect(engine.loads == [url, url])
        #expect(tab.failure == nil)
    }

    @Test func aCommittedNavigationClearsTheFailure() {
        let engines = FakeBrowserEngines()
        let tab = BrowserTab(makeEngine: engines.make)
        tab.load(URL(string: "https://example.com")!)
        engines.made[0].send(.failed(url: URL(string: "https://example.com/gone")!, message: "Not found"))
        #expect(tab.url?.absoluteString == "https://example.com/gone")
        engines.made[0].change { $0.url = URL(string: "https://example.com/")!; $0.loading = true }
        engines.made[0].send(.committed)
        #expect(tab.failure == nil)
        #expect(tab.url?.absoluteString == "https://example.com/")
    }

    @Test func aRestoredTabLoadsOnlyOnceItsPageIsShown() async {
        let engines = FakeBrowserEngines()
        let url = URL(string: "https://example.com/docs")!
        let tab = BrowserTab(url: url, makeEngine: engines.make)
        #expect(engines.made.isEmpty)
        #expect(tab.title == "example.com")
        _ = tab.view
        for _ in 0..<20 where engines.made.first?.loads.isEmpty != false { await Task.yield() }
        #expect(engines.made.count == 1)
        #expect(engines.made[0].loads == [url])
    }

    @Test func aFaviconThatIsAnImageShowsInTheTab() throws {
        let engines = FakeBrowserEngines()
        let tab = BrowserTab(makeEngine: engines.make)
        tab.load(URL(string: "https://example.com")!)
        engines.made[0].send(.favicon(Data("not an image".utf8)))
        #expect(tab.favicon == nil)
        let image = NSImage(size: NSSize(width: 16, height: 16), flipped: false) { rect in
            NSColor.systemPurple.setFill(); rect.fill(); return true
        }
        let tiff = try #require(image.tiffRepresentation)
        let png = try #require(NSBitmapImageRep(data: tiff)?.representation(using: .png, properties: [:]))
        engines.made[0].send(.favicon(png))
        #expect(tab.favicon != nil)
    }
}

@MainActor struct BrowserWorkspaceTests {
    private func workspace(_ engines: FakeBrowserEngines) -> WorkspaceState {
        let workspace = WorkspaceState()
        workspace.makeBrowserEngine = engines.make
        return workspace
    }

    @Test func aNewTabOpensInTheFocusedTileWithItsAddressFieldFocused() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let tab = workspace.openBrowser()
        let pane = WorkspacePane.browser(tab.id)
        #expect(workspace.selected == pane)
        #expect(workspace.tiles[workspace.focusedTile]?.panes == [.conversation, pane])
        #expect(tab.addressFocusRequest == 1)
        #expect(workspace.title(of: pane) == "New tab")
        #expect(engines.made.isEmpty)
    }

    @Test func aLinkOpenedBesideTheConversationLoadsInANewTile() {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let url = URL(string: "https://example.com/pr/12")!
        let tab = workspace.openBrowser(url, beside: .conversation)
        #expect(workspace.root.tiles.count == 2)
        #expect(workspace.tiles[workspace.focusedTile]?.panes == [.browser(tab.id)])
        #expect(engines.made[0].loads == [url])
        #expect(tab.addressFocusRequest == 0)
    }

    @Test func aWebLinkOpensBesideTheConversationUnlessAskedToOpenExternally() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let pullRequest = URL(string: "https://github.com/akonwi/kit/pull/12")!
        #expect(!workspace.openLink(pullRequest, beside: .conversation, externally: true))
        #expect(!workspace.openLink(URL(string: "mailto:kit@example.com")!, beside: .conversation))
        #expect(workspace.root.tiles.count == 1)
        #expect(engines.made.isEmpty)

        #expect(workspace.openLink(pullRequest, beside: .conversation))
        let pane = try #require(workspace.selected)
        #expect(workspace.root.tiles.count == 2)
        #expect(workspace.tiles[workspace.focusedTile]?.panes == [pane])
        #expect(workspace.browser(pane)?.url == pullRequest)
        #expect(engines.made[0].loads == [pullRequest])
    }

    @Test func aPageWindowOpensAsTheNextTabInTheSameTile() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        workspace.open(.scratchpad)
        workspace.select(.conversation)
        let opener = workspace.openBrowser(URL(string: "https://example.com")!)
        workspace.select(.conversation)
        let child = FakeBrowserEngine()
        child.state.url = URL(string: "https://example.com/login")
        engines.made[0].send(.openedWindow(child))
        let tile = try #require(workspace.tiles[workspace.focusedTile])
        #expect(tile.panes.count == 4)
        #expect(tile.panes[2] == .browser(opener.id))
        let opened = try #require(workspace.browser(tile.panes[3]))
        #expect(workspace.selected == tile.panes[3])
        #expect(opened.url?.absoluteString == "https://example.com/login")
        child.send(.closeRequested)
        #expect(!workspace.panes.contains(tile.panes[3]))
        #expect(child.closed)
    }

    @Test func aWindowOpenedByAClosedTabIsRefused() {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let tab = workspace.openBrowser(URL(string: "https://example.com")!)
        let engine = engines.made[0]
        workspace.close(.browser(tab.id))
        let onEvent = FakeBrowserEngine()
        // The closed tab no longer listens; a late event finds no one.
        engine.send(.openedWindow(onEvent))
        #expect(workspace.panes == [.conversation])
        let orphan = BrowserTab(makeEngine: engines.make)
        orphan.load(URL(string: "https://example.com")!)
        let child = FakeBrowserEngine()
        engines.made.last!.send(.openedWindow(child))
        #expect(child.closed)
    }

    @Test func closingATabStopsItsPage() {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let tab = workspace.openBrowser(URL(string: "https://example.com")!)
        workspace.close(.browser(tab.id))
        #expect(engines.made[0].closed)
        #expect(workspace.browsers.isEmpty)
        #expect(workspace.panes == [.conversation])
    }

    @Test func downloadsReportToTheSessionsToasts() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let feedback = SessionFeedback()
        workspace.feedback = feedback
        workspace.openBrowser(URL(string: "http://localhost:5173/reports")!)
        engines.made[0].send(.downloadFinished(file: URL(filePath: "/Users/me/Downloads/report-q3.pdf"),
                                               source: URL(string: "http://localhost:5173/export")))
        let finished = try #require(feedback.items.last)
        #expect(finished.title == "Downloaded report-q3.pdf")
        #expect(finished.detail == "Saved to Downloads from localhost:5173")
        #expect(finished.tone == .info && !finished.persistent)
        #expect(finished.icon == .arrowDown)
        #expect(finished.actionTitle == "Show in Finder")
        engines.made[0].send(.downloadFailed(filename: "big.zip", message: "The network connection was lost."))
        let failed = try #require(feedback.items.last)
        #expect(failed.title == "Couldn’t download big.zip")
        #expect(failed.tone == .error && failed.persistent)
        for _ in 0..<3 { engines.made[0].send(.downloadBlocked(source: URL(string: "http://localhost:5173/auto.zip"))) }
        let blocked = feedback.items.filter { $0.title == "Blocked a download" }
        #expect(blocked.count == 1)
        #expect(blocked.first?.detail == "localhost:5173 tried to download a file without a click.")
        #expect(blocked.first?.tone == .warning)
    }

    @Test func browserTabsRestoreAtTheirLastAddressWithoutLoading() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let page = workspace.openBrowser(URL(string: "http://localhost:5173/settings")!)
        engines.made[0].change { $0.url = URL(string: "http://localhost:5173/settings/profile")! }
        let blank = workspace.openBrowser()
        workspace.split(.browser(blank.id), .right)
        let saved = workspace.arrangement
        #expect(saved.browsers == [WorkspacePane.browser(page.id).id: "http://localhost:5173/settings/profile"])

        let data = try JSONEncoder().encode(saved)
        let restoredEngines = FakeBrowserEngines()
        let restored = self.workspace(restoredEngines)
        restored.restore(try JSONDecoder().decode(TileArrangement.self, from: data))
        #expect(restored.panes == [.conversation, .browser(page.id), .browser(blank.id)])
        #expect(restored.browser(.browser(page.id))?.url?.absoluteString == "http://localhost:5173/settings/profile")
        #expect(restored.browser(.browser(blank.id))?.url == nil)
        #expect(restored.arrangement == saved)
        #expect(restoredEngines.made.isEmpty)
    }

    @Test func aSavedAddressThatIsNotAWebAddressRestoresBlank() {
        let id = UUID()
        let pane = WorkspacePane.browser(id)
        let workspace = WorkspaceState()
        workspace.restore(TileArrangement(root: .tile(.init(panes: ["conversation", pane.id], selected: pane.id)),
                                          focused: 0, zoomed: false, browsers: [pane.id: "file:///etc/passwd"]))
        #expect(workspace.panes == [.conversation, pane])
        #expect(workspace.browser(pane)?.url == nil)
    }

    @Test func layoutsSavedWithoutBrowsersStillDecode() throws {
        let json = #"{"focused":0,"root":{"panes":["conversation"],"selected":"conversation"},"zoomed":false}"#
        let arrangement = try JSONDecoder().decode(TileArrangement.self, from: Data(json.utf8))
        #expect(arrangement.browsers == nil)
        #expect(arrangement.isDefault)
    }

    @Test func aBrowserPaneIDNamesItsInstance() {
        let id = UUID()
        #expect(WorkspacePane(id: WorkspacePane.browser(id).id) == .browser(id))
        #expect(WorkspacePane(id: "browser:nope") == nil)
        #expect(WorkspacePane(id: "browser:") == nil)
    }

    @Test func anAuthorizationPageHandsBackItsRedirectAndCloses() async throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let page = URL(string: "https://auth.example.com/authorize?state=s1")!
        let redirect = try #require(AuthorizationRedirect(authorization: page, redirectURI: "http://127.0.0.1:53173/mcp/callback"))
        var received: [URL] = []
        let tab = workspace.openAuthorization(page, redirect: redirect, beside: .conversation) { received.append($0); return true }
        let pane = WorkspacePane.browser(tab.id)
        #expect(workspace.root.tiles.count == 2)
        #expect(workspace.selected == pane)
        let engine = engines.made[0]
        #expect(engine.loads == [page])

        // Opening it again shows the waiting tab.
        #expect(workspace.openAuthorization(page, redirect: redirect, beside: .conversation) { _ in true } === tab)
        #expect(engines.made.count == 1)

        // The consent page loads; a redirect for another authorization does too.
        #expect(engine.navigate(URL(string: "https://auth.example.com/consent?state=s1")!))
        #expect(engine.navigate(URL(string: "http://127.0.0.1:53173/mcp/callback?code=c&state=other")!))
        #expect(received.isEmpty)

        let callback = URL(string: "http://127.0.0.1:53173/mcp/callback?code=c&state=s1")!
        #expect(!engine.navigate(callback))
        #expect(received == [callback])
        await Task.yield()
        #expect(workspace.browser(pane) == nil)
        #expect(workspace.root.tiles.count == 1)
        #expect(engine.closed)
    }

    @Test func aRestoredAuthorizationPageTakesTheRedirectInsteadOfOpeningAnother() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let page = URL(string: "https://auth.example.com/authorize?state=s1")!
        let restored = workspace.openBrowser(page, beside: .conversation)
        let redirect = try #require(AuthorizationRedirect(authorization: page, redirectURI: "http://127.0.0.1:53173/mcp/callback"))
        var received: [URL] = []
        let tab = workspace.openAuthorization(page, redirect: redirect, beside: .conversation) { received.append($0); return true }
        #expect(tab === restored)
        #expect(engines.made.count == 1)
        let callback = URL(string: "http://127.0.0.1:53173/mcp/callback?code=c&state=s1")!
        #expect(!engines.made[0].navigate(callback))
        #expect(received == [callback])
    }

    @Test func aRedirectNoLongerWantedLoadsInTheTab() throws {
        let engines = FakeBrowserEngines()
        let workspace = workspace(engines)
        let page = URL(string: "https://auth.example.com/authorize?state=s1")!
        let redirect = try #require(AuthorizationRedirect(authorization: page, redirectURI: "http://127.0.0.1:53173/mcp/callback"))
        let tab = workspace.openAuthorization(page, redirect: redirect, beside: .conversation) { _ in false }
        let callback = URL(string: "http://127.0.0.1:53173/mcp/callback?code=c&state=s1")!
        #expect(engines.made[0].navigate(callback))
        #expect(tab.url == callback)
        #expect(tab.redirectReceiver == nil)
        #expect(workspace.browser(.browser(tab.id)) === tab)
    }
}

struct AuthorizationRedirectTests {
    private let page = URL(string: "https://auth.example.com/authorize?client_id=kit&state=s1")!

    @Test func matchesOnlyTheCallbackCarryingTheAuthorizationsState() throws {
        let redirect = try #require(AuthorizationRedirect(authorization: page, redirectURI: "http://127.0.0.1:53173/mcp/callback"))
        #expect(redirect.callback == URL(string: "http://127.0.0.1:53173/mcp/callback"))
        #expect(redirect.state == "s1")
        #expect(redirect.matches(URL(string: "http://127.0.0.1:53173/mcp/callback?code=c&state=s1")!))
        #expect(redirect.matches(URL(string: "http://127.0.0.1:53173/mcp/callback?error=access_denied&state=s1")!))
        #expect(!redirect.matches(URL(string: "http://127.0.0.1:53173/mcp/callback?code=c")!))
        #expect(!redirect.matches(URL(string: "http://127.0.0.1:53173/mcp/callback?code=c&state=s2")!))
        #expect(!redirect.matches(URL(string: "http://127.0.0.1:53174/mcp/callback?code=c&state=s1")!))
        #expect(!redirect.matches(URL(string: "http://127.0.0.1:53173/other?code=c&state=s1")!))
        #expect(!redirect.matches(URL(string: "https://127.0.0.1:53173/mcp/callback?code=c&state=s1")!))
        #expect(!redirect.matches(URL(string: "http://localhost:53173/mcp/callback?code=c&state=s1")!))
    }

    @Test func needsALoopbackHTTPCallbackAndAState() {
        #expect(AuthorizationRedirect(authorization: page, redirectURI: "http://localhost:8080/callback") != nil)
        #expect(AuthorizationRedirect(authorization: page, redirectURI: "http://[::1]:8080/callback") != nil)
        #expect(AuthorizationRedirect(authorization: page, redirectURI: nil) == nil)
        #expect(AuthorizationRedirect(authorization: page, redirectURI: "https://127.0.0.1:53173/mcp/callback") == nil)
        #expect(AuthorizationRedirect(authorization: page, redirectURI: "http://example.com/callback") == nil)
        #expect(AuthorizationRedirect(authorization: page, redirectURI: "http://127.evil.com/callback") == nil)
        #expect(AuthorizationRedirect(authorization: URL(string: "https://auth.example.com/authorize")!,
                                      redirectURI: "http://127.0.0.1:53173/mcp/callback") == nil)
    }
}

@MainActor struct LiveViewHostTests {
    @Test func aLiveViewMovesToTheHostShowingItAndOnlyItsHolderReleasesIt() {
        let page = NSView()
        let first = LiveViewHost.Container(frame: NSRect(x: 0, y: 0, width: 300, height: 200))
        let second = LiveViewHost.Container(frame: NSRect(x: 0, y: 0, width: 500, height: 400))
        first.show(page)
        #expect(page.superview === first)
        #expect(page.frame == first.bounds)
        second.show(page)
        #expect(page.superview === second)
        #expect(page.frame == second.bounds)
        first.release()
        #expect(page.superview === second)
        second.release()
        #expect(page.superview == nil)
    }
}

struct BrowserUserAgentTests {
    private func bundle(version: String?) throws -> URL {
        let url = FileManager.default.temporaryDirectory.appending(path: "Safari-\(UUID().uuidString).app")
        let contents = url.appending(path: "Contents")
        try FileManager.default.createDirectory(at: contents, withIntermediateDirectories: true)
        var info: [String: Any] = ["CFBundleIdentifier": "com.example.safari-\(UUID().uuidString)"]
        if let version { info["CFBundleShortVersionString"] = version }
        try PropertyListSerialization.data(fromPropertyList: info, format: .xml, options: 0)
            .write(to: contents.appending(path: "Info.plist"))
        return url
    }

    @Test func theInstalledSafarisMajorAndMinorVersionNameThePage() throws {
        let installed = try bundle(version: "26.6.2")
        defer { try? FileManager.default.removeItem(at: installed) }
        #expect(WebKitBrowserEngine.installedSafariVersion(bundle: installed) == "26.6")
        let major = try bundle(version: "27")
        defer { try? FileManager.default.removeItem(at: major) }
        #expect(WebKitBrowserEngine.installedSafariVersion(bundle: major) == "27.0")
    }

    @Test func aMissingOrOddVersionFallsBack() throws {
        let odd = try bundle(version: "beta")
        defer { try? FileManager.default.removeItem(at: odd) }
        #expect(WebKitBrowserEngine.installedSafariVersion(bundle: odd) == nil)
        #expect(WebKitBrowserEngine.installedSafariVersion(bundle: URL(filePath: "/nonexistent/Safari.app")) == nil)
    }
}
