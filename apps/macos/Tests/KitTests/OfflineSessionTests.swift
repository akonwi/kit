import AppKit
@preconcurrency import CodeEditSourceEditor
@preconcurrency import CodeEditTextView
import SwiftUI
import Testing
@testable import Kit

/// devbox's handshake, which a test turns down and back up.
private actor DevboxHandshake {
    static let identity = ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: "i",
                                         kitVersion: "0.48.0", protocolVersion: kitWireVersion,
                                         machine: String(repeating: "b", count: 64), home: "/Users/kit")
    private var failure: (any Error)?
    func fail(with error: (any Error)?) { failure = error }
    func answer() throws -> ServerIdentity {
        if let failure { throw failure }
        return Self.identity
    }
}

/// devbox's session "a", which its watch sends once and then holds open.
private struct OpenSessionClient: SessionClient {
    let serverID = "devbox"
    let isDemo = false
    static let session = SessionExcerpt(id: "a", title: "Load test plan", sourceTitle: "Test", model: "m", thinking: "high",
                                        workspace: "/work", date: "",
                                        messages: [TranscriptMessage(id: "m", role: "assistant", text: "Last known reply", tools: [])])
    func sessions() async throws -> [SessionExcerpt] { [Self.session] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { Self.session }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(Self.session)
        try await Task.sleep(for: .seconds(3600))
    }
}

/// A session tab keeps its session on screen, read-only, while its server
/// can't serve it, and picks up where it was when the server is back.
@MainActor struct OfflineSessionTests {
    private static let fast = ServerConnection.Schedule(firstRetry: .milliseconds(20), maximumRetry: .milliseconds(40),
                                                       check: .seconds(60))

