import AppKit
import Testing
@testable import Kit

private actor CommandClient: PromptCommandClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    let fails: Bool
    var requests: [WirePromptCommandInput] = []
    init(fails: Bool = false) { self.fails = fails }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.disconnected }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func runPromptCommand(_ id: String, input: WirePromptCommandInput) async throws -> WireRunReservation {
        requests.append(input)
        if fails { throw ClientError.disconnected }
        return .init(sessionId: id, turnId: "turn_a", runId: "turn_a")
    }
}

private actor PalettePromptClient: PromptCommandClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    let catalog: [SessionExcerpt]
    var requests: [WirePromptCommandInput] = []
    var paused = false
    var waiting: CheckedContinuation<Void, Never>?
    init(session: SessionExcerpt, other: [SessionExcerpt] = []) { catalog = [session] + other }
    func pause() { paused = true }
    func resume() { waiting?.resume(); waiting = nil; paused = false }
    func sessions() async throws -> [SessionExcerpt] { catalog }
    func snapshot(_ id: String) async throws -> SessionExcerpt { catalog.first { $0.id == id }! }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(try await snapshot(id))
        while !Task.isCancelled { try await Task.sleep(for: .seconds(1)) }
    }
    func runPromptCommand(_ id: String, input: WirePromptCommandInput) async throws -> WireRunReservation {
        requests.append(input)
        if paused { await withCheckedContinuation { waiting = $0 } }
        return .init(sessionId: id, turnId: "turn_a", runId: "turn_a")
    }
}

@MainActor struct PromptCommandTests {
    @Test func paletteRunsPromptWithArgumentsAndPreservesComposerDraft() async throws {
        let prompt = PromptCommand(name: "review", description: "Review changes", source: "project",
                                   location: "/repo/review.md", argumentHint: "<scope>")
        var session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "Test", model: "test/model", thinking: "off",
                                     workspace: "Test", date: "", messages: [])
        session.promptCommands = [prompt]
        let client = PalettePromptClient(session: session)
        let store = SessionStore(fixture: Fixture(sessions: [session]), client: client)
        store.attach()
        defer { store.detach() }
        for _ in 0..<100 where store.connectionState != .connected { try await Task.sleep(for: .milliseconds(10)) }
        #expect(store.palettePromptUnavailableReason == nil)
        store.ui.draft = "Existing draft"
        store.runPalettePrompt(prompt, args: "\"auth module\" carefully")
        for _ in 0..<100 where (await client.requests).isEmpty || store.operations.sending {
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(await client.requests.map { ($0.name, $0.args ?? "") }.first?.0 == "review")
        #expect(await client.requests.first?.args == "\"auth module\" carefully")
        #expect(store.ui.draft == "Existing draft")
        #expect(store.operations.acknowledgedDraft == nil)
    }

    @Test func paletteAcknowledgementKeepsItsOriginalSession() async throws {
        let prompt = PromptCommand(name: "review", description: "Review changes", source: "project", location: "/repo/review.md")
        var first = SessionExcerpt(id: "a", title: "First", sourceTitle: "Test", model: "test/model", thinking: "off",
                                   workspace: "Test", date: "", messages: [])
        first.promptCommands = [prompt]
        let second = SessionExcerpt(id: "b", title: "Second", sourceTitle: "Test", model: "test/model", thinking: "off",
                                    workspace: "Test", date: "", messages: [])
        let client = PalettePromptClient(session: first, other: [second])
        let store = SessionStore(fixture: Fixture(sessions: [first, second]), client: client)
        store.attach()
        defer { store.detach() }
        for _ in 0..<100 where store.connectionState != .connected { try await Task.sleep(for: .milliseconds(10)) }
        await client.pause()
        store.runPalettePrompt(prompt, args: "")
        let origin = store.operations
        for _ in 0..<100 where (await client.requests).isEmpty { try await Task.sleep(for: .milliseconds(10)) }
        store.select("b")
        let destination = store.operations
        await client.resume()
        for _ in 0..<100 where origin.sending { try await Task.sleep(for: .milliseconds(10)) }
        #expect(origin.acknowledgedDraft == nil)
        #expect(destination.acknowledgedDraft == nil)
        #expect(destination.submission == .idle)
    }

