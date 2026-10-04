import AppKit
import Foundation
import SwiftUI
import Testing
@testable import Kit

private let conversationID = "subagent_" + String(repeating: "a", count: 32)

private struct ConfigurationCall: Equatable, Sendable {
    let session: String
    let conversation: String
    let generation: UInt64
    let model: String?
    let thinking: String?
}

private actor ConfigurationStub: SubagentConfigurationClient {
    nonisolated let serverID = "configure-test"
    nonisolated let isDemo = false
    let failure: (any Error & Sendable)?
    let warnings: [String]
    var current: SessionExcerpt
    let applied: SessionExcerpt?
    var calls: [ConfigurationCall] = []
    init(current: SessionExcerpt, applied: SessionExcerpt? = nil, failure: (any Error & Sendable)? = nil, warnings: [String] = []) {
        self.current = current; self.applied = applied; self.failure = failure; self.warnings = warnings
    }
    func sessions() async throws -> [SessionExcerpt] { [current] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { current }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws { await receive(current) }
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] { [] }
    func configureSubagent(_ session: String, conversation: String, generation: UInt64,
                           model: String?, thinking: String?) async throws -> SubagentConfigurationResult {
        calls.append(.init(session: session, conversation: conversation, generation: generation, model: model, thinking: thinking))
        if let failure { throw failure }
        if let applied { current = applied }
        let item = current.subagents?.items.first { $0.conversationID == conversation }
        return .init(model: item?.model ?? model ?? "", thinkingLevel: item?.thinkingLevel ?? thinking ?? "", warnings: warnings)
    }
}

@MainActor private func session(model: String = "openai/gpt-5", thinking: String = "medium", status: String = "idle",
                                queued: Int = 0, active: String? = nil) throws -> SessionExcerpt {
    var result = SessionExcerpt(id: "s", title: "Test", sourceTitle: "test", model: "anthropic/claude", thinking: "high",
                                workspace: "tmp", date: "", messages: [])
    var item: [String: Any] = ["name": "reviewer", "description": "Review", "model": model, "thinkingLevel": thinking,
        "status": status, "conversationID": conversationID, "generation": 3, "queuedTasks": queued]
    if let active { item["activeTaskID"] = active }
    result.subagents = try JSONDecoder().decode(SubagentRoster.self,
        from: JSONSerialization.data(withJSONObject: ["items": [item], "diagnostics": []]))
    result.followUps = try FollowUpState(.init(count: 0, previews: nil, annotationIds: nil))
    return result
}

@MainActor private func connectedStore(_ client: ConfigurationStub, session excerpt: SessionExcerpt) async throws -> SessionStore {
    let store = SessionStore(fixture: .init(sessions: [excerpt]), client: client)
    store.attach()
    for _ in 0..<200 where store.connectionState != .connected { try await Task.sleep(for: .milliseconds(10)) }
    try #require(store.connectionState == .connected)
    return store
}

@MainActor struct SubagentConfigurationTests {
    @Test func acceptedChangeRefreshesAndKeepsWarningsLocal() async throws {
        let client = ConfigurationStub(current: try session(), warnings: ["Thinking level \"max\" is unavailable; using \"high\"."])
        let operation = SubagentConfigurationOperation()
        var refreshes = 0
        let applied = await operation.change(session: "s", conversation: conversationID, generation: 3, agent: "reviewer",
            model: "openai/gpt-5-mini", thinking: nil, client: client, outstandingWork: { false }) { refreshes += 1 }
        #expect(applied)
        #expect(refreshes == 1)
        #expect(operation.pending == false)
        #expect(operation.error == nil)
        #expect(operation.warning == "Thinking level \"max\" is unavailable; using \"high\".")
        #expect(await client.calls == [.init(session: "s", conversation: conversationID, generation: 3, model: "openai/gpt-5-mini", thinking: nil)])
        operation.clearFeedback()
        #expect(operation.warning == nil)
    }