    /// This Mac's server and devbox, whose handshake `devbox` answers.
    private func servers(_ devbox: DevboxHandshake) -> ServerDirectory {
        ServerDirectory(home: URL(fileURLWithPath: "/tmp/kit-home"),
                        saved: { [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox")] }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector),
                             catalogClient: FixtureClient(fixture: Fixture(sessions: [])), schedule: Self.fast) {
                if name == "devbox" { return try await devbox.answer() }
                return DevboxHandshake.identity
            }
        }
    }

    /// A window with devbox's session "a" open in its only tab, with a
    /// draft, and the scratchpad open in a tile beside the conversation.
    private func openSession(_ devbox: DevboxHandshake) async throws -> (ServerDirectory, SessionWindowModel, SessionStore) {
        let servers = servers(devbox)
        servers.start()
        let model = SessionWindowModel(id: UUID(), servers: servers, sessions: [SessionIdentity(server: "devbox", session: "a")])
        let connection = try #require(servers.connection(named: "devbox"))
        try await eventually { connection.status.isConnected }
        let store = try #require(model.store(for: model.tabs[0], client: OpenSessionClient()))
        try await eventually { store.opening == .open && store.connectionState == .connected }
        store.ui.draft = "Also check the zoomed tile"
        store.ui.workspace.open(.scratchpad)
        store.ui.workspace.split(.scratchpad, .right)
        return (servers, model, store)
    }

    /// Takes devbox down with `error` and waits until the app sees it.
    private func takeDown(_ servers: ServerDirectory, _ devbox: DevboxHandshake, with error: any Error = ClientError.noDaemon,
                          until reached: @escaping (ServerStatus) -> Bool) async throws {
        await devbox.fail(with: error)
        let connection = try #require(servers.connection(named: "devbox"))
        try await eventually {
            if !reached(connection.status) { connection.checkNow() }
            return reached(connection.status)
        }
    }

    private func bringBack(_ servers: ServerDirectory, _ devbox: DevboxHandshake) async throws {
        await devbox.fail(with: nil)
        let connection = try #require(servers.connection(named: "devbox"))
        connection.checkNow()
        try await eventually { connection.status.isConnected }
    }

    private static func unreachable(_ status: ServerStatus) -> Bool {
        if case .unreachable = status { true } else { false }
    }

    @Test func aShownSessionStaysReadOnlyWhileItsServerIsDownAndResumesWhenItIsBack() async throws {
        let devbox = DevboxHandshake()
        let (servers, model, store) = try await openSession(devbox)
        defer { model.closeAll(); for connection in servers.connections { connection.stop() } }
        let tab = model.tabs[0]
        let arrangement = store.ui.workspace.arrangement
        #expect(tab.showsSession)
        #expect(!store.offline)
        #expect(store.live)

        try await takeDown(servers, devbox, until: Self.unreachable)
        // The tab keeps its session, tiles, and draft; nothing can be sent.
        #expect(tab.showsSession)
        #expect(tab.serverIssue == "Can’t connect")
        #expect(store.offline)
        #expect(!store.live)
        #expect(store.opening == .open)
        #expect(store.messages.map(\.text) == ["Last known reply"])
        #expect(store.ui.draft == "Also check the zoomed tile")
        #expect(store.ui.workspace.arrangement == arrangement)

        try await bringBack(servers, devbox)
        #expect(!store.offline)
        #expect(model.store(for: tab) === store)
        try await eventually { store.live }
        #expect(store.ui.draft == "Also check the zoomed tile")
        #expect(store.ui.workspace.arrangement == arrangement)
    }

    @Test func whatWouldChangeTheSessionWaitsForItsServer() async throws {
        let devbox = DevboxHandshake()
        let (servers, model, store) = try await openSession(devbox)
        defer { model.closeAll(); for connection in servers.connections { connection.stop() } }
        store.ui.workspace.scratchpad = "Unsaved plan"
        #expect(store.newSessionUnavailableReason == nil)

        try await takeDown(servers, devbox, until: Self.unreachable)
        #expect(store.newSessionUnavailableReason == "Connect to create")
        // Closing the scratchpad would save it; it stays open with its changes.
        store.closePane(.scratchpad)
        #expect(store.ui.workspace.panes.contains(.scratchpad))
        #expect(store.ui.workspace.scratchpad == "Unsaved plan")
        #expect(store.feedback.items.map(\.title) == ["Connect to save the scratchpad"])

        try await bringBack(servers, devbox)
        try await eventually { store.live }
        #expect(store.newSessionUnavailableReason == nil)
    }

    @Test func theScratchpadPaneStopsTakingTypingWhileItsServerIsDown() async throws {
        let devbox = DevboxHandshake()
        let (servers, model, store) = try await openSession(devbox)
        defer { model.closeAll(); for connection in servers.connections { connection.stop() } }
        store.ui.workspace.scratchpad = "offline"
        let host = NSHostingView(rootView: ScratchpadPane(state: store).environment(\.mica, MicaTheme(dark: true)))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 500, height: 300))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        try await eventually { InlineAnnotationEditorTests.controller(in: host) != nil }
        let controller = try #require(InlineAnnotationEditorTests.controller(in: host))
        #expect(controller.textView.isEditable)

