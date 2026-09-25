import AppKit
import SwiftUI
import Testing
@testable import Kit

private actor PluginCommandMock: PluginCommandClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    let excerpt: SessionExcerpt
    var requests: [WirePluginCommandInput] = []
    init(_ excerpt: SessionExcerpt) { self.excerpt = excerpt }
    func sessions() async throws -> [SessionExcerpt] { [excerpt] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { excerpt }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(excerpt)
        try await Task.sleep(for: .seconds(120))
    }
    func executePluginCommand(_ session: String, input: WirePluginCommandInput) async throws {
        requests.append(input)
        try await Task.sleep(for: .milliseconds(100))
    }
}

@MainActor struct PluginCommandTests {
    private func wire(_ instance: String = "host:1") -> WirePluginCommand {
        .init(id: "demo.echo", localId: "echo", pluginId: "demo", instance: instance,
              description: "Echo literal arguments", argName: "text", category: "plugins")
    }
    private func excerpt(_ commands: [PluginCommand]) -> SessionExcerpt {
        var value = SessionExcerpt(id: "session_one", title: "Plugin test", sourceTitle: "Test", model: "test/model", thinking: "off", workspace: "test", date: "", messages: [])
        value.pluginCommands = commands
        return value
    }
    @Test func catalogKeepsCanonicalGenerationIdentityAndRejectsMalformedMetadata() throws {
        let old = try #require(PluginCommand.project([wire()]).first)
        let replacement = try #require(PluginCommand.project([wire("host:2")]).first)
        #expect(old.name == "demo.echo")
        #expect(old.description == "Echo literal arguments")
        #expect(old.argName == "text")
        #expect(old.selectionID != replacement.selectionID)
        let rows = PaletteCommand.pluginCatalog([old])
        #expect(rows.map(\.name) == ["demo.echo"])
        #expect(rows.map(\.description) == ["Echo literal arguments"])
        #expect(rows.first?.plugin?.instance == "host:1")
        #expect(throws: ClientError.self) { try PluginCommand.project([wire(), wire()]) }
        #expect(throws: ClientError.self) { try PluginCommand.project([wire("host:1\n")]) }
        #expect(!PluginCommand.validSelection(id: "demo.echo/path", instance: "host:1"))
        let malformed = WirePluginCommand(id: "demo.extra.echo", localId: "echo", pluginId: "demo.extra", instance: "host:1", description: "Invalid domain", argName: nil, category: nil)
        #expect(throws: ClientError.self) { try PluginCommand.project([malformed]) }
    }
    @Test func executesLiteralArgumentsWithoutTouchingDraftAndRefusesOldSelection() async throws {
        let command = try #require(PluginCommand.project([wire()]).first)
        let session = excerpt([command]), client = PluginCommandMock(excerpt([command]))
        let state = SessionStore(fixture: Fixture(sessions: [session]), client: client)
        state.attach(); defer { state.detach() }
        for _ in 0..<100 where state.connectionState != .connected { try await Task.sleep(for: .milliseconds(5)) }
        #expect(state.pluginCommands == [command])
        state.ui.draft = "preserved composer draft"
        let args = "  quoted \"hello\" $(literal)\nnext  "
        await state.runPluginCommand(command, args: args)
        #expect(await client.requests.map(\.args) == [args])
        #expect(state.ui.draft == "preserved composer draft")
        #expect(!state.pluginCommandRunning)
        let stale = try #require(PluginCommand.project([wire("host:old")]).first)
        await state.runPluginCommand(stale, args: "never sent")
        #expect(await client.requests.count == 1)
        #expect(state.feedback.visible?.title == "Plugin command changed")
    }
    @Test func detachCancelsWaitAndSuppressesItsDelayedFeedback() async throws {
        let command = try #require(PluginCommand.project([wire()]).first)
        let session = excerpt([command]), client = PluginCommandMock(excerpt([command]))
        let state = SessionStore(fixture: Fixture(sessions: [session]), client: client)
        state.attach()
        for _ in 0..<100 where state.connectionState != .connected { try await Task.sleep(for: .milliseconds(5)) }
        let task = Task { await state.runPluginCommand(command, args: "") }
        for _ in 0..<100 where !state.pluginCommandRunning { try await Task.sleep(for: .milliseconds(5)) }
        #expect(state.pluginCommandRunning)
        #expect(state.feedback.visible?.title == "Running /demo.echo")
        state.detach()
        state.feedback.show(title: "New attachment feedback")
        await task.value
        #expect(!state.pluginCommandRunning)
        #expect(state.feedback.visible?.title == "New attachment feedback")
    }
    @Test func pluginDialogsKeepInitialValuesEmptyInputAndConfirmationLabels() throws {
        let owner = WirePluginInteractionOwner(pluginId: "demo", instance: "host:1")
        let request = WireInteractionRequest(plugin: owner, initialValue: "initial note", id: "interaction_test", sessionId: "session_one", kind: try #require(WireInteractionKind(rawValue: "input")), title: "Note", createdAt: "2026-09-21T00:00:00Z")
        let flow = try InteractionFlow(request: request)
        #expect(flow.answers[0].text == "initial note")
        flow.answers[0].text = ""
        #expect(flow.canContinue)
        #expect(flow.response(cancelled: false)?.value == "")
        let confirm = WireInteractionRequest(plugin: owner, confirmLabel: "Show toast", cancelLabel: "Cancel", defaultValue: true, id: "interaction_confirm", sessionId: "session_one", kind: try #require(WireInteractionKind(rawValue: "confirm")), title: "Continue", createdAt: "2026-09-21T00:00:00Z")
        let confirmation = try InteractionFlow(request: confirm)
        #expect(confirmation.question.optionLabels == ["true": "Show toast", "false": "Cancel"])
        #expect(confirmation.answers[0].choices == ["true"])
    }
}
