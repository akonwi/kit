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

/// A server that reports every compaction as failed for one reason.
private actor FailingCompactClient: SessionCompactionClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    let reason: WireCompactionFailureReason
    init(_ reason: WireCompactionFailureReason) { self.reason = reason }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.disconnected }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func compactSession(_ id: String, input: WireCompactSessionInput) async throws -> WireCompactSessionResult {
        throw CompactionFailed(reason: reason)
    }
}

@MainActor struct SessionCompactionTests {
    @Test func eachFailureReasonHasItsOwnExplanation() {
        let explanations = Dictionary(uniqueKeysWithValues: WireCompactionFailureReason.allCases.map { ($0, $0.explanation) })
        #expect(explanations == [
            .summaryTruncated: "The summary was longer than the compaction model's output limit.",
            .summaryInvalid: "The compaction model didn't return a usable summary.",
            .policyStop: "The provider stopped the summary under its usage policy.",
            .providerError: "The request to the model provider failed.",
            .notAdaptable: "The conversation can't be reduced to fit the model's context.",
            .canceled: "Compaction was canceled before it finished.",
            .conflict: "The session changed while compacting.",
            .internalError: "Kit couldn't finish compacting because of an internal error.",
        ])
        #expect(WireCompactionFailureReason.allCases.filter { !$0.retryable } == [.summaryTruncated, .policyStop, .notAdaptable])
    }

    @Test func aFailedCompactionExplainsItsReasonAndRetriesOnlyWhenItCouldHelp() async {
        let repeating = SessionCompactionOperation()
        await repeating.perform(session: "s", client: FailingCompactClient(.notAdaptable)) {}
        #expect(repeating.title == "Compaction failed")
        #expect(repeating.error == "The conversation can't be reduced to fit the model's context.")
        #expect(repeating.retryable == false)

        let transient = SessionCompactionOperation()
        await transient.perform(session: "s", client: FailingCompactClient(.providerError)) {}
        #expect(transient.error == "The request to the model provider failed.")
        #expect(transient.retryable)
    }

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
