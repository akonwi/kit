import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor struct SessionFeedbackTests {
    @Test func ephemeralFeedbackStacksAndExpiresByIdentity() throws {
        let state = SessionFeedback()
        state.show(title: "Session compacted")
        let first = try #require(state.visible)
        state.show(title: "File opened")
        let latest = try #require(state.visible)
        #expect(state.items.map(\.title) == ["Session compacted", "File opened"])
        state.expire(first.id)
        #expect(state.visible?.title == "File opened")
        #expect(state.items.count == 1)
        state.expire(latest.id)
        #expect(state.items.isEmpty)
    }

    @Test func remainingEphemeralTimeSurvivesSessionViewChanges() throws {
        let state = SessionFeedback()
        state.show(title: "Updated model catalog")
        let item = try #require(state.visible)
        state.saveRemainingLifetime(3.5, for: item.id)
        #expect(state.remainingLifetime(for: item.id) == 3.5)
        state.expire(item.id)
        #expect(state.remainingLifetime(for: item.id) == 10)
    }

    @Test func activePersistentFeedbackSurvivesDedupeHistoryRollover() {
        let state = SessionFeedback()
        state.show(key: "diagnostic", title: "Subagent definition warning", persistent: true)
        for index in 0..<520 { state.show(key: "event-\(index)", title: "Event \(index)") }
        state.show(key: "diagnostic", title: "Subagent definition warning", persistent: true)
        #expect(state.notices.map(\.title) == ["Subagent definition warning"])
    }

    @Test func acknowledgedFeedbackDoesNotReplayAfterDedupeRollover() throws {
        let state = SessionFeedback()
        state.show(key: "failure", title: "Turn failed", persistent: true)
        state.dismiss(try #require(state.visible?.id))
        for index in 0..<520 { state.show(key: "event-\(index)", title: "Event \(index)") }
        state.show(key: "failure", title: "Turn failed", persistent: true)
        #expect(state.notices.isEmpty)
        state.clear(key: "failure") // An explicitly resolved operation can report a new failure.
        state.show(key: "failure", title: "Turn failed again", persistent: true)
        #expect(state.notices.map(\.title) == ["Turn failed again"])
    }

    @Test func groupingDoesNotEvictPersistentFeedback() {
        let state = SessionFeedback()
        for index in 0..<6 { state.show(key: "persistent-\(index)", title: "Warning \(index)", persistent: true) }
        for index in 0..<12 { state.show(title: "Confirmation \(index)") }
        #expect(state.notices.count == 6)
        #expect(state.items.filter { !$0.persistent }.count == 10)
        #expect(state.items.count == 16)
        #expect(state.items.last?.title == "Confirmation 11")
    }

    @Test func toastStackGrowsThenFoldsAtFive() {
        _ = NSApplication.shared
        func size(for count: Int) -> NSSize {
            let state = SessionFeedback()
            for index in 0..<count { state.show(title: "Warning \(index)", detail: "Needs attention", tone: .warning, persistent: true) }
            let host = NSHostingView(rootView: SessionFeedbackView(feedback: state)
                .environment(\.mica, MicaTheme(dark: true)))
            return host.fittingSize
        }
        let single = size(for: 1), vertical = size(for: 3), folded = size(for: 5)
        #expect(single.width == 360)
        #expect(vertical.height > single.height * 2)
        #expect(folded.height < vertical.height)
    }

    @Test func toastSurfaceUsesInjectedThemeEvenWhenSystemAppearanceDiffers() throws {
        _ = NSApplication.shared
        func background(dark: Bool) throws -> NSColor {
            let state = SessionFeedback()
            state.show(title: "Catalog refreshed", detail: "83 models available")
            let host = NSHostingView(rootView: SessionFeedbackView(feedback: state)
                .environment(\.mica, MicaTheme(dark: dark))
                .environment(\.colorScheme, dark ? .light : .dark)
                .frame(width: 360, height: 120, alignment: .topTrailing))
            host.frame = NSRect(x: 0, y: 0, width: 360, height: 120)
            host.layoutSubtreeIfNeeded()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            return try #require(bitmap.colorAt(x: 320, y: 30)?.usingColorSpace(.deviceRGB))
        }
        let dark = try background(dark: true), light = try background(dark: false)
        #expect(dark.redComponent < 0.4)
        #expect(light.redComponent > 0.8)
    }

    @Test func acknowledgingPersistentToastPreventsReplay() throws {
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

    @Test func reloadEvidenceExpandsOnlyItsToastAndKeepsOtherCardsCompact() throws {
        _ = NSApplication.shared
        let state = SessionFeedback()
        state.show(title: "Another notice", detail: "Unrelated", persistent: true)
        let report = SessionReloadReport(
            diagnostics: [.init(message: "Skill ‘vaxis-ui’ was already loaded and omitted.",
                                sourcePath: "/Users/akonwi/kit/skills/vaxis-ui/SKILL.md")],
            sources: [.init(id: "vaxis-ui", path: "/Users/akonwi/kit/skills/vaxis-ui/SKILL.md")],
            warningCount: 1, refreshFailure: nil)
        state.show(title: "Session context reloaded with issues", detail: report.preview,
                   tone: .warning, persistent: true, reloadReport: report)
        let item = try #require(state.visible)
        #expect(item.detail == "Skill ‘vaxis-ui’ was already loaded and omitted.")
        #expect(item.reloadReport?.diagnostics.first?.sourcePath == "/Users/akonwi/kit/skills/vaxis-ui/SKILL.md")
        let compact = NSHostingView(rootView: SessionFeedbackCard(item: item, close: {})
            .environment(\.mica, MicaTheme(dark: true)).frame(width: 356))
        let expanded = NSHostingView(rootView: SessionFeedbackCard(item: item, close: {}, reportExpanded: true)
            .environment(\.mica, MicaTheme(dark: true)).frame(width: 564))
        #expect(compact.fittingSize.width == 356)
        #expect(expanded.fittingSize.width == 564)
        #expect(expanded.fittingSize.height > compact.fittingSize.height)
        #expect(expanded.fittingSize.height < 360) // Ordinary evidence has no bounded scroll pane.
        let closedSources = NSHostingView(rootView: SessionReloadEvidenceView(report: report, outerScrollable: false)
            .environment(\.mica, MicaTheme(dark: true)).frame(width: 500))
        let openedSources = NSHostingView(rootView: SessionReloadEvidenceView(
            report: report, outerScrollable: false, sourcesInitiallyExpanded: true)
            .environment(\.mica, MicaTheme(dark: true)).frame(width: 500))
        #expect(openedSources.fittingSize.height > closedSources.fittingSize.height + 20)
        #expect(openedSources.fittingSize.height < 360)
        openedSources.frame = NSRect(origin: .zero, size: openedSources.fittingSize)
        openedSources.layoutSubtreeIfNeeded()
        let sourcesBitmap = try #require(openedSources.bitmapImageRepForCachingDisplay(in: openedSources.bounds))
        openedSources.cacheDisplay(in: openedSources.bounds, to: sourcesBitmap)
        try #require(sourcesBitmap.representation(using: .png, properties: [:]))
            .write(to: URL(fileURLWithPath: "/tmp/kit-reload-sources-open.png"))
        expanded.frame = NSRect(origin: .zero, size: expanded.fittingSize)
        expanded.layoutSubtreeIfNeeded()
        let bitmap = try #require(expanded.bitmapImageRepForCachingDisplay(in: expanded.bounds))
        expanded.cacheDisplay(in: expanded.bounds, to: bitmap)
        try #require(bitmap.representation(using: .png, properties: [:]))
            .write(to: URL(fileURLWithPath: "/tmp/kit-reload-toast-rendered.png"))
    }

    @Test func cardRendersWithReadableGeometryInBothAppearances() throws {
        _ = NSApplication.shared
        for dark in [false, true] {
            let state = SessionFeedback()
            state.show(title: "Subagent definition warning", detail: "The reviewer definition couldn’t be loaded.", tone: .warning, persistent: true)
            let item = try #require(state.visible)
            let host = NSHostingView(rootView: SessionFeedbackCard(item: item, close: {})
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