        try await takeDown(servers, devbox, until: Self.unreachable)
        try await eventually { !controller.textView.isEditable }
        try await bringBack(servers, devbox)
        try await eventually { controller.textView.isEditable }
    }

    @Test func aScratchpadFirstShownWhileItsServerIsDownTakesNoTyping() async throws {
        let devbox = DevboxHandshake()
        let (servers, model, store) = try await openSession(devbox)
        defer { model.closeAll(); for connection in servers.connections { connection.stop() } }
        store.ui.workspace.scratchpad = "offline"
        try await takeDown(servers, devbox, until: Self.unreachable)
        let host = NSHostingView(rootView: ScratchpadPane(state: store).environment(\.mica, MicaTheme(dark: true)))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 500, height: 300))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        try await eventually { InlineAnnotationEditorTests.controller(in: host) != nil }
        let controller = try #require(InlineAnnotationEditorTests.controller(in: host))
        try await eventually { !controller.textView.isEditable }
    }

    @Test func everyStateThatStopsAServerKeepsTheSessionOnScreen() async throws {
        let devbox = DevboxHandshake()
        let (servers, model, store) = try await openSession(devbox)
        defer { model.closeAll(); for connection in servers.connections { connection.stop() } }
        let tab = model.tabs[0]
        let change = SSHHostKeyChange(host: "devbox.local", saved: "SHA256:old", now: "SHA256:new")
        let mismatch = ClientError.incompatibleDaemon(daemonVersion: "0.40.0", appProtocol: kitWireVersion,
                                                      daemonProtocol: kitWireVersion - 1, reason: .daemonProtocolOlder)
        let stops: [(any Error, String)] = [
            (ClientError.sshHostKeyChanged(change), "Host key changed"),
            (ClientError.sshServerStopped, "Server stopped"),
            (ClientError.sshSignInRequired, "Sign in"),
            (ClientError.needsPairing(.refused), "Pair again"),
            (mismatch, "Server needs a newer Kit"),
        ]
        for (error, issue) in stops {
            try await takeDown(servers, devbox, with: error) { $0.sessionIssue == issue }
            #expect(tab.showsSession, "\(issue)")
            #expect(store.offline, "\(issue)")
            try await bringBack(servers, devbox)
            #expect(!store.offline, "\(issue)")
        }
    }

    // MARK: What the tab shows

    private func host(_ model: SessionWindowModel) -> (NSWindow, NSHostingView<AnyView>) {
        let window = FocusIsolatedWindow(testContentRect: NSRect(x: 0, y: 0, width: 1200, height: 800))
        let view = SessionTabContent(model: model, tab: model.tabs[0], placement: .side)
            .environment(\.mica, MicaTheme(dark: true))
            .frame(width: 1200, height: 800)
        let host = NSHostingView(rootView: AnyView(view))
        window.contentView = host
        window.orderFront(nil)
        return (window, host)
    }

    private func views<View: NSView>(_ type: View.Type, in view: NSView) -> [View] {
        (view as? View).map { [$0] } ?? view.subviews.flatMap { views(type, in: $0) }
    }

    private func editors(in view: NSView) -> [ComposerTextView] {
        if let editor = view as? ComposerTextView { return [editor] }
        return view.subviews.flatMap { editors(in: $0) }
    }

    @Test func theTabShowsTheServersStateAboveTheSessionAndLocksTheComposer() async throws {
        let devbox = DevboxHandshake()
        let (servers, model, store) = try await openSession(devbox)
        defer { model.closeAll(); for connection in servers.connections { connection.stop() } }
        let tab = model.tabs[0]
        #expect(SessionTabContent.shown(tab) == .session(serverState: false))
        let (window, host) = host(model)
        defer { window.close() }
        func settle() async throws { for _ in 0..<5 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) } }
        try await eventually { host.layoutSubtreeIfNeeded(); return !editors(in: host).isEmpty }
        let transcript = try #require(views(NSTableView.self, in: host).first)
        let scratchpad = try #require(views(TextView.self, in: host).first)
        #expect(editors(in: host).map(\.isEditable) == [true])
        #expect(scratchpad.isEditable)

        try await takeDown(servers, devbox, until: Self.unreachable)
        #expect(SessionTabContent.shown(tab) == .session(serverState: true))
        try await eventually { host.layoutSubtreeIfNeeded(); return editors(in: host).map(\.isEditable) == [false] }
        try await settle()
        // The same transcript and scratchpad stay on screen, with the draft;
        // nothing in them can be changed.
        #expect(views(NSTableView.self, in: host).first === transcript)
        #expect(transcript.numberOfRows == store.messages.count)
        #expect(views(TextView.self, in: host).first === scratchpad)
        #expect(!scratchpad.isEditable)
        #expect(editors(in: host).map(\.string) == ["Also check the zoomed tile"])

        try await bringBack(servers, devbox)
        #expect(SessionTabContent.shown(tab) == .session(serverState: false))
        try await eventually { host.layoutSubtreeIfNeeded(); return editors(in: host).map(\.isEditable) == [true] }
        #expect(views(NSTableView.self, in: host).first === transcript)
        #expect(editors(in: host).map(\.string) == ["Also check the zoomed tile"])
        try await eventually { host.layoutSubtreeIfNeeded(); return scratchpad.isEditable }
    }

    @Test func aTabWithNoSessionToKeepShowsTheServersStateInItsPlace() async throws {
        let devbox = DevboxHandshake()
        await devbox.fail(with: ClientError.noDaemon)
        let servers = servers(devbox)
        servers.start()
        let model = SessionWindowModel(id: UUID(), servers: servers, sessions: [SessionIdentity(server: "devbox", session: "a")])
        defer { model.closeAll(); for connection in servers.connections { connection.stop() } }
        try await eventually { Self.unreachable(servers.connection(named: "devbox")?.status ?? .connecting) }
        // A session that never opened, and a launcher, have nothing to keep.
        #expect(!model.tabs[0].showsSession)
        #expect(SessionTabContent.shown(model.tabs[0]) == .serverState)
        model.openLauncher()
        let launcher = try #require(model.tabs.first { $0.isLauncher })
        model.point(launcher, at: "devbox")
        #expect(SessionTabContent.shown(launcher) == .serverState)
    }

    @Test func aChangedHostKeyKeepsTheSessionBehindItsBanner() async throws {
        let devbox = DevboxHandshake()
        let (servers, model, _) = try await openSession(devbox)
        defer { model.closeAll(); for connection in servers.connections { connection.stop() } }
        let change = SSHHostKeyChange(host: "devbox.local", saved: "SHA256:old", now: "SHA256:new")
        try await takeDown(servers, devbox, with: ClientError.sshHostKeyChanged(change)) { $0.sessionIssue == "Host key changed" }
        #expect(SessionTabContent.shown(model.tabs[0]) == .session(serverState: true))
    }
}

