import AppKit
import Foundation
import Testing
import WebKit
@testable import Kit

/// Downloads in a real WebKit page served from a loopback server: those
/// the person asked for start at once, and others wait for the person.
@MainActor struct BrowserDownloadTests {
    /// A file the server sends as an attachment at `/report.txt`, and at
    /// `/export` for a form.
    static let attachment = LoopbackHTTPServer.Response(
        headers: ["Content-Type": "text/plain", "Content-Disposition": #"attachment; filename="report.txt""#],
        body: "quarterly numbers")

    @MainActor private struct Page {
        let server: LoopbackHTTPServer
        let root: URL
        let folder: URL
        let workspace: WorkspaceState
        let feedback: SessionFeedback
        let tab: BrowserTab

        var engine: WebKitBrowserEngine { get throws { try #require(try tab.agentEngine() as? WebKitBrowserEngine) } }
        var report: URL { folder.appending(path: "report.txt") }
        func requests(_ line: String) -> Int { server.requests.filter { $0.hasPrefix(line + " ") }.count }

        func close() {
            for tab in workspace.panes.compactMap({ workspace.browser($0) }) { tab.close() }
            server.stop()
            try? FileManager.default.removeItem(at: folder)
        }
    }

    /// Opens `/` of a server answering `routes` and the attachment, with
    /// downloads saved in a folder of the test's own.
    private func open(_ body: String, title: String) async throws -> Page {
        let server = try LoopbackHTTPServer(routes: [
            "/": .init(body: "<!doctype html><html><head><title>\(title)</title></head><body>\(body)</body></html>"),
            "/report.txt": Self.attachment, "/export": Self.attachment,
        ])
        let root = try await server.start()
        let folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let workspace = WorkspaceState()
        workspace.makeBrowserEngine = { WebKitBrowserEngine.make(downloadingTo: folder) }
        let feedback = SessionFeedback()
        workspace.feedback = feedback
        let tab = workspace.openBrowser(root.appending(path: "/"))
        _ = try tab.agentEngine()
        return Page(server: server, root: root, folder: folder, workspace: workspace, feedback: feedback, tab: tab)
    }

    private func site(_ page: Page) -> String { "127.0.0.1:\(page.root.port!)" }

    @Test func aFormAPageSendsOnItsOwnWaitsAndIsSentOnce() async throws {
        let page = try await open("""
            <form method="post" action="/export"><input name="q" value="4"></form>
            <script>setTimeout(() => document.forms[0].submit(), 50)</script>
            """, title: "Export")
        defer { page.close() }
        try await eventually { page.tab.waitingDownload != nil }
        #expect(page.tab.waitingDownload == BrowserDownloadRequest(site: site(page), filename: "report.txt"))
        #expect(try page.engine.answerDownload(allow: true))
        try await eventually { FileManager.default.fileExists(atPath: page.report.path) }
        #expect(try String(contentsOf: page.report, encoding: .utf8) == "quarterly numbers")
        #expect(page.tab.waitingDownload == nil)
        try await eventually { page.feedback.items.last?.title == "Downloaded report.txt" }
        // The held response was the one saved; the form was sent once.
        #expect(page.requests("POST /export") == 1)
    }

    @Test func aDeclinedDownloadLeavesThePage() async throws {
        let page = try await open("<script>setTimeout(() => location = '/report.txt', 50)</script>", title: "Reports")
        defer { page.close() }
        try await eventually { page.tab.waitingDownload != nil }
        #expect(try page.engine.answerDownload(allow: false))
        #expect(page.tab.waitingDownload == nil)
        try await eventually { !page.tab.loading }
        #expect(page.tab.page.title == "Reports")
        #expect(try FileManager.default.contentsOfDirectory(atPath: page.folder.path) == [])
    }

    @Test func stoppingAPagesDownloadsLastsUntilTheNextPage() async throws {
        let page = try await open("""
            <a id="file" href="data:text/plain;base64,aGk=" download="notes.txt">Notes</a>
            """, title: "Notes")
        defer { page.close() }
        try await eventually { !page.tab.loading && page.tab.page.title == "Notes" }
        let webView = try #require(try page.engine.view as? WKWebView)
        // A script's click isn't the person's.
        _ = try await webView.evaluateJavaScript("file.click(); file.click(); 'ok'")
        try await eventually { page.tab.waitingDownload != nil }
        #expect(page.tab.waitingDownload == BrowserDownloadRequest(site: site(page)))
        #expect(try page.engine.answerDownload(allow: false, suppress: true))
        _ = try await webView.evaluateJavaScript("file.click(); 'ok'")
        try await Task.sleep(for: .milliseconds(300))
        #expect(page.tab.waitingDownload == nil)

        page.tab.reload()
        try await eventually { !page.tab.loading }
        _ = try await webView.evaluateJavaScript("file.click(); 'ok'")
        try await eventually { page.tab.waitingDownload != nil }
        #expect(try page.engine.answerDownload(allow: true))
        try await eventually { FileManager.default.fileExists(atPath: page.folder.appending(path: "notes.txt").path) }
    }

    @Test func aNewerDownloadTakesTheWaitingOnesPlace() async throws {
        let page = try await open("""
            <a id="notes" href="data:text/plain;base64,aGk=" download="notes.txt">Notes</a>
            <a id="report" href="/report.txt">Report</a>
            """, title: "Files")
        defer { page.close() }
        try await eventually { !page.tab.loading && page.tab.page.title == "Files" }
        let webView = try #require(try page.engine.view as? WKWebView)
        _ = try await webView.evaluateJavaScript("notes.click(); 'ok'")
        try await eventually { page.tab.waitingDownload == BrowserDownloadRequest(site: site(page)) }
        // The page's next download asks in its place; one answer settles it.
        _ = try await webView.evaluateJavaScript("report.click(); 'ok'")
        try await eventually { page.tab.waitingDownload?.filename == "report.txt" }
        #expect(page.tab.waitingDownload == BrowserDownloadRequest(site: site(page), filename: "report.txt"))
        #expect(try page.engine.answerDownload(allow: true))
        try await eventually { FileManager.default.fileExists(atPath: page.report.path) }
        #expect(page.tab.waitingDownload == nil)
        #expect(try FileManager.default.contentsOfDirectory(atPath: page.folder.path) == ["report.txt"])
    }

    @Test func theNextPageDeclinesAWaitingDownload() async throws {
        let page = try await open("""
            <a id="file" href="data:text/plain;base64,aGk=" download="notes.txt">Notes</a>
            """, title: "Notes")
        defer { page.close() }
        try await eventually { !page.tab.loading && page.tab.page.title == "Notes" }
        let webView = try #require(try page.engine.view as? WKWebView)
        _ = try await webView.evaluateJavaScript("file.click(); 'ok'")
        try await eventually { page.tab.waitingDownload != nil }
        page.tab.reload()
        try await eventually { page.tab.waitingDownload == nil }
        #expect(try !page.engine.answerDownload(allow: true))
        try await eventually { !page.tab.loading }
        #expect(try FileManager.default.contentsOfDirectory(atPath: page.folder.path) == [])
    }

    /// The regression: a window the person's click opened is theirs, so its
    /// download starts without asking, as in the tab that opened it.
    @Test func aWindowThePersonsClickOpenedDownloadsAtOnce() async throws {
        let page = try await open("""
            <a href="/report.txt" target="_blank"
               style="position: absolute; left: 0; top: 0; width: 300px; height: 100px">Report</a>
            """, title: "Reports")
        defer { page.close() }
        let view = try page.engine.view
        let stage = try #require(BrowserAgentStage.stage(view, size: BrowserTab.unshownPageSize))
        defer { stage.release() }
        try await eventually { !page.tab.loading && page.tab.page.title == "Reports" }
        try click(view, at: CGPoint(x: 100, y: 50))
        try await eventually { FileManager.default.fileExists(atPath: page.report.path) }
        // The window had only the file to show, so its tab closed, and the
        // download reported as any does.
        try await eventually { page.feedback.items.last?.title == "Downloaded report.txt" }
        #expect(page.workspace.panes == [.conversation, .browser(page.tab.id)])
        #expect(page.workspace.selected == .browser(page.tab.id))
    }

    /// Clicks `view` at `point` from its top-left corner, as the person's
    /// mouse would.
    private func click(_ view: NSView, at point: CGPoint) throws {
        let window = try #require(view.window)
        for (number, type) in [NSEvent.EventType.leftMouseDown, .leftMouseUp].enumerated() {
            let event = try #require(NSEvent.mouseEvent(
                with: type, location: view.convert(point, to: nil), modifierFlags: [],
                timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber, context: nil,
                eventNumber: number + 1, clickCount: 1, pressure: type == .leftMouseDown ? 1 : 0))
            if type == .leftMouseDown { view.mouseDown(with: event) } else { view.mouseUp(with: event) }
        }
    }
}