    @Test func conflictExplainsBusyModelChangeAfterRefresh() async throws {
        let client = ConfigurationStub(current: try session(), failure: ClientError.http(409))
        let operation = SubagentConfigurationOperation()
        var refreshed = false
        let applied = await operation.change(session: "s", conversation: conversationID, generation: 3, agent: "reviewer",
            model: "openai/gpt-5-mini", thinking: nil, client: client, outstandingWork: { refreshed }) { refreshed = true }
        #expect(applied == false)
        #expect(operation.error == "reviewer has active or queued work. Change its model when that work finishes.")
    }

    @Test func conflictOnThinkingReportsChangedConversation() async throws {
        let client = ConfigurationStub(current: try session(), failure: ClientError.http(409))
        let operation = SubagentConfigurationOperation()
        await operation.change(session: "s", conversation: conversationID, generation: 3, agent: "reviewer",
            model: nil, thinking: "high", client: client, outstandingWork: { true }) {}
        #expect(operation.error == "This conversation changed. Review its current configuration and try again.")
    }

    @Test func uncertainFailureRefreshesOnceWithoutReplay() async throws {
        let client = ConfigurationStub(current: try session(), failure: ClientError.disconnected)
        let operation = SubagentConfigurationOperation()
        var refreshes = 0
        let applied = await operation.change(session: "s", conversation: conversationID, generation: 3, agent: "reviewer",
            model: nil, thinking: "low", client: client, outstandingWork: { false }) { refreshes += 1 }
        #expect(applied == false)
        #expect(refreshes == 1)
        #expect(operation.error == ClientError.disconnected.localizedDescription)
        #expect(await client.calls.count == 1)
    }

    @Test func declaredRejectionMessageIsShownLocally() async throws {
        let client = ConfigurationStub(current: try session(), failure: SubagentConfigurationRejected(message: "requested model/thinking combination is unsupported"))
        let operation = SubagentConfigurationOperation()
        await operation.change(session: "s", conversation: conversationID, generation: 3, agent: "reviewer",
            model: nil, thinking: "max", client: client, outstandingWork: { false }) {}
        #expect(operation.error == "requested model/thinking combination is unsupported")
    }

    @Test func storeSendsThinkingWhileRunningButNeverModel() async throws {
        let running = try session(status: "running", active: "task_1")
        let client = ConfigurationStub(current: running, applied: try session(thinking: "high", status: "running", active: "task_1"))
        let store = try await connectedStore(client, session: running)
        await store.configureSubagent("reviewer", model: "openai/gpt-5-mini")
        #expect(await client.calls.isEmpty)
        await store.configureSubagent("reviewer", thinking: "high")
        #expect(await client.calls == [.init(session: "s", conversation: conversationID, generation: 3, model: nil, thinking: "high")])
        #expect(store.selected?.subagents?.items.first?.thinkingLevel == "high")
        // The attached session's configuration is independent of the child.
        #expect(store.selected?.model == "anthropic/claude")
        #expect(store.selected?.thinking == "high")
    }

    @Test func storeSkipsQueuedModelChangeAndUnchangedValues() async throws {
        let queued = try session(queued: 1)
        let client = ConfigurationStub(current: queued)
        let store = try await connectedStore(client, session: queued)
        await store.configureSubagent("reviewer", model: "openai/gpt-5-mini")
        await store.configureSubagent("reviewer", thinking: "medium")
        #expect(await client.calls.isEmpty)
    }

    @Test func storeSendsOnlyTheChangedModelAndRefreshesFromServer() async throws {
        let idle = try session()
        let client = ConfigurationStub(current: idle, applied: try session(model: "openai/gpt-5-mini", thinking: "low"))
        let store = try await connectedStore(client, session: idle)
        await store.configureSubagent("reviewer", model: "openai/gpt-5-mini")
        #expect(await client.calls == [.init(session: "s", conversation: conversationID, generation: 3, model: "openai/gpt-5-mini", thinking: nil)])
        let item = try #require(store.selected?.subagents?.items.first)
        #expect(item.model == "openai/gpt-5-mini")
        #expect(item.thinkingLevel == "low")
        #expect(store.subagentConfiguration(conversationID).pending == false)
    }