    @Test func directInvocationPreservesQuotedArguments() throws {
        let invocation = try #require(PromptCommand.invocation("/review \"auth module\" carefully\nnext line"))
        #expect(invocation.name == "review")
        #expect(invocation.args == "\"auth module\" carefully\nnext line")
    }

    @Test func discoveryRejectsAmbiguousNamesAndUnsafeHints() throws {
        let wire = WirePromptCommand(argumentHint: nil, name: "review", description: "Review changes", source: "project", location: "/tmp/review.md")
        #expect(try PromptCommand.project([wire]).map(\.name) == ["review"])
        let hinted = WirePromptCommand(argumentHint: "<scope>", name: "review", description: "Review changes",
                                       source: "project", location: "/tmp/review.md")
        #expect(try PromptCommand.project([hinted]).first?.argumentHint == "<scope>")
        for invalid in ["line\nbreak", "\u{1B}escape", "left\u{200D}right"] {
            let wire = WirePromptCommand(argumentHint: invalid, name: "review", description: "Review",
                                         source: "project", location: "/tmp/review.md")
            #expect(throws: ClientError.self) { try PromptCommand.project([wire]) }
        }
        #expect(throws: ClientError.self) { try PromptCommand.project([wire, wire]) }
    }

    @Test func commandAcknowledgementUsesSharedDraftAndUncertaintyRecovery() async throws {
        for fails in [false, true] {
            let client = CommandClient(fails: fails)
            let operation = SessionOperations()
            var acknowledged = false
            var recover = false
            operation.submitCommand(client: client, session: "s", input: .init(name: "review", args: "\"auth module\""),
                draft: .init(text: "/review \"auth module\"", notes: [:], attachmentIDs: []),
                uncertain: { recover = true }, acknowledged: { acknowledged = true })
            for _ in 0..<100 where operation.sending { try await Task.sleep(for: .milliseconds(5)) }
            #expect(await client.requests.count == 1)
            #expect(acknowledged == !fails)
            #expect(recover == fails)
            #expect(operation.uncertain == fails)
            if !fails { #expect(operation.acknowledgedDraft?.text == "/review \"auth module\"") }
        }
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_COMMAND_LIVE_TEST"] == "1"))
    func liveDiscoveryAndArgumentExpansion() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("kit-command-" + UUID().uuidString)
        let prompts = root.appendingPathComponent(".agents/prompts")
        try FileManager.default.createDirectory(at: prompts, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }
        try "---\ndescription: Verify command arguments\n---\nReply exactly: $1 $2. Do not call tools.\n".write(to: prompts.appendingPathComponent("verify-command.md"), atomically: true, encoding: .utf8)
        let client = try await HTTPClient.local()
        let id = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        do {
            _ = try await client.createSession(.init(id: id, cwd: root.path, name: "macOS command verification",
                model: "openai-codex/gpt-5.6-sol", thinkingLevel: "off", temporary: true))
            let initial = try await client.snapshot(id)
            #expect(initial.promptCommands?.first(where: { $0.name == "verify-command" })?.description == "Verify command arguments")
            let receipt = try await client.runPromptCommand(id, input: .init(name: "verify-command", args: "\"blue sky\" CHECK"))
            #expect(receipt.runId == receipt.turnId)
            var final = try await client.snapshot(id)
            let deadline = Date().addingTimeInterval(60)
            while final.activeRunID != nil && Date() < deadline {
                try await Task.sleep(for: .milliseconds(500))
                final = try await client.snapshot(id)
            }
            #expect(final.activeRunID == nil)
            #expect(final.messages.contains { $0.role == "user" && $0.text.contains("Reply exactly: blue sky CHECK.") })
            #expect(final.messages.contains { $0.role == "assistant" && $0.text.contains("blue sky CHECK") })
            try await client.disposeSession(id)
        } catch {
            try? await client.disposeSession(id)
            throw error
        }
    }
}
