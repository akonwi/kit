import AppKit
import Foundation
import Testing
@testable import Kit

/// Runs Kit's browser agent page script in WebKit against a local page.
@MainActor struct BrowserAgentScriptTests {
    static let page = """
        <!doctype html><html><head><title>Sign in</title></head><body>
        <h1>Welcome</h1>
        <form id="login" onsubmit="event.preventDefault(); document.getElementById('out').textContent = 'Hello, ' + this.user.value + '!'">
          <label for="user">Email</label><input id="user" name="user" value="">
          <label>Password <input type="password" name="pass" value="hunter2"></label>
          <label><input type="checkbox" name="keep"> Keep me signed in</label>
          <select name="plan"><option value="free">Free</option><option value="pro">Pro</option></select>
          <button type="submit">Sign in</button>
        </form>
        <p id="out"></p>
        <div style="display:none"><button>Hidden</button></div>
        <a href="/help">Help</a>
        <button onclick="document.getElementById('out').textContent = 'agent: ' + typeof globalThis.__kitAgent">Probe</button>
        <button onclick="document.getElementById('out').textContent = 'answer: ' + prompt('Your name?', 'nobody')">Ask</button>
        </body></html>
        """

    /// Keeps the page in a window, so it can show dialogs.
    let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 800, height: 600), styleMask: [.titled],
                          backing: .buffered, defer: false)

    private func loadedEngine() async throws -> any BrowserEngine {
        let engine = WebKitBrowserEngine.make()
        window.isReleasedWhenClosed = false
        window.contentView = engine.view
        let data = Data(Self.page.utf8).base64EncodedString()
        engine.load(URL(string: "data:text/html;base64," + data)!)
        try await eventually { !engine.state.loading && engine.state.title == "Sign in" }
        return engine
    }

    private func run(_ engine: any BrowserEngine, _ operation: String, _ arguments: [String: Any]) async throws -> [String: Any] {
        let json = try await engine.runAgentScript(operation, arguments: arguments)
        return try #require(try JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
    }

    @Test func theInteractiveOutlineListsControlsWithRefsAndValues() async throws {
        let engine = try await loadedEngine()
        let snapshot = try await run(engine, "snapshot", ["mode": "interactive"])
        #expect(snapshot["title"] as? String == "Sign in")
        #expect(snapshot["outline"] as? String == """
            - heading "Welcome" level=1
            - textbox "Email" [ref=e1]
            - textbox "Password" [ref=e2] secret value="hunter2"
            - checkbox "Keep me signed in" [ref=e3] unchecked
            - combobox [ref=e4] value="Free" options=["Free", "Pro"]
            - button "Sign in" [ref=e5]
            - link "Help" [ref=e6] -> /help
            - button "Probe" [ref=e7]
            - button "Ask" [ref=e8]
            """)
        engine.close()
    }

    @Test func actionsChangeThePageAndRefsStayStable() async throws {
        let engine = try await loadedEngine()
        _ = try await run(engine, "snapshot", ["mode": "interactive"])
        #expect(try await run(engine, "act", ["action": "fill", "ref": "e1", "text": "Grace"])["ok"] as? Bool == true)
        #expect(try await run(engine, "act", ["action": "check", "ref": "e3"])["ok"] as? Bool == true)
        #expect(try await run(engine, "act", ["action": "select", "ref": "e4", "text": "Pro"])["ok"] as? Bool == true)
        try await Task.sleep(for: .milliseconds(50))
        #expect(try await run(engine, "act", ["action": "click", "ref": "e5"])["ok"] as? Bool == true)
        try await Task.sleep(for: .milliseconds(50))
        let snapshot = try await run(engine, "snapshot", ["mode": "full"])
        #expect(snapshot["outline"] as? String == """
            - heading "Welcome" level=1
            - form
              - textbox "Email" [ref=e1] value="Grace"
              - textbox "Password" [ref=e2] secret value="hunter2"
              - checkbox "Keep me signed in" [ref=e3] checked
              - combobox [ref=e4] value="Pro" options=["Free", "Pro"]
              - button "Sign in" [ref=e5] focused
            - text: "Hello, Grace!"
            - link "Help" [ref=e6] -> /help
            - button "Probe" [ref=e7]
            - button "Ask" [ref=e8]
            """)

        let unknown = try await run(engine, "act", ["action": "click", "ref": "e99"])
        #expect(unknown["error"] as? String == "Unknown ref e99. Take a new snapshot; the page may have changed.")
        let wrong = try await run(engine, "act", ["action": "fill", "ref": "e3", "text": "x"])
        #expect(wrong["error"] as? String == "fill needs a text field; use select, check, or click for other controls.")
        engine.close()
    }

    @Test func thePageCannotSeeTheAgentScript() async throws {
        let engine = try await loadedEngine()
        _ = try await run(engine, "snapshot", ["mode": "interactive"])
        _ = try await run(engine, "act", ["action": "click", "ref": "e7"])
        try await Task.sleep(for: .milliseconds(50))
        let full = try await run(engine, "snapshot", ["mode": "full"])
        #expect((full["outline"] as? String)?.contains(#"- text: "agent: undefined""#) == true)
        engine.close()
    }

    @Test func anActionThatOpensADialogReturnsAndTheAgentCanAnswerIt() async throws {
        let workspace = WorkspaceState()
        workspace.makeBrowserEngine = WebKitBrowserEngine.make
        let data = Data(Self.page.utf8).base64EncodedString()
        let tab = workspace.openBrowser(URL(string: "data:text/html;base64," + data)!)
        window.isReleasedWhenClosed = false
        window.contentView = tab.view
        try await eventually { !tab.loading && tab.page.title == "Sign in" }
        let plugin = BrowserAgentPlugin { workspace }
        plugin.glide = .zero
        plugin.makeFence = { UntrustedPageContent(nonce: "n0nce") }
        let id = BrowserAgentPlugin.tabID(tab)
        func act(_ json: String) async throws -> WireCompleteClientPluginCallInput {
            await plugin.perform(ClientPluginCallRequest(pluginID: "browser", toolID: "act",
                                                         input: try JSONDecoder().decode(WireJSON.self, from: Data(json.utf8))))
        }
        _ = await plugin.perform(ClientPluginCallRequest(pluginID: "browser", toolID: "snapshot",
                                                         input: .object(["tab": .string(id)])))
        let asked = try await act(#"{"tab":"\#(id)","action":"click","ref":"e8"}"#)
        #expect(try tab.agentEngine().pageDialog == BrowserPageDialog(kind: .prompt, message: "Your name?", defaultText: "nobody"))
        #expect(asked.content?.first?.text == """
            Did click on e8. Snapshot of tab \(id) · origin none · full
            A page prompt is open and blocks the page until it is answered; browser__act accept or dismiss answers it, with text for the prompt. The user can also answer it in the app.
            [UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            message: Your name?
            default text: nobody
            [/UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            """)
        let answered = try await act(#"{"tab":"\#(id)","action":"accept","text":"Grace"}"#)
        #expect(try tab.agentEngine().pageDialog == nil)
        #expect(answered.content?.first?.text?.contains(#"- text: "answer: Grace""#) == true)
        tab.close()
    }

    @Test func aTabThatWasNeverShownCanBeReadAndCaptured() async throws {
        let data = Data(Self.page.utf8).base64EncodedString()
        // As restored: the address waits for the page to be created.
        let tab = BrowserTab(url: URL(string: "data:text/html;base64," + data)!, makeEngine: WebKitBrowserEngine.make)
        let engine = try tab.agentEngine()
        #expect(engine.view.frame.size == BrowserTab.unshownPageSize)
        #expect(engine.view.window == nil)
        try await eventually { !engine.state.loading && engine.state.title == "Sign in" }
        let snapshot = try await run(engine, "snapshot", ["mode": "interactive"])
        #expect((snapshot["outline"] as? String)?.contains(#"- textbox "Email" [ref=e1]"#) == true)
        let image = try await engine.viewportImage()
        #expect(image.width > 0 && image.height > 0)
        tab.close()
    }
}