    @Test func paneControlsDisableOnlyModelForOutstandingWork() async throws {
        let running = try session(status: "running", active: "task_1")
        let store = try await connectedStore(ConfigurationStub(current: running), session: running)
        var agent = try #require(store.selected?.subagents?.items.first)
        let operation = store.subagentConfiguration(conversationID)
        var controls = SubagentConfigurationControls(state: store, agent: agent, operation: operation)
        #expect(controls.modelEnabled == false)
        #expect(controls.thinkingEnabled)
        #expect(SubagentConfigurationControls.modelHelp(agent: agent, models: [])
            == "reviewer's model can change once its active and queued work finishes")
        agent = try #require(try session().subagents?.items.first)
        controls = SubagentConfigurationControls(state: store, agent: agent, operation: operation)
        #expect(controls.modelEnabled)
        #expect(SubagentConfigurationControls.thinkingLabel("off") == "Thinking off")
        #expect(SubagentConfigurationControls.thinkingLabel("xhigh") == "Xhigh thinking")
    }

    @Test func modelPickerTitleIdentifiesTarget() throws {
        let data = "[" + (0..<100).map { index in
            "{\"id\":\"provider/model\(index)\",\"name\":\"Model \(index)\",\"provider\":\"provider\",\"api\":\"test\",\"contextWindow\":10000,\"available\":true}"
        }.joined(separator: ",") + "]"
        let models = try JSONDecoder().decode([WireModelCapability].self, from: Data(data.utf8))
        let theme = ThemeConfiguration.decode("").theme(dark: false)
        let host = NSHostingView(rootView: ComposerModelPicker(models: models, selectedID: "provider/model50",
            select: { _ in }, reload: {}, dismiss: {}, title: "Model for reviewer").environment(\.mica, theme))
        #expect(host.fittingSize.width == ComposerModelPicker.width)
        #expect(host.fittingSize.height == ComposerModelPicker.maxHeight + ComposerModelPicker.titleHeight + 1)
    }

    @Test func renderPaneConfigurationControls() async throws {
        let running = try session(status: "running", active: "task_1")
        let client = ConfigurationStub(current: running, failure: ClientError.http(409))
        let store = try await connectedStore(client, session: running)
        store.ui.workspace.open(.agent("reviewer"))
        await store.configureSubagent("reviewer", thinking: "high")
        for dark in [false, true] {
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let view = AgentPane(state: store, name: "reviewer")
                .environment(\.mica, theme).environment(\.colorScheme, dark ? .dark : .light)
                .foregroundStyle(theme.text).background(theme.surface).frame(width: 630, height: 240)
            let host = NSHostingView(rootView: view)
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 630, height: 240),
                                  styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentView = host
            window.orderFront(nil)
            defer { window.close() }
            for _ in 0..<8 {
                host.layoutSubtreeIfNeeded()
                try await Task.sleep(for: .milliseconds(30))
            }
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-subagent-configuration-\(dark ? "dark" : "light").png"))
        }
        #expect(store.subagentConfiguration(conversationID).error == "This conversation changed. Review its current configuration and try again.")
    }
}

