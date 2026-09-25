import AppKit
import SwiftUI
import Foundation
import Testing
@testable import Kit

struct SubagentTests {
    private func snapshot(definitions: [[String: Any]] = [], conversations: [[String: Any]] = []) throws -> WireSessionSnapshot {
        let base = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0}}"#
        var json = try JSONSerialization.jsonObject(with: Data(base.utf8)) as! [String: Any]
        json["subagentDefinitions"] = definitions
        json["subagentConversations"] = conversations
        json["subagentDiagnostics"] = [["severity": "warning", "code": "test", "message": "Definition unavailable", "source": ["kind": "file", "path": "/tmp/agent"]]]
        return try JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json))
    }
    private func definition(_ name: String) -> [String: Any] {
        ["name": name, "description": "Review code", "model": "configured", "source": ["kind": "file", "path": "/tmp/agent"]]
    }
    private func conversation(_ name: String, id: String = "c", status: String = "idle") -> [String: Any] {
        ["id": id, "agentName": name, "model": "actual", "thinkingLevel": "medium", "state": status, "generation": 1, "queuedTasks": 0, "updatedAt": "now"]
    }

    @Test func rosterIncludesInactiveAndRetainedAgentsAndUsesActualModel() throws {
        let roster = try SubagentRoster(snapshot(definitions: [definition("unused"), definition("reviewer")],
            conversations: [conversation("reviewer", status: "running"), conversation("removed", id: "retained")]))
        #expect(roster.items.map(\.name) == ["reviewer", "removed", "unused"])
        #expect(roster.items.map(\.status) == ["running", "idle", "inactive"])
        #expect(roster.items.map(\.model) == ["actual", "actual", "configured"])
        #expect(roster.items.map(\.thinkingLevel) == ["medium", "medium", nil])
        #expect(roster.items.map(\.configurationLabel) == ["actual · Medium thinking", "actual · Medium thinking", "configured"])
        #expect(roster.items.map(\.conversationID) == ["c", "retained", nil])
        #expect(roster.items[1].description == "Previously active subagent conversation")
        #expect(roster.diagnostics == ["Definition unavailable"])
    }

    @Test func configurationUsesConversationThinkingIncludingOff() throws {
        for level in ["off", "minimal", "low", "medium", "high", "xhigh", "max"] {
            var child = conversation("reviewer")
            child["thinkingLevel"] = level
            let roster = try SubagentRoster(snapshot(conversations: [child]))
            let label = level == "off" ? "Thinking off" : level.capitalized + " thinking"
            #expect(roster.items.first?.configurationLabel == "actual · " + label)
        }
    }

    @Test func rosterRejectsAmbiguousIdentity() throws {
        let value = try snapshot(conversations: [conversation("a"), conversation("b")])
        #expect(throws: ClientError.self) { try SubagentRoster(value) }
    }

    @Test func rosterRefreshPreservesActiveMainTranscript() throws {
        var projection = try SessionEventProjection(snapshot())
        let event = try JSONDecoder().decode(WireSessionEvent.self, from: Data(#"{"streamId":"stream","sequence":1,"sessionId":"s","turnId":"t","runId":"t","kind":"tool.started","toolCallId":"tool","toolName":"bash"}"#.utf8))
        try projection.apply(event)
        let messages = projection.session.messages
        try projection.updateSubagents(snapshot(conversations: [conversation("reviewer", status: "running")]))
        #expect(projection.session.messages == messages)
        #expect(projection.session.subagents?.items.first?.status == "running")
    }

    @MainActor @Test func conversationLoadsEmptyFailureAndRetry() async {
        let state = SubagentConversationState()
        await state.load(client: Stub(result: .success([])), session: "s", conversation: "c")
        #expect(state.loaded)
        #expect(state.messages == [])
        await state.load(client: Stub(result: .failure(.http(404))), session: "s", conversation: "c")
        #expect(state.unavailable)
        #expect(state.error == "This subagent conversation is no longer available.")
        let message = TranscriptMessage(id: "m", role: "assistant", text: "Reviewed", tools: [])
        await state.load(client: Stub(result: .success([message])), session: "s", conversation: "c")
        #expect(state.messages == [message])
        #expect(state.error == nil)
        #expect(state.loading == false)
    }

    @MainActor @Test func cancellationDoesNotPublishLateResult() async throws {
        let state = SubagentConversationState()
        let client = DeferredClient()
        let task = Task { await state.load(client: client, session: "s", conversation: "c") }
        while !(await client.waiting) { await Task.yield() }
        #expect(state.loading)
        task.cancel()
        await client.finish()
        await task.value
        #expect(state.loaded == false)
        #expect(state.loading == false)
        #expect(state.error == nil)
    }

    private actor DeferredClient: SubagentClient {
        nonisolated let serverID = "deferred"
        nonisolated let isDemo = false
        private var continuation: CheckedContinuation<[TranscriptMessage], Never>?
        var waiting: Bool { continuation != nil }
        func finish() { continuation?.resume(returning: []); continuation = nil }
        func sessions() async throws -> [SessionExcerpt] { [] }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
        func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] {
            await withCheckedContinuation { continuation = $0 }
        }
    }

    @MainActor @Test func renderRetainedConversationWithSharedTranscript() async throws {
        let excerpt = try SessionProjection.snapshot(snapshot(definitions: [definition("unused"), definition("reviewer")],
            conversations: [conversation("reviewer", status: "idle"), conversation("removed", id: "retained")]))
        let messages = [TranscriptMessage(id: "user", role: "user", text: "Review the session lifecycle.", tools: []),
            TranscriptMessage(id: "tools", role: "tools", text: "", tools: [ToolActivity(id: "read", name: "read", summary: "Read file", output: "Session owns its requests", arguments: "{\"path\":\"SessionReplica.swift\"}", failed: false)]),
            TranscriptMessage(id: "reply", role: "assistant", text: "The requests are cancelled when the session detaches.\n\n```swift\nfunc detach() { task?.cancel() }\n```", tools: [])]
        let store = SessionStore(fixture: Fixture(sessions: [excerpt]), client: Stub(result: .success(messages)))
        store.ui.workspace.open(.agent("reviewer"))
        for dark in [false, true] {
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let view = HStack(spacing: 0) {
                SubagentsPopover(state: store).frame(width: 360)
                Rectangle().fill(theme.border).frame(width: 1)
                AgentPane(state: store, name: "reviewer").frame(width: 630)
            }.environment(\.mica, theme).environment(\.colorScheme, dark ? .dark : .light)
                .foregroundStyle(theme.text).background(theme.surface).frame(width: 991, height: 600)
            let host = NSHostingView(rootView: view)
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 991, height: 600),
                                  styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentView = host
            window.orderFront(nil)
            defer { window.close() }
            try await Task.sleep(for: .milliseconds(250))
            for _ in 0..<8 {
                host.layoutSubtreeIfNeeded()
                try await Task.sleep(for: .milliseconds(50))
            }
            func tables(_ view: NSView) -> [NSTableView] {
                (view as? NSTableView).map { [$0] } ?? view.subviews.flatMap { tables($0) }
            }
            let table = try #require(tables(host).first)
            #expect(table.numberOfRows == 3)
            #expect(table.rect(ofRow: 2).height > 100)
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-subagents-\(dark ? "dark" : "light").png"))
        }
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_SUBAGENT_LIVE_TEST"] == "1"))
    func retainedConversationFromLocalServer() async throws {
        let client = try await HTTPClient.local()
        let catalog = try await client.sessions()
        for session in catalog.prefix(30) {
            let snapshot = try await client.snapshot(session.id)
            guard let child = snapshot.subagents?.items.first(where: { $0.conversationID != nil }),
                  let id = child.conversationID else { continue }
            let messages = try await client.subagentTranscript(session: session.id, conversation: id)
            #expect(Set(messages.map(\.id)).count == messages.count)
            actor Probe {
                var count = 0
                func receive(_ update: SubagentTranscriptUpdate) { count += 1 }
            }
            let probe = Probe()
            let watch = Task {
                try await client.watchSubagent(session: session.id, conversation: id) { await probe.receive($0) }
            }
            for _ in 0..<150 {
                if await probe.count > 0 { break }
                try await Task.sleep(for: .milliseconds(20))
            }
            watch.cancel()
            let result = await watch.result
            if await probe.count == 0 {
                if case .failure(let error) = result { Issue.record("Child watch failed: \(error)") }
                else { Issue.record("Child watch did not publish a snapshot") }
            }
            #expect(await probe.count > 0)
            print("Verified retained subagent transcript and watch: \(messages.count) rows")
            return
        }
        Issue.record("No retained conversation found in the local server catalog")
    }

    @MainActor @Test func activePaneStreamsAndHiddenPaneStopsWatching() async throws {
        let excerpt = try SessionProjection.snapshot(snapshot(definitions: [definition("reviewer")],
            conversations: [conversation("reviewer", status: "running")]))
        let client = StreamingPreview()
        let store = SessionStore(fixture: Fixture(sessions: [excerpt]), client: client)
        store.ui.workspace.open(.agent("reviewer"))
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let view = AgentPane(state: store, name: "reviewer")
            .environment(\.mica, theme).environment(\.colorScheme, .dark)
            .foregroundStyle(theme.text).background(theme.surface).frame(width: 630, height: 500)
        let host = NSHostingView(rootView: view)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 630, height: 500),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<20 {
            host.layoutSubtreeIfNeeded()
            try await Task.sleep(for: .milliseconds(30))
        }
        #expect(await client.active == 1)
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        try #require(bitmap.representation(using: .png, properties: [:]))
            .write(to: URL(fileURLWithPath: "/tmp/kit-subagent-streaming.png"))
        store.ui.workspace.select(.conversation)
        for _ in 0..<50 {
            if await client.active == 0 { break }
            try await Task.sleep(for: .milliseconds(20))
        }
        #expect(await client.active == 0)
        store.ui.workspace.select(.agent("reviewer"))
        for _ in 0..<50 {
            if await client.calls == 2 { break }
            try await Task.sleep(for: .milliseconds(20))
        }
        #expect(await client.calls == 2)
        #expect(await client.active == 1)
        store.ui.workspace.select(.conversation)
    }

    private actor StreamingPreview: SubagentStreamingClient {
        nonisolated let serverID = "preview"
        nonisolated let isDemo = false
        var active = 0
        var calls = 0
        func sessions() async throws -> [SessionExcerpt] { [] }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
        func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] { [] }
        func watchSubagent(session: String, conversation: String,
                           receive: @escaping @Sendable (SubagentTranscriptUpdate) async -> Void) async throws {
            active += 1; calls += 1
            defer { active -= 1 }
            let tools = [ToolActivity(id: "read", name: "read", summary: "Read file", output: "", failed: false,
                                      status: "Running…", thinking: "Checking request ownership and cancellation.")]
            await receive(SubagentTranscriptUpdate(messages: [
                TranscriptMessage(id: "user", role: "user", text: "Review the session lifecycle.", tools: []),
                TranscriptMessage(id: "tools", role: "tools", text: "", tools: tools)],
                activity: "Checking request ownership and cancellation."))
            try await Task.sleep(for: .seconds(30))
        }
    }

    private struct Stub: SubagentClient {
        let serverID = "test"
        let isDemo = false
        let result: Result<[TranscriptMessage], ClientError>
        func sessions() async throws -> [SessionExcerpt] { [] }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
        func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] { try result.get() }
    }
}
