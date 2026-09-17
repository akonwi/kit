import Foundation
import Testing
@testable import Kit

private actor DirectoryClient: SessionDirectoryClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var inputs: [WireChangeCWDInput] = []
    let loseFirst: Bool
    init(loseFirst: Bool = false) { self.loseFirst = loseFirst }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { Self.value }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func vcs(_ session: String) async throws -> WireSessionVCSStatus {
        WireSessionVCSStatus(sessionId: session, cwd: "/tmp", status: nil)
    }
    func changeDirectory(_ session: String, input: WireChangeCWDInput) async throws -> SessionExcerpt {
        inputs.append(input)
        if loseFirst && inputs.count == 1 { throw ClientError.disconnected }
        return Self.value
    }
    static let value = SessionExcerpt(id: "a", title: "Session", sourceTitle: "Test", model: "p/m",
                                     thinking: "medium", workspace: "tmp", cwd: "/tmp", date: "", messages: [])
}

@MainActor struct DirectoryChangeTests {
    @Test func relativeRetryUsesSameMutationAndLocksTargetUntilResolved() async {
        let client = DirectoryClient(loseFirst: true)
        let operation = DirectoryChangeOperation()
        #expect(await operation.perform(path: "..", session: "a", client: client, refresh: {}) == false)
        let original = operation.request?.mutationId
        #expect(await operation.perform(path: "/other", session: "a", client: client, refresh: {}) == false)
        #expect(await operation.perform(path: "..", session: "a", client: client, refresh: {}))
        let inputs = await client.inputs
        #expect(inputs.map(\.mutationId) == [original, original])
        #expect(inputs.map(\.path) == ["..", ".."])
        #expect(operation.request == nil)
    }

    @Test func acknowledgedMutationIsNotRepeatedWhenRefreshFails() async {
        let client = DirectoryClient()
        let operation = DirectoryChangeOperation()
        #expect(await operation.perform(path: "..", session: "a", client: client) { throw ClientError.disconnected } == false)
        #expect(operation.acknowledged)
        #expect(await operation.perform(path: "..", session: "a", client: client, refresh: {}))
        #expect(await client.inputs.count == 1)
    }

    @Test func workspaceScopeChangeClosesOldFileViewsButRetainsDraftNotes() {
        let workspace = WorkspaceState(demo: false)
        workspace.open(.file("old.swift"))
        workspace.scratchpad = "Unsent notes"
        workspace.invalidateDirectory()
        #expect(workspace.panes == [.conversation])
        #expect(workspace.selected == .conversation)
        #expect(workspace.scratchpad == "Unsent notes")
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_CWD_LIVE_TEST"] == "1"))
    func liveRelativeDirectoryChangeIsIdempotent() async throws {
        let client = try await HTTPClient.local()
        let model = try #require(try await client.models().first)
        let id = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        do {
            _ = try await client.createSession(.init(id: id, cwd: "/private/tmp/kit-macos-verification",
                name: "macOS cwd verification", model: model.id,
                thinkingLevel: model.thinkingLevels?.first?.rawValue ?? "off", temporary: true))
            let input = WireChangeCWDInput(mutationId: "cwd_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased(), path: "..")
            #expect(try await client.changeDirectory(id, input: input).cwd == "/private/tmp")
            #expect(try await client.changeDirectory(id, input: input).cwd == "/private/tmp")
            #expect(try await client.snapshot(id).cwd == "/private/tmp")
            #expect(try await client.vcs(id).cwd == "/private/tmp")
            _ = try await client.files(id)
            try await client.disposeSession(id)
        } catch {
            try await client.disposeSession(id)
            throw error
        }
    }
}