/// A file's comments while its session's server can't take them: saved
/// ones stay, and nothing new can begin.
@MainActor struct ReadOnlyCommentsTests {
    private struct Harness: View {
        let file: WorkspaceFile
        let annotations: AnnotationState
        let readOnly: Bool
        @State private var position = SourceEditorState()
        var body: some View {
            let _ = annotations.records
            AnnotatedFileEditor(file: file, position: $position, annotations: annotations,
                client: InlineAnnotationClient(), session: "session_test")
                .environment(\.sessionReadOnly, readOnly)
        }
    }

    private func gutter(in view: NSView) -> AnnotationGutterOverlay? {
        (view as? AnnotationGutterOverlay) ?? view.subviews.lazy.compactMap { gutter(in: $0) }.first
    }

    @Test func aReadOnlyFileOffersNoNewComments() async throws {
        let file = try InlineAnnotationEditorTests.file("func send() {}\nfunc cancel() {}\n")
        let note = try FileAnnotation(id: 1, anchor: .init(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: file.path, fileRevision: file.revision, startLine: 1, endLine: 1), workingTreeDiff: nil),
            body: "Kept while offline.", source: "func send() {}", stale: false, complete: true)
        let annotations = AnnotationState(); annotations.observe([note])
        for readOnly in [false, true] {
            let host = NSHostingView(rootView: Harness(file: file, annotations: annotations, readOnly: readOnly)
                .environment(\.mica, MicaTheme(dark: true)))
            let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 700, height: 300))
            window.contentView = host
            window.orderFront(nil)
            defer { window.close() }
            try await eventually {
                guard let controller = InlineAnnotationEditorTests.controller(in: host) else { return false }
                return !controller.textView.layoutManager.blockHeights.isEmpty
            }
            let controller = try #require(InlineAnnotationEditorTests.controller(in: host))
            // The saved comment stays under its line either way.
            #expect(controller.textView.layoutManager.blockHeights.keys.sorted() == [0])
            #expect(try #require(gutter(in: controller.view)).enabled == !readOnly)
            let begin = SelectionLineComments.handler(for: controller.textView)?(NSRange(location: 0, length: 4))
            #expect((begin != nil) == !readOnly)
        }
    }
}

/// What the banner over a kept session says for each server state.
@MainActor struct ServerStateBannerTests {
    private let home = URL(fileURLWithPath: "/tmp/devbox")

