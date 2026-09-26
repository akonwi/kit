import Foundation
import Testing
@testable import Kit

private actor ReloadClient: SessionReloadClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var calls = 0
    let fail: Bool
    let clean: Bool
    let includeDiagnostic: Bool
    var failRefresh = false
    var holdReload = false
    var pendingReload: CheckedContinuation<Void, Never>?
    func pauseReload() { holdReload = true }
    func reloadWaiting() -> Bool { pendingReload != nil }
    func resumeReload() { pendingReload?.resume(); pendingReload = nil; holdReload = false }
    init(fail: Bool = false, clean: Bool = false, includeDiagnostic: Bool = true) {
        self.fail = fail; self.clean = clean; self.includeDiagnostic = includeDiagnostic
    }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        if failRefresh { throw ClientError.disconnected }
        var session = SessionExcerpt(id: id, title: "Test", sourceTitle: "Test", model: "m", thinking: "off", workspace: "/tmp", date: "", messages: [])
        session.followUps = try FollowUpState(WireFollowUpQueue(count: 0, previews: [], annotationIds: nil))
        return session
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(try snapshot(id))
        try await Task.sleep(for: .seconds(60))
    }
    func setRefreshFailure(_ fail: Bool) { failRefresh = fail }
    func reloadSession(_ id: String) async throws -> WireReloadSessionResult {
        calls += 1
        if holdReload { await withCheckedContinuation { pendingReload = $0 } }
        if fail { throw ClientError.disconnected }
        let source = WirePromptSource(sectionId: "context", id: "agents", kind: .value4, path: "/tmp/AGENTS.md")
        return WireReloadSessionResult(sessionId: id, eventStreamId: "stream_test", sources: [source],
            diagnostics: clean || !includeDiagnostic ? [] : [WirePromptDiagnostic(severity: "warning", code: "omitted", message: "Source exceeds size limit", source: source)],
            warnings: clean ? [] : ["Active turn uses its existing context"])
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
        let failedRefresh = try #require(SessionReloadFeedback.from(operation))
        #expect(failedRefresh.title == "Session context reloaded with issues")
        #expect(failedRefresh.detail.hasPrefix("Session refresh failed:"))
        #expect(failedRefresh.report?.warningCount == 2)
        #expect(failedRefresh.report?.diagnostics.first?.message.contains("Session refresh failed:") == true)
        #expect(failedRefresh.persistent)
        #expect(failedRefresh.recovery == .refresh)
        await operation.refreshOnly({})
        #expect(operation.refreshError == nil)
        let warning = try #require(SessionReloadFeedback.from(operation))
        #expect(warning.detail == "2 warnings")
        let report = try #require(warning.report)
        #expect(report.diagnostics.map(\.message) == ["Active turn uses its existing context", "Source exceeds size limit"])
        #expect(report.diagnostics[0].sourcePath == nil)
        #expect(report.diagnostics[1].sourcePath == "/tmp/AGENTS.md")
        #expect(report.diagnostics[1].sourceID == "agents")
        #expect(report.diagnostics[1].severity == "warning")
        #expect(report.diagnostics[1].code == "omitted")
        #expect(report.sources.count == 1)
        #expect(report.sources[0].id == "agents")
        #expect(report.sources[0].path == "/tmp/AGENTS.md")
        #expect(report.accessibilityDetail.contains("Loaded sources:\nagents · /tmp/AGENTS.md"))
        #expect(warning.persistent)
        #expect(warning.recovery == nil)
        #expect(await client.calls == 1)
    }

    @Test func oneWarningPreviewsItsMessageWithoutAWarningCount() async throws {
        let client = ReloadClient(includeDiagnostic: false), operation = SessionReloadOperation()
        await operation.perform(session: "s", client: client, refresh: {})
        let summary = try #require(SessionReloadFeedback.from(operation))
        #expect(summary.detail == "Active turn uses its existing context")
        #expect(summary.report?.warningCount == 1)
        #expect(summary.report?.diagnostics.first?.sourcePath == nil)
        #expect(summary.report?.sources.first?.path == "/tmp/AGENTS.md")
        #expect(summary.persistent) // Warnings stay available until acknowledged.
    }

    @Test func failedReloadStillResynchronizesWithoutAutomaticResubmission() async {
        let client = ReloadClient(fail: true)
        let operation = SessionReloadOperation()
        var refreshes = 0
        await operation.perform(session: "s", client: client) { refreshes += 1 }
        #expect(refreshes == 1)
        #expect(operation.error != nil)
        #expect(operation.pending == false)
        let failure = SessionReloadFeedback.from(operation)
        #expect(failure?.title == "Session reload failed")
        #expect(failure?.persistent == true)
        #expect(failure?.recovery == .reload)
        #expect(await client.calls == 1)
    }

    @Test func cleanReloadReportsAnEphemeralSuccessToast() async throws {
        let client = ReloadClient(clean: true), operation = SessionReloadOperation()
        await operation.perform(session: "s", client: client, refresh: {})
        let summary = try #require(SessionReloadFeedback.from(operation))
        #expect(summary.title == "Session context reloaded")
        #expect(summary.detail == "")
        #expect(!summary.persistent)
        #expect(summary.recovery == nil)
    }

    @Test func paletteReloadOperationUsesSessionToastAndRefreshOnlyRecovery() async throws {
        let client = ReloadClient(clean: true)
        let session = try await client.snapshot("s")
        let store = SessionStore(fixture: .init(sessions: [session]), client: client)
        store.attach(); defer { store.detach() }
        for _ in 0..<100 where store.connectionState != .connected { try await Task.sleep(for: .milliseconds(10)) }
        #expect(store.connectionState == .connected)
        await client.setRefreshFailure(true)
        await store.reloadSession(for: "s")
        let toast = try #require(store.feedback.visible)
        #expect(toast.title == "Session context reloaded with issues")
        #expect(toast.persistent)
        #expect(toast.actionTitle == "Refresh session")
        #expect(await client.calls == 1)
        await client.setRefreshFailure(false)
        toast.action?()
        for _ in 0..<100 where store.feedback.visible?.title != "Session context reloaded" { try await Task.sleep(for: .milliseconds(10)) }
        #expect(store.feedback.visible?.title == "Session context reloaded")
        #expect(store.feedback.visible?.persistent == false)
        #expect(await client.calls == 1)
    }

    @Test func cancelledCallerStillPublishesAcknowledgedReloadEvidence() async throws {
        let client = ReloadClient()
        await client.pauseReload()
        let session = try await client.snapshot("s")
        let store = SessionStore(fixture: .init(sessions: [session]), client: client)
        store.attach(); defer { store.detach() }
        for _ in 0..<100 where store.connectionState != .connected { try await Task.sleep(for: .milliseconds(10)) }
        #expect(store.connectionState == .connected)
        let reload = Task { await store.reloadSession(for: "s") }
        for _ in 0..<100 where !(await client.reloadWaiting()) { try await Task.sleep(for: .milliseconds(10)) }
        #expect(await client.reloadWaiting())
        reload.cancel()
        await client.resumeReload()
        await reload.value
        #expect(store.feedback.visible?.title == "Session context reloaded with issues")
        let report = try #require(store.feedback.visible?.reloadReport)
        #expect(report.diagnostics.map(\.message).contains("Source exceeds size limit"))
        #expect(report.warningCount == 2)
        #expect(store.feedback.visible?.persistent == true)
        #expect(await client.calls == 1)
    }

    @Test func switchingSessionsKeepsReloadResultWithItsOriginalSession() async throws {
        let client = ReloadClient(clean: true)
        await client.pauseReload()
        let first = try await client.snapshot("s"), second = try await client.snapshot("other")
        let store = SessionStore(fixture: .init(sessions: [first, second]), client: client)
        store.attach(); defer { store.detach() }
        for _ in 0..<100 where store.connectionState != .connected { try await Task.sleep(for: .milliseconds(10)) }
        #expect(store.connectionState == .connected)
        let originalFeedback = store.feedback
        let reload = Task { await store.reloadSession(for: "s") }
        for _ in 0..<100 where !(await client.reloadWaiting()) { try await Task.sleep(for: .milliseconds(10)) }
        #expect(await client.reloadWaiting())
        store.select("other")
        await client.resumeReload()
        await reload.value
        #expect(originalFeedback.visible?.title == "Session context reloaded with issues")
        #expect(originalFeedback.visible?.actionTitle == "Refresh session")
        #expect(store.feedback.items.isEmpty)
        await store.reloadSession(for: "s") // A delayed palette task cannot target the new selection.
        #expect(await client.calls == 1)
        store.select("s")
        originalFeedback.visible?.action?()
        for _ in 0..<100 where originalFeedback.visible?.actionTitle != nil { try await Task.sleep(for: .milliseconds(10)) }
        #expect(originalFeedback.visible?.title == "Session context reloaded")
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
