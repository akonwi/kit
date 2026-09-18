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

@MainActor struct PromptCommandTests {
    @Test func completionInsertsBeforeSubmittingAndPreservesQuotedArguments() throws {
        let state = ComposerCommandState()
        state.catalog = [.init(name: "review", description: "Review changes", source: "project", location: "/tmp/review.md")]
        state.observe(text: "/rev", caret: NSRange(location: 4, length: 0), pasted: false)
        #expect(state.selected?.name == "review")
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        editor.commands = state; editor.string = "/rev"
        editor.setSelectedRange(NSRange(location: 4, length: 0))
        var submissions = 0
        editor.submit = { submissions += 1 }
        let enter = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: 0, context: nil, characters: "\r", charactersIgnoringModifiers: "\r", isARepeat: false, keyCode: 36))
        editor.keyDown(with: enter)
        #expect(editor.string == "/review ")
        #expect(editor.selectedRange().location == 8)
        #expect(submissions == 0)
        editor.keyDown(with: enter)
        #expect(submissions == 1)
        let invocation = try #require(PromptCommand.invocation("/review \"auth module\" carefully\nnext line"))
        #expect(invocation.name == "review")
        #expect(invocation.args == "\"auth module\" carefully\nnext line")
        state.observe(text: "/review args", caret: NSRange(location: 12, length: 0), pasted: false)
        #expect(state.isOpen == false)
    }

    @Test func discoveryRejectsAmbiguousNamesAndCompletionRespectsCaretAndPaste() throws {
        let wire = WirePromptCommand(argumentHint: nil, name: "review", description: "Review changes", source: "project", location: "/tmp/review.md")
        #expect(try PromptCommand.project([wire]).map(\.name) == ["review"])
        #expect(throws: ClientError.self) { try PromptCommand.project([wire, wire]) }
        let state = ComposerCommandState()
        state.catalog = try PromptCommand.project([wire])
        state.observe(text: "/", caret: NSRange(location: 1, length: 0), pasted: false)
        #expect(state.visibleMatches.map(\.name) == ["review"])
        state.observe(text: "/review", caret: NSRange(location: 7, length: 0), pasted: true)
        #expect(!state.isOpen)
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
