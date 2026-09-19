import AppKit
import SwiftUI
import Foundation
import Testing
@testable import Kit

private actor MutationStub: SessionMutationClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var snapshotFailure: ClientError?
    var durableMessages: [String: [TranscriptMessage]] = [:]
    var snapshotReads = 0
    func failSnapshots(_ error: ClientError?) { snapshotFailure = error }
    func acceptThenLoseAcknowledgement(queued: Bool = false) {
        if queued {
            queue = WireFollowUpQueue(count: 1, previews: [calls.last!.text], annotationIds: nil)
        } else {
            durableMessages["s"] = [TranscriptMessage(id: "server-user", role: "user", text: calls.last!.text, tools: [])]
        }
        continuation?.resume(throwing: ClientError.disconnected)
        continuation = nil
    }
    var running = false
    func beginRun() { running = true }
    var calls: [WirePromptInput] = []
    var continuation: CheckedContinuation<WirePromptSubmission, any Error>?
    var aborted: [String] = []
    var queue = WireFollowUpQueue(count: 0, previews: [], annotationIds: nil)
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        snapshotReads += 1
        if let snapshotFailure { throw snapshotFailure }
        var value = SessionExcerpt(id: id, title: "Test", sourceTitle: "Test", model: "m", thinking: "high", workspace: "w", date: "", messages: [])
        value.messages = durableMessages[id] ?? []
        value.followUps = try FollowUpState(queue)
        value.activeRunID = running ? "run" : nil
        return value
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(try snapshot(id))
        try await Task.sleep(for: .seconds(60))
    }
    func submit(_ session: String, input: WirePromptInput) async throws -> WirePromptSubmission {
        calls.append(input)
        return try await withCheckedThrowingContinuation { continuation = $0 }
    }
    func acknowledge(_ session: String = "s", queued: Bool = false) {
        queue = WireFollowUpQueue(count: queued ? 1 : 0, previews: queued ? ["Queued prompt"] : [], annotationIds: nil)
        continuation?.resume(returning: WirePromptSubmission(reservation: queued ? nil : WireRunReservation(sessionId: session, turnId: "turn", runId: "run"), queued: queued, queue: queue))
        continuation = nil
    }
    func reject(_ error: ClientError) { continuation?.resume(throwing: error); continuation = nil }
    func followUps(_ session: String) async throws -> FollowUpState { try FollowUpState(queue) }
    func restoreFollowUps(_ session: String) async throws -> WireRestoreFollowUpsResult {
        queue = WireFollowUpQueue(count: 0, previews: [], annotationIds: nil)
        return WireRestoreFollowUpsResult(messages: [WirePromptInput(text: "Restored", attachmentIds: ["attachment"], annotationIds: nil)], queue: queue)
    }
    func promoteFollowUps(_ session: String) async throws -> WirePromoteFollowUpsResult {
        queue = WireFollowUpQueue(count: 0, previews: [], annotationIds: nil)
        return WirePromoteFollowUpsResult(promoted: 1, queue: queue)
    }
    func abort(_ session: String, run: String) async throws { aborted.append(run) }
}

