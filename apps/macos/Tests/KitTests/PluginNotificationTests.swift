import Foundation
import Testing
@testable import Kit

private actor NotificationMock: PluginNotificationClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var callbacks: [@Sendable (PluginNotification) async -> Void] = []
    var sessionsSeen: [String] = []
    var initialFailure: ClientError?
    init(initialFailure: ClientError? = nil) { self.initialFailure = initialFailure }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.disconnected }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func watchPluginNotifications(_ session: String, receive: @escaping @Sendable (PluginNotification) async -> Void) async throws {
        sessionsSeen.append(session); callbacks.append(receive)
        if let failure = initialFailure { initialFailure = nil; throw failure }
        try await Task.sleep(for: .seconds(120))
    }
    func emit(_ index: Int, _ notification: PluginNotification) async { await callbacks[index](notification) }
}

@MainActor struct PluginNotificationTests {
    private func notification(_ title: String = "Notice", persistent: Bool = false, variant: String = "info") -> PluginNotification {
        .init(pluginID: "demo", title: title, detail: "Details", variant: variant, persistent: persistent)
    }
    @Test func decoderValidatesShapeTextAndRawUTF8() throws {
        let valid = Data(#"{"pluginId":"demo","instance":"host:1","title":"Notice","subtitle":"line\nnext","variant":"warning","persistent":true}"#.utf8)
        let decoded = try PluginNotification.decode(valid)
        #expect(decoded.pluginID == "demo")
        #expect(decoded.detail == "line\nnext")
        #expect(decoded.persistent)
        for invalid in [
            Data(#"{"pluginId":"demo","instance":"host:1","title":"","variant":"info"}"#.utf8),
            Data(#"{"pluginId":"demo","instance":"host:1","title":"Notice","variant":"success"}"#.utf8),
            Data(#"{"pluginId":"demo","instance":"host:1","title":"Notice","variant":"info","unknown":true}"#.utf8),
            Data(#"{"pluginId":"demo","instance":"host:1","title":"Notice","subtitle":"\u001b[2J","variant":"info"}"#.utf8),
            Data(#"{"pluginId":"demo","instance":"host:1","title":"First","ti\u0074le":"Second","variant":"info"}"#.utf8),
            Data(repeating: 32, count: 32 * 1024), Data([0xff])
        ] { #expect(throws: (any Error).self) { try PluginNotification.decode(invalid) } }
    }
    @Test func existingAlertsShowSeverityProvenanceAndPersistentDismissal() throws {
        let feedback = SessionFeedback()
        feedback.showPluginNotification(notification("Warning", persistent: true, variant: "warning"))
        let warning = try #require(feedback.visible)
        #expect(warning.title == "Warning · demo")
        #expect(warning.detail == "Details")
        #expect(warning.tone == .warning)
        #expect(feedback.notices.map(\.id) == [warning.id])
        feedback.showPluginNotification(notification("Error", variant: "error"))
        #expect(feedback.visible?.tone == .error)
        feedback.clearTransientPluginNotifications()
        #expect(feedback.items.map(\.title) == ["Warning · demo"])
        feedback.dismiss(warning.id)
        #expect(feedback.items.isEmpty)
        feedback.showPluginNotification(notification())
        #expect(feedback.visible?.tone == .info)
        feedback.expire(try #require(feedback.visible?.id))
        #expect(feedback.items.isEmpty)
    }
    @Test func persistentPluginAlertsAreBoundedWithoutEvictingOtherNotices() {
        let feedback = SessionFeedback()
        feedback.show(key: "other", title: "Other notice", persistent: true)
        for i in 0..<40 { feedback.showPluginNotification(notification("Notice \(i)", persistent: true)) }
        #expect(feedback.notices.count == 33)
        #expect(feedback.notices.first?.title == "Other notice")
        #expect(feedback.notices.dropFirst().first?.title == "Notice 8 · demo")
    }
    @Test func protocolFailuresStopButDisconnectsReconnectWithoutReplay() async throws {
        let invalid = NotificationMock(initialFailure: .invalidPayload)
        let disconnected = NotificationMock(initialFailure: .disconnected)
        let invalidWatch = PluginNotificationWatch(), reconnectingWatch = PluginNotificationWatch()
        defer { invalidWatch.stop(); reconnectingWatch.stop() }
        invalidWatch.start(session: "one", client: invalid) { _ in Issue.record("Unexpected event") }
        reconnectingWatch.start(session: "two", client: disconnected) { _ in Issue.record("Unexpected event") }
        for _ in 0..<200 { if await disconnected.sessionsSeen.count == 2 { break }; try await Task.sleep(for: .milliseconds(10)) }
        #expect(await disconnected.sessionsSeen == ["two", "two"])
        #expect(await invalid.sessionsSeen == ["one"])
    }

    @Test func attachmentGenerationSuppressesOldCallbacksAndStopPreventsDelivery() async throws {
        let client = NotificationMock(), watch = PluginNotificationWatch(), feedback = SessionFeedback()
        watch.start(session: "one", client: client) { feedback.showPluginNotification($0) }
        for _ in 0..<100 { if await client.callbacks.count == 1 { break }; try await Task.sleep(for: .milliseconds(5)) }
        #expect(await client.sessionsSeen == ["one"])
        await client.emit(0, notification("First"))
        #expect(feedback.visible?.title == "First · demo")
        watch.start(session: "two", client: client) { feedback.showPluginNotification($0) }
        for _ in 0..<100 { if await client.callbacks.count == 2 { break }; try await Task.sleep(for: .milliseconds(5)) }
        #expect(await client.sessionsSeen == ["one", "two"])
        await client.emit(1, notification("Current"))
        await client.emit(0, notification("Stale"))
        #expect(feedback.visible?.title == "Current · demo")
        watch.stop()
        await client.emit(1, notification("After stop"))
        #expect(feedback.visible?.title == "Current · demo")
    }
}
