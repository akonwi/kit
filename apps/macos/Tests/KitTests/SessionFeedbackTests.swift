import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor struct SessionFeedbackTests {
    @Test func ephemeralFeedbackIsReplacedAndExpiresByIdentity() throws {
        let state = SessionFeedback()
        state.show(title: "Session compacted")
        let first = try #require(state.visible)
        state.show(title: "File opened")
        let latest = try #require(state.visible)
        state.expire(first.id)
        #expect(state.visible?.title == "File opened")
        #expect(state.items.count == 1)
        state.expire(latest.id)
        #expect(state.items.isEmpty)
    }

    @Test func dismissingCardAlsoClearsFooterNoticeAndPreventsReplay() throws {
        let state = SessionFeedback()
        state.show(key: "definition", title: "Subagent definition warning", detail: "Missing model", tone: .warning, persistent: true)
        let item = try #require(state.visible)
        state.expire(item.id)
        #expect(state.visible?.title == item.title)
        #expect(state.notices.map(\.title) == ["Subagent definition warning"])
        state.dismiss(item.id)
        #expect(state.visible == nil)
        #expect(state.notices.count == 0)
        state.show(key: "definition", title: item.title, persistent: true)
        #expect(state.items.isEmpty)
    }

    @Test func sessionDiagnosticsDeduplicateAcrossSnapshots() {
        let state = SessionFeedback()
        var session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "Test", model: "m", thinking: "high", workspace: "w", date: "", messages: [])
        session.subagentDiagnostics = [.init(severity: "warning", code: "invalid", message: "Missing model",
            source: .init(kind: "file", path: "/workspace/reviewer.md", pluginId: nil))]
        state.observe(session); state.observe(session)
        #expect(state.notices.map(\.detail) == ["Missing model\n/workspace/reviewer.md"])
        let otherSession = SessionFeedback()
        otherSession.observe(session)
        #expect(otherSession.notices.count == 1)
    }

    @Test func resolvedOperationsCanReportAnotherOutcome() {
        let state = SessionFeedback()
        state.show(key: "compaction", title: "Compaction failed", persistent: true)
        state.clear(key: "compaction")
        state.show(key: "compaction", title: "Session compacted")
        #expect(state.items.map(\.title) == ["Session compacted"])
        #expect(state.notices.isEmpty)
    }

    @Test func liveCompactionFeedbackIsDeduplicatedAndManualCommandsOwnTheirResult() {
        let state = SessionFeedback()
        var session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "Test", model: "m", thinking: "high", workspace: "w", date: "", messages: [])
        session.compactionOutcome = CompactionOutcome(id: "auto", failed: false, detail: "")
        state.observe(session); state.observe(session)
        #expect(state.items.map(\.title) == ["Session compacted"])
        session.compactionOutcome = CompactionOutcome(id: "manual", failed: false, detail: "")
        state.observe(session, manualCompaction: true)
        state.observe(session)
        #expect(state.items.map(\.title) == ["Session compacted"])
        session.compactionOutcome = CompactionOutcome(id: "failed", failed: true, detail: "Provider unavailable")
        state.observe(session)
        #expect(state.notices.map(\.detail) == ["Provider unavailable"])
    }

    @Test func cardRendersWithReadableGeometryInBothAppearances() throws {
        _ = NSApplication.shared
        for dark in [false, true] {
            let state = SessionFeedback()
            state.show(title: "Subagent definition warning", detail: "The reviewer definition couldn’t be loaded.", tone: .warning, persistent: true)
            let item = try #require(state.visible)
            let host = NSHostingView(rootView: SessionFeedbackCard(item: item, close: {}, expire: {})
                .environment(\.mica, MicaTheme(dark: dark)).environment(\.colorScheme, dark ? .dark : .light)
                .frame(width: 600))
            host.frame = NSRect(origin: .zero, size: host.fittingSize)
            host.layoutSubtreeIfNeeded()
            #expect(host.bounds.width == 600)
            #expect(host.bounds.height >= 80 && host.bounds.height < 160)
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-feedback-\(dark ? "dark" : "light").png"))
        }
    }
}