@MainActor struct SessionOperationsTests {
    private func wait(_ condition: () async -> Bool) async throws {
        for _ in 0..<100 {
            if await condition() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        Issue.record("Operation did not reach the expected state")
    }
    private func store(_ client: MutationStub) async throws -> SessionStore {
        let session = try await client.snapshot("s")
        let state = SessionStore(fixture: Fixture(sessions: [session]), client: client)
        state.attach()
        try await wait { state.connectionState == .connected }
        return state
    }
    @Test func acknowledgementClearsOnlyTheSubmittedDraftAndDoesNotInventMessages() async throws {
        let client = MutationStub()
        let state = try await store(client)
        defer { state.detach() }
        state.ui.draft = "Original"
        state.send(); state.send()
        try await wait { await client.calls.count == 1 }
        #expect(state.operations.submission == .pending)
        #expect(state.ui.draft == "Original")
        state.ui.draft = "Next message"
        await client.acknowledge()
        try await wait { state.operations.submission == .acknowledged("Sent") }
        #expect(state.ui.draft == "Next message")
        #expect(state.messages == [])
        #expect(await client.calls.count == 1)
    }
    @Test func rejectedAndAmbiguousSubmissionsKeepDraftWithoutAutomaticRetry() async throws {
        let client = MutationStub()
        let state = try await store(client)
        defer { state.detach() }
        state.ui.draft = "Keep me"
        state.ui.serverAttachmentIDs = ["attachment"]
        state.send()
        try await wait { await client.calls.count == 1 }
        await client.reject(.http(400))
        try await wait { if case .failed = state.operations.submission { true } else { false } }
        #expect(state.ui.draft == "Keep me")
        #expect(state.ui.serverAttachmentIDs == ["attachment"])
        state.send()
        try await wait { await client.calls.count == 2 }
        await client.reject(.disconnected)
        try await wait { state.operations.recovery == .ready }
        #expect(await client.calls.count == 2)
        #expect(state.ui.draft == "Keep me")
        #expect(state.ui.serverAttachmentIDs == ["attachment"])
    }
    @Test(arguments: [false, true]) func lostAcknowledgementResynchronizesWithoutReplaying(queued: Bool) async throws {
        let client = MutationStub()
        let state = try await store(client)
        defer { state.detach() }
        state.ui.draft = "Accepted despite lost response"
        state.send()
        try await wait { await client.calls.count == 1 }
        await client.acceptThenLoseAcknowledgement(queued: queued)
        try await wait { state.operations.recovery == .ready && state.connectionState == .connected }
        #expect(state.ui.draft == "Accepted despite lost response")
        #expect(state.operations.submission == .idle)
        if queued {
            #expect(state.operations.queue?.previews == ["Accepted despite lost response"])
        } else {
            #expect(state.messages.map(\.id) == ["server-user"])
            #expect(state.messages.map(\.text) == ["Accepted despite lost response"])
        }
        #expect(await client.calls.count == 1)
        state.send()
        try await wait { await client.calls.count == 2 }
        await client.acknowledge()
        try await wait { state.ui.draft.isEmpty }
        #expect(await client.calls.count == 2)
    }

    @Test func failedRecoveryRetriesOnlyReadsUntilExplicitResubmission() async throws {
        let client = MutationStub()
        let state = try await store(client)
        defer { state.detach() }
        state.ui.draft = "Keep this draft"
        state.send()
        try await wait { await client.calls.count == 1 }
        await client.failSnapshots(.disconnected)
        await client.acceptThenLoseAcknowledgement()
        try await wait { if case .failed = state.operations.recovery { true } else { false } }
        #expect(state.ui.draft == "Keep this draft")
        state.send()
        #expect(await client.calls.count == 1)
        await client.failSnapshots(nil)
        state.attach()
        try await wait { state.operations.recovery == .ready && state.connectionState == .connected }
        #expect(state.messages.map(\.text) == ["Keep this draft"])
        #expect(await client.calls.count == 1)
        state.ui.draft = "User chose to send this"
        state.send()
        try await wait { await client.calls.count == 2 }
        #expect(await client.calls.last?.text == "User chose to send this")
        await client.acknowledge()
        try await wait { state.ui.draft.isEmpty }
    }

    @Test func recoveryAfterSwitchDoesNotReplaceAnotherSessionsDraft() async throws {
        let client = MutationStub()
        let state = try await store(client)
        defer { state.detach() }
        state.ui.draft = "Session A"
        state.send()
        try await wait { await client.calls.count == 1 }
        state.select("b")
        state.ui.draft = "Session B"
        await client.acceptThenLoseAcknowledgement()
        try await Task.sleep(for: .milliseconds(30))
        #expect(state.ui.draft == "Session B")
        #expect(state.messages.map(\.text) == [])
        state.select("s")
        try await wait { state.operations.recovery == .ready }
        #expect(state.ui.draft == "Session A")
        #expect(state.messages.map(\.text) == ["Session A"])
        state.ui.draft = "New draft while reviewing"
        #expect(state.ui.draft == "New draft while reviewing")
        #expect(await client.calls.count == 1)
    }

    @Test func acknowledgementAfterSessionSwitchClearsTheOriginalDraftOnReturn() async throws {
        let client = MutationStub()
        let state = try await store(client)
        defer { state.detach() }
        state.ui.draft = "Session A"
        state.send()
        try await wait { await client.calls.count == 1 }
        state.select("b")
        state.ui.draft = "Session B"
        await client.acknowledge()
        try await Task.sleep(for: .milliseconds(30))
        #expect(state.ui.draft == "Session B")
        state.select("s")
        #expect(state.ui.draft == "")
        #expect(state.operations.submission == .acknowledged("Sent"))
    }
    @Test func queuedAcknowledgementAndRestoreUseServerPayload() async throws {
        let client = MutationStub()
        let state = try await store(client)
        defer { state.detach() }
        state.ui.draft = "Follow up"
        state.send()
        try await wait { await client.calls.count == 1 }
        await client.acknowledge(queued: true)
        try await wait { state.operations.submission == .acknowledged("Queued") }
        #expect(state.ui.draft == "")
        #expect(state.operations.queue?.previews == ["Queued prompt"])
        state.changeQueue(.edit)
        try await wait { !state.operations.queuePending }
        #expect(state.ui.draft == "Restored")
        #expect(state.ui.serverAttachmentIDs == ["attachment"])
        #expect(state.operations.queue?.count == 0)
    }
    @Test func abortWaitsForAuthoritativeTerminalState() async throws {
        let client = MutationStub()
        let operations = SessionOperations()
        var session = try await client.snapshot("s")
        session.activeRunID = "run"
        operations.reconcile(session)
        operations.abort(client: client, session: "s", run: "run")
        try await wait { await client.aborted.count == 1 }
        #expect(operations.abortingRun == "run")
        operations.reconcile(session)
        #expect(operations.abortingRun == "run")
        session.activeRunID = nil
        operations.reconcile(session)
        #expect(operations.abortingRun == nil)
    }
    @Test func renderLiveComposerQueueAndControls() async throws {
        let client = MutationStub()
        await client.beginRun()
        let state = try await store(client)
        defer { state.detach() }
        state.ui.draft = "Follow up"
        state.send()
        try await wait { await client.calls.count == 1 }
        await client.acknowledge(queued: true)
        try await wait { state.operations.submission == .acknowledged("Queued") }
        state.ui.draft = "Also check cancellation when the window closes."
        for dark in [false, true] {
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let view = VStack(spacing: 12) {
                Spacer(minLength: 0)
                ComposerView(state: state)
                SessionFooter(state: state)
            }.environment(\.mica, theme).environment(\.colorScheme, dark ? .dark : .light)
                .foregroundStyle(theme.text).background(theme.surface).frame(width: 840, height: 320)
            let host = NSHostingView(rootView: view)
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 840, height: 320),
                                  styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentView = host
            window.orderFront(nil)
            defer { window.close() }
            for _ in 0..<8 {
                host.layoutSubtreeIfNeeded()
                try await Task.sleep(for: .milliseconds(30))
            }
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-live-composer-\(dark ? "dark" : "light").png"))
            #expect(state.running)
            #expect(state.operations.queue?.count == 1)
        }
    }