private final class ConfigureResponse: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var bodies: [Data] = []
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        if let stream = request.httpBodyStream {
            stream.open()
            var data = Data(), buffer = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable { let count = stream.read(&buffer, maxLength: buffer.count); if count <= 0 { break }; data.append(buffer, count: count) }
            stream.close()
            Self.bodies.append(data)
        } else if let body = request.httpBody { Self.bodies.append(body) }
        #expect(request.httpMethod == "POST")
        #expect(url.path == "/v1/sessions/s/subagents/\(conversationID)/configure")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        let conversation = { (model: String, thinking: String, generation: Int) in
            "{\"id\":\"\(conversationID)\",\"agentName\":\"reviewer\",\"model\":\"\(model)\",\"thinkingLevel\":\"\(thinking)\",\"state\":\"running\",\"generation\":\(generation),\"queuedTasks\":0,\"updatedAt\":\"2026-01-01T00:00:00Z\"}"
        }
        let (status, body): (Int, String) = switch url.port {
        case 19301: (200, "{\"conversation\":\(conversation("openai/gpt-5", "high", 3))}")
        case 19302: (200, "{\"conversation\":\(conversation("openai/gpt-5", "low", 3))}")
        case 19303: (409, #"{"error":{"code":"conflict","message":"subagent conversation conflict"}}"#)
        case 19304: (400, #"{"error":{"code":"invalid_request","message":"requested model/thinking combination is unsupported"}}"#)
        case 19306: (200, "{\"conversation\":\(conversation("openai/gpt-5-mini", "low", 3)),\"warnings\":[\"unsafe\\u202Ewarning\"]}")
        default: (200, "{\"conversation\":\(conversation("openai/gpt-5-mini", "low", 3)),\"warnings\":[\"Using low thinking.\"]}")
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil,
            headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@Suite(.serialized) struct SubagentConfigurationTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ConfigureResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "test",
                              serverID: "test", configuration: configuration)
    }

    @Test func thinkingPatchOmitsModelAndReturnsAuthoritativeConfiguration() async throws {
        ConfigureResponse.bodies = []
        let result = try await client(19301).configureSubagent("s", conversation: conversationID, generation: 3, model: nil, thinking: "high")
        #expect(result == .init(model: "openai/gpt-5", thinkingLevel: "high", warnings: []))
        let body = try JSONSerialization.jsonObject(with: try #require(ConfigureResponse.bodies.last)) as? [String: Any]
        #expect(body?["generation"] as? Int == 3)
        #expect(body?["thinkingLevel"] as? String == "high")
        #expect(body?["model"] == nil)
    }

    @Test func modelPatchCarriesServerWarnings() async throws {
        let result = try await client(19305).configureSubagent("s", conversation: conversationID, generation: 3, model: "openai/gpt-5-mini", thinking: nil)
        #expect(result == .init(model: "openai/gpt-5-mini", thinkingLevel: "low", warnings: ["Using low thinking."]))
    }

    @Test func mismatchedResponseIsRejected() async throws {
        await #expect(throws: ClientError.self) {
            try await client(19302).configureSubagent("s", conversation: conversationID, generation: 3, model: nil, thinking: "high")
        }
    }

    @Test func rendererControlWarningIsRejected() async throws {
        await #expect(throws: ClientError.self) {
            try await client(19306).configureSubagent("s", conversation: conversationID, generation: 3, model: "openai/gpt-5-mini", thinking: nil)
        }
    }

    @Test func declaredFailuresKeepTheirMeaning() async throws {
        do {
            _ = try await client(19303).configureSubagent("s", conversation: conversationID, generation: 3, model: "openai/gpt-5-mini", thinking: nil)
            Issue.record("Expected conflict")
        } catch ClientError.http(409) {}
        do {
            _ = try await client(19304).configureSubagent("s", conversation: conversationID, generation: 3, model: nil, thinking: "max")
            Issue.record("Expected rejection")
        } catch let error as SubagentConfigurationRejected {
            #expect(error.message == "requested model/thinking combination is unsupported")
        }
    }

    @Test func invalidPatchIsNotSent() async throws {
        ConfigureResponse.bodies = []
        let transport = try client(19301)
        for (model, thinking, generation, conversation) in [(String?.none, String?.none, UInt64(3), conversationID),
            ("gpt-5", nil, 3, conversationID), (nil, "extreme", 3, conversationID),
            (nil, "high", 0, conversationID), (nil, "high", 3, "subagent_bad")] {
            await #expect(throws: MutationNotSent.self) {
                try await transport.configureSubagent("s", conversation: conversation, generation: generation, model: model, thinking: thinking)
            }
        }
        #expect(ConfigureResponse.bodies.isEmpty)
    }
}
