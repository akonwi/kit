import AppKit
import Foundation
import SwiftUI
import Testing
@testable import Kit

/// A session whose subagents can be dismissed, recording each dismissal.
private actor PickerSessionStub: SubagentDismissalClient {
    nonisolated let serverID = "picker-test"
    nonisolated let isDemo = false
    let session: SessionExcerpt
    var dismissals: [String] = []
    init(session: SessionExcerpt) { self.session = session }
    func sessions() async throws -> [SessionExcerpt] { [session] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { session }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(session)
    }
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] { [] }
    func dismissSubagent(_ session: String, conversation: String, generation: UInt64) async throws {
        dismissals.append("\(session)/\(conversation)@\(generation)")
    }
}

@MainActor struct SubagentPickerTests {
    private func roster(definitions: [[String: Any]], conversations: [[String: Any]],
                        diagnostics: [String] = []) throws -> SubagentRoster {
        let base = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0}}"#
        var json = try JSONSerialization.jsonObject(with: Data(base.utf8)) as! [String: Any]
        json["subagentDefinitions"] = definitions
        json["subagentConversations"] = conversations
        json["subagentDiagnostics"] = diagnostics.map {
            ["severity": "warning", "code": "test", "message": $0, "source": ["kind": "file", "path": "/tmp/agent"]]
        }
        return try SubagentRoster(JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json)))
    }
    private func definition(_ name: String, _ description: String, model: String? = nil) -> [String: Any] {
        var value: [String: Any] = ["name": name, "description": description, "source": ["kind": "file", "path": "/tmp/" + name]]
        if let model { value["model"] = model }
        return value
    }
    private func conversation(_ name: String, _ state: String, model: String, thinking: String,
                              generation: UInt64 = 1) -> [String: Any] {
        ["id": "subagent_" + name, "agentName": name, "model": model, "thinkingLevel": thinking, "state": state,
         "generation": generation, "queuedTasks": 0, "updatedAt": "now"]
    }

    /// Every state the server reports, a retained conversation whose
    /// definition is gone, and an agent that hasn't started.
    private func team() throws -> SubagentRoster {
        try roster(definitions: [
            definition("designer", "UI/UX expert for front-end work"),
            definition("librarian", "Searches project docs"),
            definition("code-reviewer", "Reviews changes for regressions"),
            definition("ard-expert", "Ard language authority", model: "openai/gpt-5.3-codex"),
            definition("migrator", "Moves data between schemas"),
        ], conversations: [
            conversation("ard-expert", "idle", model: "openai/gpt-5.3-codex", thinking: "off"),
            conversation("code-reviewer", "running", model: "anthropic/claude-sonnet-4-5", thinking: "high", generation: 4),
            conversation("retired", "aborted", model: "openai-codex/gpt-5.6-sol", thinking: "medium"),
            conversation("migrator", "interrupted", model: "anthropic/claude-haiku-4-5", thinking: "low"),
            conversation("librarian", "failed", model: "anthropic/claude-haiku-4-5", thinking: "low"),
        ])
    }

    private func rows(_ roster: SubagentRoster) -> [PaletteCommand] {
        SubagentPicker.rows(roster, sessionModel: "anthropic/claude-opus-4-1", sessionThinking: "medium")
    }

    /// Each row as it is drawn: status, name, the description (after the
    /// reason when it can't be opened), and the configuration on the right.
    private func lines(_ rows: [PaletteCommand]) -> [String] {
        rows.map { row in
            let marker = switch row.status {
            case .working: "⠋"
            case .failed: "✗"
            case .stopped: "⊘"
            case .idle: "○"
            case nil: " "
            }
            let description = row.unavailableReason.map { "⊘ \($0) · \(row.description)" } ?? row.description
            return "\(marker) \(row.name) | \(description) | \(row.meta)"
        }
    }

    @Test func rowsShowTheTUIStatusDescriptionAndConfigurationInItsOrder() throws {
        #expect(lines(rows(try team())) == [
            "⠋ code-reviewer | Reviews changes for regressions | Claude Sonnet 4.5 (high)",
            "✗ librarian | Searches project docs | Claude Haiku 4.5 (low)",
            "⊘ retired | Previously active subagent conversation | GPT 5.6 Sol (medium)",
            "⊘ migrator | Moves data between schemas | Claude Haiku 4.5 (low)",
            "○ ard-expert | Ard language authority | GPT 5.3 Codex (off)",
            "  designer | ⊘ not started · UI/UX expert for front-end work | Claude Opus 4.1 (medium)",
        ])
    }

    @Test func rowsFollowTheRosterAsAgentsChangeState() throws {
        let finished = try roster(definitions: [definition("code-reviewer", "Reviews"), definition("librarian", "Searches")],
                                  conversations: [conversation("code-reviewer", "idle", model: "openai/gpt-5.3-codex", thinking: "low"),
                                                  conversation("librarian", "running", model: "openai/gpt-5.3-codex", thinking: "low")])
        #expect(lines(rows(finished)) == [
            "⠋ librarian | Searches | GPT 5.3 Codex (low)",
            "○ code-reviewer | Reviews | GPT 5.3 Codex (low)",
        ])
    }

    @Test func configurationNamesTheModelLikeTheCatalogOrTheTUI() {
        let catalog = [WireModelCapability(id: "anthropic/claude-sonnet-4-5", name: "Claude Sonnet 4.5", provider: "anthropic",
                                           api: "anthropic", contextWindow: 200_000, maxInputTokens: nil, maxOutputTokens: nil,
                                           thinkingLevels: nil, inputs: nil, available: true)]
        #expect(SubagentPicker.configuration(model: "anthropic/claude-sonnet-4-5", thinking: "high", models: catalog)
                == "Claude Sonnet 4.5 (high)")
        #expect(SubagentPicker.configuration(model: "openai/gpt-5.3-codex", thinking: "") == "GPT 5.3 Codex")
        #expect(SubagentPicker.configuration(model: "", thinking: "high") == "(high)")
        #expect(SubagentPicker.modelName("anthropic/claude-sonnet-4-6") == "Claude Sonnet 4.6")
        #expect(SubagentPicker.modelName("openai-codex/gpt-5.6-sol") == "GPT 5.6 Sol")
        #expect(SubagentPicker.modelName("local-model") == "Local Model")
    }

    @Test func searchMatchesNamesAndDescriptionsAndSelectsTheFirstStartedAgent() throws {
        let rows = rows(try team())
        let matches = PaletteCommand.filter(rows, query: "ex", wholeQuery: true)
        #expect(lines(matches) == [
            "○ ard-expert | Ard language authority | GPT 5.3 Codex (off)",
            "  designer | ⊘ not started · UI/UX expert for front-end work | Claude Opus 4.1 (medium)",
        ])
        #expect(PaletteCommand.selection(in: matches, current: nil, key: "ex") == "ard-expert")
        #expect(PaletteCommand.selection(in: rows, current: nil, key: "") == "code-reviewer")
        #expect(PaletteCommand.filter(rows, query: "zebra", wholeQuery: true).isEmpty)
        #expect(SubagentPicker.noMatch("zebra") == "No subagents match “zebra”")
        #expect(SubagentPicker.empty == "No subagents are defined")
    }

    @Test func rowsReserveTheStatusColumnOnlyWhenTheyHaveStatus() throws {
        #expect(PaletteColumns(rows(try team())).status == PaletteColumns.statusWidth)
        #expect(PaletteColumns(PaletteCommand.builtIns()).status == 0)
    }

    /// A store attached to the team, with the palette showing the picker.
    private func store(diagnostics: [String] = []) async throws -> (SessionStore, PickerSessionStub) {
        var session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "test", model: "anthropic/claude-opus-4-1",
                                     thinking: "medium", workspace: "tmp", date: "", messages: [])
        let team = try team()
        session.subagents = SubagentRoster(items: team.items, diagnostics: diagnostics)
        session.followUps = try FollowUpState(.init(count: 0, previews: nil, annotationIds: nil))
        let client = PickerSessionStub(session: session)
        let store = SessionStore(fixture: .init(sessions: [session]), client: client)
        store.attach()
        try await eventually { store.connectionState == .connected }
        store.ui.palette = true
        store.ui.subagentsPresented = true
        return (store, client)
    }

    @Test func returnOpensAStartedAgentAndRefusesOneThatHasNotStarted() async throws {
        let (store, _) = try await store()
        let picker = SubagentPickerState()
        let rows = rows(try team())
        let designer = try #require(rows.first { $0.id == "designer" })
        picker.activate(designer, store: store)
        #expect(picker.refusal.rowID == "designer")
        #expect(store.ui.palette)
        #expect(store.ui.workspace.panes == [.conversation])

        let reviewer = try #require(rows.first { $0.id == "code-reviewer" })
        picker.activate(reviewer, store: store)
        #expect(store.ui.palette == false)
        #expect(store.ui.workspace.panes == [.conversation, .agent("code-reviewer")])
        #expect(store.ui.workspace.selected == .agent("code-reviewer"))
    }

    @Test func dismissAlwaysAsksFirstAndKeepsThePickerOpen() async throws {
        let (store, client) = try await store()
        let picker = SubagentPickerState()

        picker.selectedID = "designer"
        #expect(picker.canDismiss(picker.selectedID, in: store) == false)
        picker.requestDismissal(store: store)
        #expect(picker.confirmation == nil)

        // An idle agent with no outstanding work still asks.
        picker.selectedID = "ard-expert"
        picker.requestDismissal(store: store)
        #expect(picker.confirmation?.agent.name == "ard-expert")
        picker.cancelDismissal()
        #expect(picker.confirmation == nil)
        #expect(await client.dismissals == [])

        picker.selectedID = "code-reviewer"
        picker.requestDismissal(store: store)
        let confirmation = try #require(picker.confirmation)
        #expect(confirmation.agent.name == "code-reviewer")
        #expect(confirmation.session == "s")
        await picker.confirmDismissal(confirmation, store: store)
        #expect(await client.dismissals == ["s/subagent_code-reviewer@4"])
        #expect(picker.confirmation == nil)
        #expect(store.ui.palette)
        #expect(store.ui.subagentsPresented)
    }

    @Test func aRowMenuDismissesThatRowRatherThanTheSelection() async throws {
        let (store, _) = try await store()
        let picker = SubagentPickerState()
        picker.selectedID = "code-reviewer"
        picker.requestDismissal("librarian", store: store)
        #expect(picker.confirmation?.agent.name == "librarian")
    }

    /// Draws the picker as the palette shows it. Set KIT_SNAPSHOT_DIR to
    /// write the render there for review.
    private func host(_ store: SessionStore, snapshot: String) async throws -> NSHostingView<some View> {
        _ = NSApplication.shared
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let host = NSHostingView(rootView: SubagentsPanel(state: store).environment(\.mica, theme)
            .environment(\.colorScheme, .dark))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 800, height: 600), styleMask: [.borderless],
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        for _ in 0..<10 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(30)) }
        host.frame = NSRect(origin: .zero, size: host.fittingSize)
        host.layoutSubtreeIfNeeded()
        if let directory = ProcessInfo.processInfo.environment["KIT_SNAPSHOT_DIR"] {
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: directory).appendingPathComponent("subagents-\(snapshot).png"))
        }
        window.close()
        return host
    }

    /// Warnings and the Dismiss action share the palette's fixed frame with
    /// the rows rather than growing it.
    @Test func warningsAndTheDismissActionKeepThePaletteFrame() async throws {
        let (store, _) = try await store(diagnostics: ["~/.kit/agents/tester.md: unknown model"])
        let host = try await host(store, snapshot: "dark")
        #expect(host.fittingSize == CGSize(width: PaletteMetrics.width, height: PaletteMetrics.height))
    }
}
