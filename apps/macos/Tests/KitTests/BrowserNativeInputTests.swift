import AppKit
import Foundation
import Testing
import WebKit
@testable import Kit

/// Delivers native input to a real WebKit page staged off screen, as the
/// agent does on a page whose session tab isn't shown.
@MainActor struct BrowserNativeInputTests {
    static let page = """
        <!doctype html><html><head><title>Input</title><style>
        body { margin: 0 } #pad { position: absolute; left: 100px; top: 100px; width: 200px; height: 100px }
        #field { position: absolute; left: 100px; top: 300px; width: 200px }
        </style></head><body>
        <div id="pad"></div><input id="field">
        <script>
        window.log = [];
        const pad = document.getElementById('pad');
        for (const type of ['mouseenter', 'mousedown', 'mouseup', 'click']) {
          pad.addEventListener(type, e => log.push(type + (e.isTrusted ? '' : ' untrusted') + ' ' + e.offsetX + ',' + e.offsetY));
        }
        document.getElementById('field').addEventListener('keydown', e => log.push('key ' + e.key + (e.isTrusted ? '' : ' untrusted')));
        </script></body></html>
        """

    private func stagedTab() async throws -> (BrowserTab, any BrowserEngine, BrowserAgentStage) {
        let data = Data(Self.page.utf8).base64EncodedString()
        let tab = BrowserTab(url: URL(string: "data:text/html;base64," + data)!, makeEngine: WebKitBrowserEngine.make)
        let engine = try tab.agentEngine()
        let stage = try #require(BrowserAgentStage.stage(engine.view, size: BrowserTab.unshownPageSize))
        try await eventually { !engine.state.loading && engine.state.title == "Input" }
        return (tab, engine, stage)
    }

    /// Evaluates `expression` in the page's own world.
    private func page(_ engine: any BrowserEngine, _ expression: String) async throws -> String {
        let webView = try #require(engine.view as? WKWebView)
        return try #require(try await webView.evaluateJavaScript(expression) as? String)
    }

    private func log(_ engine: any BrowserEngine) async throws -> String { try await page(engine, "log.join('|')") }

    @Test func pointerInputReachesAStagedPageAsAPersons() async throws {
        let (tab, engine, stage) = try await stagedTab()
        defer { stage.release(); tab.close() }
        try engine.sendInput(.move(CGPoint(x: 150, y: 130)))
        try engine.sendInput(.down(CGPoint(x: 150, y: 130), clicks: 1))
        try engine.sendInput(.up(CGPoint(x: 150, y: 130), clicks: 1))
        try await eventually { (try? await log(engine))?.contains("click") == true }
        #expect(try await log(engine) == "mouseenter 50,30|mousedown 50,30|mouseup 50,30|click 50,30")
    }

    @Test func keysTypeIntoTheFocusedFieldOfAStagedPage() async throws {
        let (tab, engine, stage) = try await stagedTab()
        defer { stage.release(); tab.close() }
        try engine.sendInput(.down(CGPoint(x: 150, y: 310), clicks: 1))
        try engine.sendInput(.up(CGPoint(x: 150, y: 310), clicks: 1))
        try engine.sendInput(.keys(["H", "i", "Enter"].compactMap(BrowserKey.init)))
        try await eventually { (try? await log(engine))?.contains("Enter") == true }
        #expect(try await log(engine) == "key H|key i|key Enter")
        let value = try await page(engine, "document.getElementById('field').value")
        #expect(value == "Hi")
    }

    static let app = """
        <!doctype html><html><head><title>App</title><style>body { margin: 0 }</style></head><body>
        <canvas id="board" width="400" height="200" style="display:block"></canvas>
        <input id="name" aria-label="Name"><input id="file" type="file" aria-label="Upload">
        <button onclick="out.textContent = 'confirmed: ' + confirm('Delete everything?')">Delete</button>
        <p id="out"></p>
        <script>
        const out = document.getElementById('out');
        board.addEventListener('click', e => out.textContent = 'board ' + e.offsetX + ',' + e.offsetY + (e.isTrusted ? '' : ' untrusted'));
        file.addEventListener('change', () => out.textContent = 'file ' + file.files[0].name + ' ' + file.files[0].size);
        </script></body></html>
        """

