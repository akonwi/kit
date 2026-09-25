import Foundation
import Testing
@testable import Kit

private actor ReloadClient: SessionReloadClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var calls = 0
    let fail: Bool
    init(fail: Bool = false) { self.fail = fail }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.disconnected }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func reloadSession(_ id: String) async throws -> WireReloadSessionResult {
        calls += 1
        if fail { throw ClientError.disconnected }
        let source = WirePromptSource(sectionId: "context", id: "agents", kind: .value4, path: "/tmp/AGENTS.md")
        return WireReloadSessionResult(sessionId: id, eventStreamId: "stream_test", sources: [source],
            diagnostics: [WirePromptDiagnostic(severity: "warning", code: "omitted", message: "Source exceeds size limit", source: source)],
            warnings: ["Active turn uses its existing context"])
    }
}

@MainActor struct SessionReloadTests {
    @Test func acknowledgedReloadRetainsAllDiagnosticsAndRefreshRetryOnlyReads() async throws {
        let client = ReloadClient()
        let operation = SessionReloadOperation()
        await operation.perform(session: "s", client: client) { throw ClientError.disconnected }
        let result = try #require(operation.result)
        #expect(result.warnings == ["Active turn uses its existing context"])
        #expect(result.diagnostics?.first?.message == "Source exceeds size limit")
        #expect(result.diagnostics?.first?.source.path == "/tmp/AGENTS.md")
        #expect(operation.refreshError != nil)
        await operation.refreshOnly({})
        #expect(operation.refreshError == nil)
        #expect(await client.calls == 1)
    }

    @Test func failedReloadStillResynchronizesWithoutAutomaticResubmission() async {
        let client = ReloadClient(fail: true)
        let operation = SessionReloadOperation()
        var refreshes = 0
        await operation.perform(session: "s", client: client) { refreshes += 1 }
        #expect(refreshes == 1)
        #expect(operation.error != nil)
        #expect(operation.pending == false)
        #expect(await client.calls == 1)
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_RELOAD_LIVE_TEST"] == "1"))
    func liveReloadPreservesTranscriptAndReturnsSources() async throws {
        let client = try await HTTPClient.local()
        let model = try #require(try await client.models().first)
        let id = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        do {
            _ = try await client.createSession(.init(id: id, cwd: "/private/tmp/kit-macos-verification",
                name: "macOS reload verification", model: model.id,
                thinkingLevel: model.thinkingLevels?.first?.rawValue ?? "off", temporary: true))
            let before = try await client.snapshot(id)
            let result = try await client.reloadSession(id)
            let after = try await client.snapshot(id)
            #expect(result.sessionId == id)
            #expect(!(result.sources ?? []).isEmpty)
            #expect(after.messages == before.messages)
            #expect(after.cwd == before.cwd)
            try await client.disposeSession(id)
        } catch {
            try? await client.disposeSession(id)
            throw error
        }
    }
}