    @Test func retryCountdownUsesServerDeadline() {
        let now = Date(timeIntervalSince1970: 0)
        #expect(SessionFooter.secondsRemaining("1970-01-01T00:00:05.500Z", now: now) == 6)
        #expect(SessionFooter.secondsRemaining("1970-01-01T00:00:05Z", now: now.addingTimeInterval(10)) == 0)
    }
}

extension SessionOperationsTests {
    @Test func annotationOnlyPromptSendsOrderedIDsAndRejectsStaleDrafts() async throws {
        let client = MutationStub()
        let state = try await store(client)
        defer { state.detach() }
        let anchor = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: "main.swift", fileRevision: "file_test", startLine: 1, endLine: 1), workingTreeDiff: nil)
        let first = try FileAnnotation(id: 1, anchor: anchor, body: "First", source: "source", stale: false, complete: true)
        let second = try FileAnnotation(id: 2, anchor: anchor, body: "Second", source: "source", stale: false, complete: true)
        state.annotationState.observe([first, second])
        state.send()
        try await wait { await client.calls.count == 1 }
        #expect(await client.calls.first?.text == "")
        #expect(await client.calls.first?.annotationIds == [1, 2])
        await client.acknowledge()
        try await wait { state.operations.submission == .acknowledged("Sent") }
        state.annotationState.observe([try FileAnnotation(id: 3, anchor: anchor, body: "Stale", source: "source", stale: true, complete: true)])
        state.send()
        #expect(await client.calls.count == 1)
        #expect(state.annotationState.records.map(\.id) == [3])
    }
}