    @Test func theAgentWorksAPageWhoseSessionIsntShown() async throws {
        let root = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }
        try Data("resume".utf8).write(to: root.appending(path: "resume.txt"))
        let workspace = WorkspaceState()
        workspace.makeBrowserEngine = WebKitBrowserEngine.make
        let tab = workspace.openBrowser(URL(string: "data:text/html;base64," + Data(Self.app.utf8).base64EncodedString())!)
        defer { tab.close() }
        try await eventually { !tab.loading && tab.page.title == "App" }
        let plugin = BrowserAgentPlugin(workspace: { workspace }, workingDirectory: { root.path })
        plugin.glide = .zero
        plugin.revealLimit = .zero
        let id = BrowserAgentPlugin.tabID(tab)
        func act(_ json: String) async throws -> WireCompleteClientPluginCallInput {
            let input = try JSONDecoder().decode(WireJSON.self, from: Data(#"{"tab":"\#(id)",\#(json)}"#.utf8))
            return await plugin.perform(ClientPluginCallRequest(pluginID: "browser", toolID: "act", input: input))
        }
        func page() async throws -> String {
            let webView = try #require(try tab.agentEngine().view as? WKWebView)
            return try #require(try await webView.evaluateJavaScript("out.textContent") as? String)
        }
        _ = await plugin.perform(ClientPluginCallRequest(pluginID: "browser", toolID: "snapshot", input: .object(["tab": .string(id)])))

        #expect(try await act(#""action":"click","x":120,"y":80"#).error == nil)
        #expect(try await page() == "board 120,80")

        #expect(try await act(#""action":"type","ref":"e1","text":"Grace""#).error == nil)
        let webView = try #require(try tab.agentEngine().view as? WKWebView)
        #expect(try await webView.evaluateJavaScript("document.getElementById('name').value") as? String == "Grace")

        let chooser = try await act(#""action":"click","ref":"e2""#)
        #expect(chooser.error == nil)
        #expect(tab.pageDialog?.kind == .files)
        #expect(try await act(#""action":"choose_files","files":["resume.txt"]"#).error == nil)
        try await eventually { (try? await page()) == "file resume.txt 6" }

        // The confirm waits for an answer, marking the tab, while the page is in no window.
        let asked = try await act(#""action":"click","ref":"e3""#)
        #expect(asked.error == nil)
        #expect(tab.pageDialog == BrowserPageDialog(kind: .confirm, message: "Delete everything?"))
        #expect(try tab.agentEngine().view.window == nil)
        try await Task.sleep(for: .milliseconds(300))
        #expect(tab.pageDialog?.kind == .confirm)
        #expect(try await act(#""action":"dismiss""#).error == nil)
        #expect(tab.pageDialog == nil)
        try await eventually { (try? await page()) == "confirmed: false" }
    }

    @Test func theAgentsClickDoesNotLetAPageStartADownload() async throws {
        let page = """
            <!doctype html><html><head><title>Files</title></head><body>
            <a href="data:text/plain;base64,aGk=" download="notes.txt">Download</a></body></html>
            """
        let workspace = WorkspaceState()
        workspace.makeBrowserEngine = WebKitBrowserEngine.make
        let tab = workspace.openBrowser(URL(string: "data:text/html;base64," + Data(page.utf8).base64EncodedString())!)
        defer { tab.close() }
        try await eventually { !tab.loading && tab.page.title == "Files" }
        let plugin = BrowserAgentPlugin { workspace }
        plugin.glide = .zero
        plugin.revealLimit = .zero
        let id = BrowserAgentPlugin.tabID(tab)
        _ = await plugin.perform(ClientPluginCallRequest(pluginID: "browser", toolID: "snapshot", input: .object(["tab": .string(id)])))
        let input = try JSONDecoder().decode(WireJSON.self, from: Data(#"{"tab":"\#(id)","action":"press","ref":"e1","key":"Enter"}"#.utf8))
        #expect(await plugin.perform(ClientPluginCallRequest(pluginID: "browser", toolID: "act", input: input)).error == nil)
        // The tab offers the blocked download to the person instead.
        try await eventually { tab.blockedDownload != nil }
    }
}
