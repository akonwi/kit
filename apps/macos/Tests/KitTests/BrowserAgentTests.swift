import AppKit
import Foundation
import SwiftUI
import Testing
@testable import Kit

private func input(_ json: String) throws -> WireJSON {
    try JSONDecoder().decode(WireJSON.self, from: Data(json.utf8))
}

private func call(_ tool: String, _ json: String = "{}") throws -> ClientPluginCallRequest {
    ClientPluginCallRequest(pluginID: "browser", toolID: tool, input: try input(json))
}

private extension WireCompleteClientPluginCallInput {
    var onlyText: String? {
        guard error == nil, let content, content.count == 1, content[0].type == "text" else { return nil }
        return content[0].text
    }
}

@MainActor struct BrowserAgentPluginTests {
    let workspace = WorkspaceState()
    let engines = FakeBrowserEngines()
    let plugin: BrowserAgentPlugin

    init() {
        workspace.makeBrowserEngine = engines.make
        let workspace = workspace
        plugin = BrowserAgentPlugin { workspace }
        plugin.glide = .zero
        plugin.settleLimit = .milliseconds(200)
        plugin.makeFence = { UntrustedPageContent(nonce: "n0nce") }
        plugin.revealLimit = .zero
    }

    /// A tab showing `url`, opened as the agent would, with a live engine.
    private func agentTab(_ url: String = "https://example.com/login") -> (BrowserTab, FakeBrowserEngine) {
        let tab = workspace.openRequestedPage(URL(string: url)!, beside: .conversation)
        let engine = engines.made.last!
        engine.change { $0.title = "Sign in" }
        return (tab, engine)
    }

    private func id(_ tab: BrowserTab) -> String { BrowserAgentPlugin.tabID(tab) }