    @Test func eachStateSaysWhatHappenedInOneLine() {
        func summary(_ status: ServerStatus, name: String = "devbox", connector: ServerConnector? = nil) -> String {
            ServerStatePresentation(name: name, connector: connector ?? .home(home), status: status, sessionTab: true,
                                    appVersion: "Kit for Mac 1.0").summary
        }
        #expect(summary(.unreachable(reason: "ssh: connect to host devbox port 22: Operation timed out", retry: nil))
            == "ssh: connect to host devbox port 22: Operation timed out")
        #expect(summary(.serverStopped) == "Kit reached devbox over SSH, but its Kit server isn’t running.")
        #expect(summary(.signInRequired) == "devbox asked for something only you can answer, such as a password.")
        let change = SSHHostKeyChange(host: "devbox.local", saved: "SHA256:old", now: "SHA256:new")
        #expect(summary(.hostKeyChanged(change))
            == "A reinstalled host does this, but so does someone intercepting the connection, so Kit stopped connecting.")
        let older = ServerMismatch(serverKitVersion: "0.61.0", serverProtocol: 14, appProtocol: 15, reason: .daemonProtocolOlder)
        #expect(summary(.incompatible(older)) == "devbox runs Kit 0.61.0, protocol 14; this app speaks protocol 15.")
        let newer = ServerMismatch(serverKitVersion: "0.68.0", serverProtocol: 16, appProtocol: 15, reason: .clientProtocolOlder)
        #expect(summary(.incompatible(newer))
            == "devbox runs Kit 0.68.0, protocol 16; this app speaks protocol 15. devbox and its work keep running.")
        #expect(summary(.incompatible(older), name: "local", connector: .home(DevHomeResolver.defaultHome))
            == "The server runs Kit 0.61.0, protocol 14; this app speaks protocol 15.")
        #expect(ServerStateBanner.help(message: "No running Kit server found.", reason: "No running Kit server found.")
            == "No running Kit server found.")
        #expect(ServerStateBanner.help(message: "Kit can’t reach devbox.", reason: "ssh: connect to host devbox port 22")
            == "Kit can’t reach devbox.\n\nssh: connect to host devbox port 22")
        #expect(ServerStateBanner.pairingSummary(name: "devbox")
            == "Get a link with kit server pair on devbox, or Pair a Device on another paired device.")
    }
}

/// The scratchpad while its session's server can't take changes: what it
/// says stays, and typing waits for the server.
@MainActor struct ReadOnlyScratchpadTests {
    @Observable final class Model { var editable = true; var text = "offline" }
    private struct Harness: View {
        let model: Model
        @State private var position = SourceEditorState()
        var body: some View {
            @Bindable var model = model
            ScratchpadEditor(text: $model.text, editorState: $position, editable: model.editable)
        }
    }

    @Test func theScratchpadStopsTakingTypingWhenItsServerGoesAndTakesItAgainWhenItIsBack() async throws {
        let model = Model()
        let host = NSHostingView(rootView: Harness(model: model).environment(\.mica, MicaTheme(dark: true)))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 500, height: 300))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        try await eventually { InlineAnnotationEditorTests.controller(in: host) != nil }
        let controller = try #require(InlineAnnotationEditorTests.controller(in: host))
        #expect(controller.textView.isEditable)
        model.editable = false
        try await eventually { !controller.textView.isEditable }
        controller.textView.insertText("x", replacementRange: NSRange(location: 7, length: 0))
        #expect(controller.textView.string == "offline")
        // Nor can an accessibility client change it.
        #expect(!controller.textView.isAccessibilitySelectorAllowed(#selector(NSView.setAccessibilityValue(_:))))
        controller.textView.setAccessibilityValue("offlinex")
        #expect(controller.textView.string == "offline")
        model.editable = true
        try await eventually { controller.textView.isEditable }
    }
}

/// A stale comment's sheet while its session's server can't take changes:
/// it shows what was loaded and offers only Close.
@MainActor struct ReadOnlyStaleCommentTests {
    @Test func aStaleCommentOffersNoChangesWhileItsServerIsDown() {
        #expect(StaleAnnotationView.actions(stale: true, readOnly: false) == [.delete, .selectNewRange])
        #expect(StaleAnnotationView.actions(stale: false, readOnly: false) == [.delete])
        #expect(StaleAnnotationView.actions(stale: true, readOnly: true) == [])
    }
}
