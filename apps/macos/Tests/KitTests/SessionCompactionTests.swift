import Foundation
import Testing
@testable import Kit

private actor CompactClient: SessionCompactionClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var inputs: [String] = []
    let loseFirst: Bool
    let compacted: Bool
    init(loseFirst: Bool = false, compacted: Bool = true) { self.loseFirst = loseFirst; self.compacted = compacted }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.disconnected }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func compactSession(_ id: String, input: WireCompactSessionInput) async throws -> WireCompactSessionResult {
        inputs.append(input.operationId)
        if loseFirst && inputs.count == 1 { throw ClientError.disconnected }
        return .init(operationId: input.operationId, compacted: compacted,
                     checkpointId: compacted ? "checkpoint_test" : nil, eventStreamId: "stream_new")
    }
}

@MainActor struct SessionCompactionTests {
    @Test func lostAcknowledgementKeepsIdentityAndRefreshesBeforeExplicitRetry() async {
        let client = CompactClient(loseFirst: true)
        let operation = SessionCompactionOperation()
        var reads = 0
        await operation.perform(session: "s", client: client) { reads += 1 }
        let original = operation.operationID
        operation.beginNewIfResolved()
        #expect(operation.operationID == original)
        #expect(reads == 1)
        operation.dismissFeedback()
        #expect(operation.showsFeedback == false)
        #expect(operation.operationID == original)
        await operation.perform(session: "s", client: client) { reads += 1 }
        #expect(await client.inputs == [original, original])
        #expect(reads == 2)
        #expect(operation.title == "Session compacted")
    }

    @Test func acknowledgedCompactionRetriesOnlySnapshotAndDistinguishesNoOp() async {
        let client = CompactClient(compacted: false)
        let operation = SessionCompactionOperation()
        await operation.perform(session: "s", client: client) { throw ClientError.disconnected }
        let original = operation.operationID
        operation.beginNewIfResolved()
        #expect(operation.operationID == original)
        await operation.perform(session: "s", client: client) {}
        #expect(await client.inputs.count == 1)
        #expect(operation.title == "Not enough turns to compact")
        #expect(operation.refreshError == nil)
        operation.beginNewIfResolved()
        #expect(operation.operationID == nil)
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_COMPACT_HISTORY_TEST"] == "1"))
    func liveHistoricalCompactionPreservesMessagesAndReplaysResult() async throws {
        let client = try await HTTPClient.local()
        let model = try #require(try await client.models().first { $0.id == "openai-codex/gpt-5.6-sol" })
        let id = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        do {
            _ = try await client.createSession(.init(id: id, cwd: "/private/tmp/kit-macos-verification",
                name: "macOS compaction verification", model: model.id, thinkingLevel: "off", temporary: true))
            for number in 1...4 {
                _ = try await client.submit(id, input: .init(text: "Isolated UI verification exchange \(number). Reply ACK only. Do not call tools.", attachmentIds: nil, annotationIds: nil))
                let deadline = Date().addingTimeInterval(60)
                while true {
                    let snapshot = try await client.snapshot(id)
                    if snapshot.activeRunID == nil { break }
                    guard Date() < deadline else { throw ClientError.disconnected }
                    try await Task.sleep(for: .milliseconds(500))
                }
            }
            let before = try await client.snapshot(id)
            let input = WireCompactSessionInput(operationId: "compact_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased())
            let first = try await client.compactSession(id, input: input)
            let second = try await client.compactSession(id, input: input)
            let after = try await client.snapshot(id)
            #expect(first.compacted)
            #expect(second.checkpointId == first.checkpointId)
            #expect(second.eventStreamId == first.eventStreamId)
            for message in before.messages { #expect(after.messages.contains(message)) }
            #expect(after.activeRunID == nil)
            try await client.disposeSession(id)
        } catch {
            try? await client.disposeSession(id)
            throw error
        }
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_COMPACT_LIVE_TEST"] == "1"))
    func liveEmptyCompactionIsIdempotent() async throws {
        let client = try await HTTPClient.local()
        let model = try #require(try await client.models().first)
        let id = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        do {
            _ = try await client.createSession(.init(id: id, cwd: "/private/tmp/kit-macos-verification",
                name: "macOS compaction verification", model: model.id,
                thinkingLevel: model.thinkingLevels?.first?.rawValue ?? "off", temporary: true))
            let input = WireCompactSessionInput(operationId: "compact_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased())
            let first = try await client.compactSession(id, input: input)
            let second = try await client.compactSession(id, input: input)
            #expect(first.compacted == false)
            #expect(second.operationId == first.operationId)
            #expect(second.eventStreamId == first.eventStreamId)
            #expect(try await client.snapshot(id).messages.isEmpty)
            try await client.disposeSession(id)
        } catch {
            try? await client.disposeSession(id)
            throw error
        }
    }
}
