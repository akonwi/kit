import AppKit
import Foundation
import Testing
import SwiftUI
@testable import Kit

private actor ScratchpadServer: ScratchpadClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    private var record = ScratchpadRecord(owner: "root", content: "shared", revision: 1, updatedAt: "2026-09-20T12:00:00Z")
    private var fail = false
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func scratchpad(session: String) async throws -> ScratchpadRecord { record }
    func updateScratchpad(session: String, content: String, expectedRevision: Int64) async throws -> ScratchpadRecord {
        try Task.checkCancellation()
        if fail { throw ClientError.disconnected }
        guard expectedRevision == record.revision else { throw ScratchpadFailure.conflict(record) }
        record = ScratchpadRecord(owner: "root", content: content, revision: record.revision + 1, updatedAt: "2026-09-20T12:00:01Z")
        return record
    }
    func remote(_ content: String) { record = ScratchpadRecord(owner: "root", content: content, revision: record.revision + 1, updatedAt: "2026-09-20T12:00:02Z") }
    func setFailure(_ value: Bool) { fail = value }
}

@MainActor struct ScratchpadStateTests {
    @Test func debouncedAutosaveCompletesWithoutCancellingItsRequest() async throws {
        let server = ScratchpadServer()
        let state = ScratchpadState(demo: false)
        await state.load(client: server, session: "root")
        state.bind(client: server, session: "root")
        let started = ContinuousClock.now
        state.edit("autosaved note")
        let deadline = started + .seconds(12)
        while state.status != .saved && ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(50))
        }
        #expect(state.status == .saved, "status: \(state.status)")
        #expect(ContinuousClock.now - started >= .seconds(5))
        #expect(try await server.scratchpad(session: "root").content == "autosaved note")
    }

    @Test func contentBoundaryMatchesServerRules() {
        #expect(ScratchpadRecord.validContent("# Notes\n\tindented"))
        #expect(!ScratchpadRecord.validContent("CR\rLF"))
        #expect(!ScratchpadRecord.validContent("invisible\u{200B}format"))
        #expect(!ScratchpadRecord.validContent(String(repeating: "x", count: 65537)))
    }

    @Test func acceptsNanosecondServerTimestamp() throws {
        let record = try ScratchpadRecord(WireScratchpad(ownerSessionId: "root", content: "notes",
            revision: "1", updatedAt: "2026-09-20T12:00:00.123456789Z"))
        #expect(record.revision == 1)
    }

    @Test func cleanRemoteAndConflictedDraft() async throws {
        let server = ScratchpadServer()
        let state = ScratchpadState(demo: false)
        await state.load(client: server, session: "child")
        #expect(state.draft == "shared")
        await server.remote("from agent")
        state.observe(try await server.scratchpad(session: "child"))
        #expect(state.draft == "from agent")
        #expect(state.status == .saved)
        state.edit("my draft")
        await server.remote("new shared")
        state.observe(try await server.scratchpad(session: "child"))
        #expect(state.draft == "my draft")
        #expect(state.status == .conflict)
        state.review()
        #expect(state.reviewText.contains("-new shared"))
        #expect(state.reviewText.contains("+my draft"))
        state.keepEditing()
        #expect(state.status == .conflict)
        state.review()
        await state.replaceReviewed(client: server, session: "child")
        #expect(state.status == .saved)
        #expect(try await server.scratchpad(session: "child").content == "my draft")
    }

    @Test func staleReviewRefreshesAndFailedSaveKeepsDraft() async throws {
        let server = ScratchpadServer()
        let state = ScratchpadState(demo: false)
        await state.load(client: server, session: "child")
        state.edit("local")
        await server.remote("remote one")
        state.observe(try await server.scratchpad(session: "child"))
        state.review()
        await server.remote("remote two")
        await state.replaceReviewed(client: server, session: "child")
        #expect(state.status == .conflict)
        #expect(state.reviewText.contains("-remote two"))
        #expect(state.draft == "local")
        state.useShared()
        state.edit("retry me")
        await server.setFailure(true)
        #expect(await state.save(client: server, session: "child") == false)
        #expect(state.draft == "retry me")
        await server.setFailure(false)
        #expect(await state.save(client: server, session: "child"))
    }

    @Test func closeWaitsForSuccessfulSave() async throws {
        let server = ScratchpadServer()
        let workspace = WorkspaceState(demo: false)
        workspace.open(.scratchpad)
        await workspace.scratchpadState.load(client: server, session: "child")
        workspace.scratchpad = "keep this draft"
        await server.setFailure(true)
        await workspace.closeScratchpad(client: server, session: "child")
        #expect(workspace.panes.contains(.scratchpad))
        #expect(workspace.scratchpad == "keep this draft")
        await server.setFailure(false)
        await workspace.closeScratchpad(client: server, session: "child")
        #expect(!workspace.panes.contains(.scratchpad))
        #expect(try await server.scratchpad(session: "child").content == "keep this draft")
    }

    @Test func familySessionsShareAndReconcile() async throws {
        let server = ScratchpadServer()
        let parent = ScratchpadState(demo: false)
        let child = ScratchpadState(demo: false)
        await parent.load(client: server, session: "root")
        await child.load(client: server, session: "fork")
        parent.edit("parent notes")
        #expect(await parent.save(client: server, session: "root"))
        child.observe(try await server.scratchpad(session: "fork"))
        #expect(child.draft == "parent notes")
        child.edit("fork notes")
        parent.edit("parent draft")
        #expect(await child.save(client: server, session: "fork"))
        parent.observe(try await server.scratchpad(session: "root"))
        #expect(parent.status == .conflict)
        #expect(parent.draft == "parent draft")
    }

    @Test func conflictReviewRendersInScratchpadPane() async throws {
        let store = SessionStore(fixture: try Fixture.load())
        store.ui.workspace = WorkspaceState(demo: false)
        let pad = store.ui.workspace.scratchpadState
        pad.observe(ScratchpadRecord(owner: "root", content: "shared line", revision: 1, updatedAt: "2026-09-20T12:00:00Z"))
        pad.edit("local line")
        pad.observe(ScratchpadRecord(owner: "root", content: "new shared line", revision: 2, updatedAt: "2026-09-20T12:00:01Z"))
        pad.review()
        let theme = MicaTheme(dark: false)
        let host = NSHostingView(rootView: ScratchpadPane(state: store)
            .environment(\.mica, theme).environment(\.colorScheme, .light).background(theme.surface))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 760, height: 500),
                              styleMask: [.titled], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<5 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(30)) }
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        try #require(bitmap.representation(using: .png, properties: [:]))
            .write(to: URL(fileURLWithPath: "/tmp/kit-scratchpad-conflict.png"))
        #expect(pad.reviewText.contains("-new shared line"))
        #expect(pad.reviewText.contains("+local line"))
    }
}
