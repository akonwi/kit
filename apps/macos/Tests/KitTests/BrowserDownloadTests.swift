import AppKit
import Foundation
import Testing
import WebKit
@testable import Kit

/// Downloads in a real WebKit page served from a loopback server.
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

    /// The regression: a window the person's click opened is theirs, so its
    /// download starts, as it would in the tab that opened it.
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
        let opened = page.workspace.panes.compactMap { page.workspace.browser($0) }.filter { $0 !== page.tab }
        #expect(opened.count == 1)
        #expect(opened.first?.blockedDownload == nil)
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