    @Test func listsTheWindowsTabsWithPageContentFenced() async throws {
        let (agent, _) = agentTab()
        let user = workspace.openBrowser(URL(string: "http://localhost:3000/settings")!)
        engines.made.last!.change { $0.title = "Settings" }
        let result = await plugin.perform(try call("tabs"))
        #expect(result.onlyText == """
            2 browser tabs:
            - tab \(id(agent)) · origin https://example.com · opened by agent · in background
            [UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            title: Sign in
            address: https://example.com/login
            [/UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            - tab \(id(user)) · origin http://localhost:3000 · opened by user · in background
            [UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            title: Settings
            address: http://localhost:3000/settings
            [/UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            """)
    }

    @Test func tabsWaitingForAnAuthorizationRedirectAreNotListed() async throws {
        let page = URL(string: "https://auth.example.com/authorize?state=abc")!
        let redirect = try #require(AuthorizationRedirect(authorization: page, redirectURI: "http://localhost:1455/auth/callback"))
        workspace.openAuthorization(page, redirect: redirect,
                                    beside: .conversation) { _ in true }
        let result = await plugin.perform(try call("tabs"))
        #expect(result.onlyText == "No browser tabs are open. browser__navigate with a url opens one.")
    }

    @Test func anUnknownTabFailsWithAPointerToTheTabList() async throws {
        let result = await plugin.perform(try call("snapshot", #"{"tab":"missing"}"#))
        #expect(result.error == "Unknown tab missing. Call browser__tabs for the tabs open now.")
    }

    @Test func inputOutsideTheDeclaredSchemaIsRefused() async throws {
        let (tab, engine) = agentTab()
        let extra = await plugin.perform(try call("snapshot", #"{"tab":"\#(id(tab))","depth":3}"#))
        #expect(extra.error == "Unexpected input depth.")
        let mode = await plugin.perform(try call("snapshot", #"{"tab":"\#(id(tab))","mode":"everything"}"#))
        #expect(mode.error == "mode must be one of interactive, full.")
        let fill = await plugin.perform(try call("act", #"{"tab":"\#(id(tab))","action":"fill","ref":"e1"}"#))
        #expect(fill.error == "fill needs a ref and text.")
        let ref = await plugin.perform(try call("act", #"{"tab":"\#(id(tab))","action":"click","ref":"login"}"#))
        #expect(ref.error == "ref must be one from a snapshot, like e12.")
        #expect(engine.agentScripts.isEmpty)
    }

    @Test func snapshotFencesThePagesOutline() async throws {
        let (tab, engine) = agentTab()
        engine.agentResults["snapshot"] = #"{"title":"Sign in","url":"https://example.com/login","outline":"- textbox \"Password\" [ref=e2] secret value=\"hunter2\""}"#
        let result = await plugin.perform(try call("snapshot", #"{"tab":"\#(id(tab))"}"#))
        #expect(result.onlyText == """
            Snapshot of tab \(id(tab)) · origin https://example.com · interactive
            [UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            title: Sign in
            address: https://example.com/login
            - textbox "Password" [ref=e2] secret value="hunter2"
            [/UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            """)
        #expect(engine.agentScripts.map(\.operation) == ["snapshot"])
        #expect(engine.agentScripts.first?.arguments["mode"] as? String == "interactive")
    }

    @Test func actingLocatesTheElementActsAndReturnsTheSettledPage() async throws {
        let (tab, engine) = agentTab()
        engine.agentResults["locate"] = #"{"x":40,"y":20,"viewportWidth":800,"viewportHeight":600}"#
        engine.agentResults["act"] = #"{"ok":true}"#
        engine.agentResults["snapshot"] = #"{"title":"Welcome","url":"https://example.com/home","outline":"- text: \"Hello, Grace!\""}"#
        let result = await plugin.perform(try call("act", #"{"tab":"\#(id(tab))","action":"click","ref":"e6"}"#))
        #expect(result.onlyText == """
            Did click on e6. Snapshot of tab \(id(tab)) · origin https://example.com · full
            [UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            title: Welcome
            address: https://example.com/home
            - text: "Hello, Grace!"
            [/UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            """)
        #expect(engine.agentScripts.map(\.operation) == ["locate", "act", "snapshot"])
        #expect(engine.agentScripts[1].arguments["action"] as? String == "click")
        #expect(engine.agentScripts[1].arguments["ref"] as? String == "e6")
        #expect(engine.agentScripts[2].arguments["mode"] as? String == "full")
    }

    @Test func aPageDialogBlocksActionsUntilAnswered() async throws {
        let (tab, engine) = agentTab()
        engine.pageDialog = BrowserPageDialog(kind: .prompt, message: "Your name?", defaultText: "")
        let blocked = await plugin.perform(try call("act", #"{"tab":"\#(id(tab))","action":"click","ref":"e1"}"#))
        #expect(blocked.error == """
            A page prompt is open and blocks the page until it is answered; browser__act accept or dismiss answers it, with text for the prompt. The user can also answer it in the app.
            [UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            message: Your name?
            [/UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            Answer it before acting on the page.
            """)
        engine.agentResults["snapshot"] = #"{"title":"Sign in","url":"https://example.com/login","outline":"- text: \"Hello, Grace!\""}"#
        let answered = await plugin.perform(try call("act", #"{"tab":"\#(id(tab))","action":"accept","text":"Grace"}"#))
        #expect(engine.dialogAnswers.map(\.accept) == [true])
        #expect(engine.dialogAnswers.map(\.text) == ["Grace"])
        #expect(answered.onlyText == """
            Answered the prompt. Snapshot of tab \(id(tab)) · origin https://example.com · full
            [UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            title: Sign in
            address: https://example.com/login
            - text: "Hello, Grace!"
            [/UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            """)
        let none = await plugin.perform(try call("act", #"{"tab":"\#(id(tab))","action":"dismiss"}"#))
        #expect(none.error == "The tab has no open page dialog or file chooser.")
    }

    @Test func pointerAndKeyActionsSendThePageNativeInput() async throws {
        let (tab, engine) = agentTab()
        engine.agentResults["locate"] = #"{"x":40,"y":20,"viewportWidth":1280,"viewportHeight":800}"#
        engine.agentResults["act"] = #"{"ok":true}"#
        engine.agentResults["snapshot"] = #"{"title":"","url":"https://example.com/login","outline":""}"#
        func act(_ json: String) async throws -> WireCompleteClientPluginCallInput {
            await plugin.perform(try call("act", #"{"tab":"\#(id(tab))",\#(json)}"#))
        }
        #expect(try await act(#""action":"hover","x":100,"y":50"#).error == nil)
        #expect(engine.inputs == [.move(CGPoint(x: 100, y: 50))])
        #expect(try await act(#""action":"press","ref":"e1","key":"Shift+Tab""#).error == nil)
        #expect(engine.agentScripts.suffix(3).map(\.operation) == ["locate", "act", "snapshot"])
        #expect(engine.agentScripts.suffix(2).first?.arguments["action"] as? String == "focus")
        #expect(engine.inputs.last == .keys([BrowserKey(characters: "\t", keyCode: 48, modifiers: .shift)]))
        #expect(try await act(#""action":"type","text":"Hi\n""#).error == nil)
        #expect(engine.inputs.last == .keys([BrowserKey(characters: "H", keyCode: 4, modifiers: .shift),
                                             BrowserKey(characters: "i", keyCode: 34), BrowserKey(characters: "\r", keyCode: 36)]))
        let before = engine.inputs.count
        #expect(try await act(#""action":"drag","ref":"e1","to_x":140,"to_y":20"#).error == nil)
        let dragged = (1...10).map { BrowserInput.dragged(CGPoint(x: 40 + 10 * Double($0), y: 20)) }
        #expect(Array(engine.inputs[before...]) == [.move(CGPoint(x: 40, y: 20)), .down(CGPoint(x: 40, y: 20), clicks: 1)]
                + dragged + [.up(CGPoint(x: 140, y: 20), clicks: 1)])
    }

    @Test func coordinatesArePixelsOfTheTabsScreenshot() async throws {
        let (tab, engine) = agentTab()
        engine.agentResults["snapshot"] = #"{"title":"","url":"https://example.com/login","outline":""}"#
        // A view longer than the screenshot side is captured at half size.
        engine.view.frame = NSRect(x: 0, y: 0, width: 3200, height: 1000)
        let clicked = await plugin.perform(try call("act", #"{"tab":"\#(id(tab))","action":"click","x":100,"y":50}"#))
        #expect(clicked.error == nil)
        #expect(engine.inputs == [.move(CGPoint(x: 200, y: 100)), .down(CGPoint(x: 200, y: 100), clicks: 1),
                                  .up(CGPoint(x: 200, y: 100), clicks: 1)])
        let outside = await plugin.perform(try call("act", #"{"tab":"\#(id(tab))","action":"click","x":1700,"y":50}"#))
        #expect(outside.error == "x and y must be within the screenshot, 1600×500 pixels.")
    }

    @Test func actionsTakeOnlyTheirOwnInputs() async throws {
        let (tab, _) = agentTab()
        func error(_ json: String) async throws -> String? {
            await plugin.perform(try call("act", #"{"tab":"\#(id(tab))",\#(json)}"#)).error
        }
        #expect(try await error(#""action":"drag","ref":"e1""#) == "drag needs a to_ref or to_x and to_y to drop on.")
        #expect(try await error(#""action":"click","ref":"e1","x":1,"y":2"#) == "Give a ref or x and y, not both.")
        #expect(try await error(#""action":"hover","x":1"#) == "x and y go together.")
        #expect(try await error(#""action":"type","text":"a","key":"Enter""#) == "type takes no key.")
        #expect(try await error(#""action":"press","key":"Hyper+a""#)
                == "key must be a key name like Enter, Shift+Tab, or a single character.")
        #expect(try await error(#""action":"choose_files","files":"a.txt""#) == "files must be an array of strings.")
    }

    @Test func aFileChooserTakesFilesOnlyFromTheWorkingDirectory() async throws {
        let root = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        let outside = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        try FileManager.default.createDirectory(at: outside, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root); try? FileManager.default.removeItem(at: outside) }
        try Data("resume".utf8).write(to: root.appending(path: "resume.pdf"))
        try Data("key".utf8).write(to: outside.appending(path: "id_ed25519"))
        try FileManager.default.createSymbolicLink(at: root.appending(path: "link"), withDestinationURL: outside.appending(path: "id_ed25519"))
        let workspace = workspace
        let plugin = BrowserAgentPlugin(workspace: { workspace }, workingDirectory: { root.path })
        plugin.glide = .zero
        plugin.settleLimit = .milliseconds(200)
        plugin.revealLimit = .zero
        let (tab, engine) = agentTab()
        engine.agentResults["snapshot"] = #"{"title":"","url":"https://example.com/login","outline":""}"#
        engine.pageDialog = BrowserPageDialog(kind: .files)
        func choose(_ files: String) async throws -> String? {
            await plugin.perform(try call("act", #"{"tab":"\#(id(tab))","action":"choose_files","files":\#(files)}"#)).error
        }
        #expect(try await choose(#"["../\#(outside.lastPathComponent)/id_ed25519"]"#)
                == "../\(outside.lastPathComponent)/id_ed25519 is outside the session’s working directory.")
        #expect(try await choose(#"["link"]"#) == "link is outside the session’s working directory.")
        #expect(try await choose(#"["resume.pdf","resume.pdf"]"#) == "This file chooser takes one file.")
        #expect(try await choose(#"["missing.pdf"]"#) == "missing.pdf isn’t a readable file.")
        #expect(try await choose(#"["resume.pdf"]"#) == nil)
        let resolved = root.resolvingSymlinksInPath().appending(path: "resume.pdf").standardizedFileURL
        #expect(engine.chosenFiles == [[resolved]])
    }

    @Test func actingOnATabBringsItToTheFrontWithoutTakingFocus() async throws {
        let (behind, engine) = agentTab()
        engine.agentResults["snapshot"] = #"{"title":"","url":"https://example.com/login","outline":""}"#
        let front = workspace.openBrowser(URL(string: "https://example.com/other")!, in: workspace.tile(containing: .browser(behind.id)))
        workspace.focus(tile: try #require(workspace.tile(containing: .conversation)))
        let focused = workspace.focusedTile
        #expect(workspace.isVisible(.browser(front.id)))
        #expect(await plugin.perform(try call("snapshot", #"{"tab":"\#(id(behind))"}"#)).error == nil)
        #expect(workspace.isVisible(.browser(behind.id)))
        #expect(workspace.focusedTile == focused)
    }

    @Test func actionsOnOneTabRunInTheOrderTheyArrive() async throws {
        let (tab, engine) = agentTab()
        engine.agentResults["locate"] = #"{"x":1,"y":1,"viewportWidth":800,"viewportHeight":600}"#
        engine.agentResults["act"] = #"{"ok":true}"#
        engine.agentResults["snapshot"] = #"{"title":"","url":"https://example.com/login","outline":""}"#
        let first = try call("act", #"{"tab":"\#(id(tab))","action":"fill","ref":"e1","text":"grace"}"#)
        let second = try call("act", #"{"tab":"\#(id(tab))","action":"fill","ref":"e2","text":"hunter2"}"#)
        async let a = plugin.perform(first)
        async let b = plugin.perform(second)
        _ = await (a, b)
        let refs = engine.agentScripts.map { "\($0.operation):\($0.arguments["ref"] as? String ?? "-")" }
        #expect(refs == ["locate:e1", "act:e1", "snapshot:-", "locate:e2", "act:e2", "snapshot:-"])
    }

    @Test func aCallWaitingOnATabThatClosesFailsWithoutReopeningIt() async throws {
        let (tab, engine) = agentTab()
        engine.agentResults["locate"] = #"{"x":1,"y":1,"viewportWidth":800,"viewportHeight":600}"#
        engine.agentResults["act"] = #"{"ok":true}"#
        engine.agentResults["snapshot"] = #"{"title":"","url":"https://example.com/login","outline":""}"#
        let first = try call("act", #"{"tab":"\#(id(tab))","action":"click","ref":"e1"}"#)
        let second = try call("act", #"{"tab":"\#(id(tab))","action":"click","ref":"e2"}"#)
        async let a = plugin.perform(first)
        async let b = plugin.perform(second)
        try await eventually { engine.agentScripts.count >= 1 }
        workspace.close(.browser(tab.id))
        let (_, waited) = await (a, b)
        #expect(waited.error == "The tab was closed. Call browser__tabs for the tabs open now.")
        #expect(engines.made.count == 1)
    }

    @Test func aRestoredTabThatWasNeverShownLoadsBeforeItIsRead() async throws {
        let pane = "browser:" + UUID().uuidString.lowercased()
        workspace.restore(TileArrangement(root: .tile(.init(panes: ["conversation", pane], selected: "conversation")),
                                          focused: 0, zoomed: false, browsers: [pane: "https://example.com/hidden"]))
        let restored = try #require(WorkspacePane(id: pane))
        let tab = try #require(workspace.browser(restored))
        #expect(engines.made.isEmpty)
        let result = await plugin.perform(try call("snapshot", #"{"tab":"\#(id(tab))"}"#))
        let engine = try #require(engines.made.first)
        #expect(engine.loads == [URL(string: "https://example.com/hidden")!])
        #expect(engine.view.frame.size == BrowserTab.unshownPageSize)
        #expect(engine.agentScripts.map(\.operation) == ["snapshot"])
        #expect(result.error == "No result for snapshot.")
    }

    @Test func navigatingWithoutATabOpensOneBesideTheConversation() async throws {
        let refused = await plugin.perform(try call("navigate", #"{"url":"file:///etc/passwd"}"#))
        #expect(refused.error == "url must be an absolute http or https address without credentials.")
        #expect(engines.made.isEmpty)

        let result = await plugin.perform(try call("navigate", #"{"url":"https://example.com/"}"#))
        let tab = try #require(workspace.panes.compactMap(workspace.browser).first)
        #expect(tab.opener == .agent)
        #expect(engines.made.map(\.loads) == [[URL(string: "https://example.com/")!]])
        #expect(result.onlyText == """
            Tab \(id(tab)) · origin https://example.com
            [UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            title: example.com
            address: https://example.com/
            [/UNTRUSTED_PAGE_CONTENT nonce=n0nce]
            The page has loaded; browser__snapshot reads it.
            """)
    }

    @Test func screenshotsAreBoundedImages() throws {
        let context = CGContext(data: nil, width: 3200, height: 1000, bitsPerComponent: 8, bytesPerRow: 0,
                                space: CGColorSpace(name: CGColorSpace.sRGB)!,
                                bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        context.setFillColor(CGColor(red: 0.2, green: 0.4, blue: 0.8, alpha: 1))
        context.fill(CGRect(x: 0, y: 0, width: 3200, height: 1000))
        let (data, type) = try BrowserAgentPlugin.encode(context.makeImage()!)
        let image = try #require(NSBitmapImageRep(data: data))
        #expect(type == "image/png")
        #expect(image.pixelsWide == 1600)
        #expect(image.pixelsHigh == 500)
    }

    @Test func theDeclarationIsAValidAttachment() throws {
        let declaration = BrowserAgentPlugin.declaration
        #expect(declaration.pluginId == "browser")
        #expect(declaration.tools.map(\.id) == ["tabs", "snapshot", "act", "screenshot", "navigate"])
        #expect(declaration.tools.map(\.executionMode) == ["parallel", "parallel", nil, "parallel", nil])
        // The generated client accepts it as the attach request body.
        let data = try JSONEncoder().encode(declaration)
        _ = try JSONDecoder().decode(Components.Schemas.AttachClientPluginInput.self, from: data)
    }
}

/// A session client that hosts plugin instances and records what it's asked.
private actor HostingClient: ClientPluginHostingClient {
    nonisolated let serverID = "hosting-test"
    nonisolated let isDemo = false
    let session: SessionExcerpt
    let events: [ClientPluginCallEvent]
    private(set) var hosted: [String] = []
    private(set) var completions: [(call: String, result: WireCompleteClientPluginCallInput)] = []
    private(set) var detached: [String] = []

    init(session: SessionExcerpt, events: [ClientPluginCallEvent]) {
        self.session = session
        self.events = events
    }

    func sessions() async throws -> [SessionExcerpt] { [session] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { session }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        try await watch(id, hosting: nil, receive: receive)
    }
    func watch(_ id: String, hosting: ClientPluginHosting?,
               receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        if let hosting { hosted.append(hosting.instanceID) }
        await receive(session)
        for event in events { await hosting?.receive(event) }
        try await Task.sleep(for: .seconds(60))
    }
    func detachClientPlugin(session id: String, instance: String) async throws { detached.append(instance) }
    func completeClientPluginCall(session id: String, instance: String, call: String,
                                  result: WireCompleteClientPluginCallInput) async throws {
        completions.append((call, result))
    }
}

@MainActor struct ClientPluginHostTests {
    private let callID = "clientcall_" + String(repeating: "a", count: 32)

    private func excerpt() -> SessionExcerpt {
        SessionExcerpt(id: "s", title: "Hosting", sourceTitle: "test", model: "m", thinking: "off", workspace: "tmp", date: "", messages: [])
    }

    @Test func aWindowHostsTheBrowserPluginAndAnswersEachCallOnce() async throws {
        let request = ClientPluginCallRequest(pluginID: "browser", toolID: "tabs", input: .object([:]))
        let client = HostingClient(session: excerpt(), events: [
            .requested(id: callID, call: request),
            // Delivered again, as after a reconnect.
            .requested(id: callID, call: request),
        ])
        let store = SessionStore(fixture: Fixture(sessions: [excerpt()]), client: client)
        store.attach()
        try await eventually { await client.completions.count == 1 }
        try await Task.sleep(for: .milliseconds(50))
        let completions = await client.completions
        #expect(completions.map(\.call) == [callID])
        #expect(completions.first?.result.content?.first?.text == "No browser tabs are open. browser__navigate with a url opens one.")
        let hosted = await client.hosted
        #expect(hosted.count == 1)
        #expect(hosted.allSatisfy(ClientPluginHosting.validInstanceID))

        store.detach()
        store.stop()
        try await eventually { await client.detached == hosted }
    }

    @Test func callEventsAreReadOnlyForTheirInstance() throws {
        let instance = ClientPluginHosting.newInstanceID()
        func event(_ json: String) throws -> WireSessionEvent {
            try JSONDecoder().decode(WireSessionEvent.self, from: Data(json.utf8))
        }
        let requested = try event("""
            {"streamId":"stream_1","sequence":4,"sessionId":"s","kind":"client_plugin.call.requested",
             "clientPluginCallId":"\(callID)","clientPluginInstanceId":"\(instance)",
             "clientPluginCall":{"pluginId":"browser","toolId":"snapshot","toolCallId":"call_1","subagentConversationId":"conversation_2","input":{"tab":"t"}}}
            """)
        guard case .requested(let id, let call)? = ClientPluginCallEvent(requested, instance: instance) else {
            Issue.record("The request wasn't read")
            return
        }
        #expect(id == callID)
        #expect(call.toolID == "snapshot")
        #expect(call.subagentConversationID == "conversation_2")
        #expect(ClientPluginCallEvent(requested, instance: ClientPluginHosting.newInstanceID()) == nil)

        // Another stream sees the event without its call.
        let withheld = try event("""
            {"streamId":"stream_1","sequence":4,"sessionId":"s","kind":"client_plugin.call.requested",
             "clientPluginCallId":"\(callID)","clientPluginInstanceId":"\(instance)"}
            """)
        #expect(ClientPluginCallEvent(withheld, instance: instance) == nil)

        let resolved = try event("""
            {"streamId":"stream_1","sequence":5,"sessionId":"s","kind":"client_plugin.call.resolved",
             "clientPluginCallId":"\(callID)","clientPluginInstanceId":"\(instance)","clientPluginCallResolution":"withdrawn"}
            """)
        guard case .resolved(_, let resolution)? = ClientPluginCallEvent(resolved, instance: instance) else {
            Issue.record("The resolution wasn't read")
            return
        }
        #expect(resolution == "withdrawn")
    }
}

@MainActor struct AgentPointerTests {
    @Test func thePressRingNeverProducesNonFiniteValues() {
        let timeline = KeyframeTimeline(initialValue: PressRing.Frame()) { PressRing.tracks }
        for step in 0...60 {
            let frame = timeline.value(time: Double(step) / 100)
            #expect(frame.scale.isFinite && frame.opacity.isFinite)
        }
        #expect(timeline.value(time: 0).opacity == 0.9)
        #expect(timeline.value(time: timeline.duration).opacity == 0)
        #expect(timeline.value(time: timeline.duration).scale == 1.6)
    }

    @Test func thePointerFollowsActionsAndFadesAfterLingering() async throws {
        let tab = BrowserTab(makeEngine: FakeBrowserEngines().make)
        tab.moveAgentPointer(to: CGPoint(x: 40, y: 900), in: CGSize(width: 800, height: 600))
        #expect(tab.agentPointer == AgentPointer(location: CGPoint(x: 40, y: 600)))
        tab.pressAgentPointer()
        #expect(tab.agentPointer?.presses == 1)
        tab.moveAgentPointer(to: CGPoint(x: CGFloat.nan, y: 10), in: CGSize(width: 800, height: 600))
        #expect(tab.agentPointer?.location == CGPoint(x: 40, y: 600))
        tab.releaseAgentPointer()
        #expect(tab.agentPointer?.visible == true)
        try await eventually { tab.agentPointer?.visible == false }
    }
}
