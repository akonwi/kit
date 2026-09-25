import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor struct NativeTranscriptTests {
    private final class FlippedDocument: NSView {
        override var isFlipped: Bool { true }
    }

    @Test func transcriptColumnRemainsCenteredAcrossResizes() async throws {
        let input = NativeTranscript(messages: [
            TranscriptMessage(id: "message", role: "user", text: "Identical transcript content", tools: [])
        ], hasHistory: false, historyLoading: false, historyError: nil, active: false,
           presentation: TranscriptPresentationState(), workspace: WorkspaceState(demo: false),
           resumeRequest: 0, latestOutOfView: .constant(false), loadHistory: {}, theme: MicaTheme(dark: false))
        let adapter = NativeTranscriptCoordinator(input)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 700),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = adapter.scroll
        window.orderFront(nil)
        defer { adapter.stop(); window.close() }
        adapter.receive(input)
        for width: CGFloat in [900, 1700, 600, 1700] {
            window.setContentSize(NSSize(width: width, height: 700))
            try await Task.sleep(for: .milliseconds(200))
            adapter.scroll.layoutSubtreeIfNeeded()
            adapter.table.layoutSubtreeIfNeeded()
            let cell = try #require(adapter.table.view(atColumn: 0, row: 0, makeIfNecessary: true) as? NativeTranscriptCell)
            let frame = cell.convert(cell.bounds, to: adapter.scroll.contentView)
            #expect(abs(frame.midX - adapter.scroll.contentView.bounds.midX) < 1,
                    "Cell \(frame), viewport \(adapter.scroll.contentView.bounds), table \(adapter.table.frame)")
        }
    }

    @Test func progressiveGroupsResizeNativeRowsAndRespectManualExpansion() async throws {
        let presentation = TranscriptPresentationState()
        let workspace = WorkspaceState(demo: false)
        var count = 5
        var active = true
        func input() -> NativeTranscript {
            let tools = (0..<count).map {
                ToolActivity(id: "call-\($0)", name: "read", summary: "file\($0).go", output: "contents", failed: $0 == 0, status: "Completed")
            }
            return NativeTranscript(messages: [TranscriptMessage(id: "group", role: "tools", text: "", tools: tools)],
                hasHistory: false, historyLoading: false, historyError: nil, active: active,
                presentation: presentation, workspace: workspace, resumeRequest: 0,
                latestOutOfView: .constant(false), loadHistory: {}, theme: MicaTheme(dark: false))
        }
        let adapter = NativeTranscriptCoordinator(input())
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 700),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = adapter.scroll
        window.orderFront(nil)
        defer { adapter.stop(); window.close() }
        func height(matching predicate: (CGFloat) -> Bool) async throws -> CGFloat {
            let deadline = ContinuousClock.now.advanced(by: .seconds(2))
            while ContinuousClock.now < deadline {
                let value = adapter.table.rect(ofRow: 0).height
                if predicate(value) { return value }
                try await Task.sleep(for: .milliseconds(20))
            }
            let value = adapter.table.rect(ofRow: 0).height
            #expect(predicate(value), "Unexpected tool group height: \(value)")
            return value
        }
        adapter.receive(input())
        let openHeight = try await height { $0 > 150 }
        count = 6
        adapter.receive(input())
        let closedHeight = try await height { $0 < 80 }
        #expect(openHeight > closedHeight)
        #expect(adapter.distanceFromBottom < 1)
        presentation.drawer(for: "group").expanded = true
        _ = try await height { $0 > openHeight }
        count = 7
        adapter.receive(input())
        _ = try await height { $0 > openHeight }
        active = false
        adapter.receive(input())
        #expect(presentation.drawer(for: "group").isExpanded(count: count, inProgress: active))
        presentation.drawer(for: "group").expanded = nil
        _ = try await height { $0 < 80 }
        count = 3; active = true
        adapter.receive(input())
        _ = try await height { $0 > 100 }
        active = false
        adapter.receive(input())
        _ = try await height { $0 < 80 }
    }

    @Test func longResponseStartsAtBeginningAndNavigatesRenderedHeadings() async throws {
        let reading = TranscriptReadingState()
        let presentation = TranscriptPresentationState()
        let workspace = WorkspaceState(demo: false)
        var rows = [TranscriptMessage(id: "question", role: "user", text: "Explain the design", tools: [])]
        var active = true
        func input(resume: Int = 0) -> NativeTranscript {
            NativeTranscript(messages: rows, hasHistory: false, historyLoading: false,
                historyError: nil, active: active, presentation: presentation, workspace: workspace,
                resumeRequest: resume, latestOutOfView: .constant(false), loadHistory: {},
                theme: MicaTheme(dark: false), reading: reading)
        }
        let adapter = NativeTranscriptCoordinator(input())
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 400),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = adapter.scroll
        window.orderFront(nil)
        defer { adapter.stop(); window.close() }
        func settle(_ predicate: () -> Bool) async throws {
            let deadline = ContinuousClock.now.advanced(by: .seconds(3))
            while !predicate(), ContinuousClock.now < deadline {
                try await Task.sleep(for: .milliseconds(20))
            }
            #expect(predicate())
        }
        adapter.receive(input())
        try await settle { adapter.table.rect(ofRow: 0).height != 90 }
        rows.append(TranscriptMessage(id: "answer", role: "assistant", text:
            "## Overview\n\n" + String(repeating: "A paragraph explaining the design.\n\n", count: 15)
            + "## Details\n\n" + String(repeating: "Implementation detail.\n\n", count: 15), tools: []))
        active = false
        adapter.receive(input())
        try await settle { reading.location?.sections.count == 2 && adapter.anchor()?.id == "message:answer" }
        #expect(abs(adapter.anchor()?.inset ?? -100) < 1)
        #expect(reading.location?.sections.map(\.title) == ["Overview", "Details"])
        let details = try #require(reading.location?.sections.last)
        reading.navigate?(details.id)
        try await settle { reading.location?.selected == 1 }
        #expect(abs((adapter.anchor()?.inset ?? 0) - details.offset - 12) < 1)
        adapter.receive(input(resume: 1))
        try await settle { adapter.distanceFromBottom < 1 }

        // A response arriving while the reader is above the bottom leaves the
        // same message and intra-message position visible.
        reading.navigate?(try #require(reading.location?.sections.first?.id))
        let before = try #require(adapter.anchor())
        active = true
        adapter.receive(input(resume: 1))
        rows.append(TranscriptMessage(id: "second", role: "assistant", text: rows[1].text, tools: []))
        active = false
        adapter.receive(input(resume: 1))
        try await Task.sleep(for: .milliseconds(200))
        #expect(adapter.anchor()?.id == before.id)
        #expect(abs((adapter.anchor()?.inset ?? 0) - before.inset) < 1)

        // Opening a snapshot containing the same long answer starts at Latest.
        let restored = NativeTranscriptCoordinator(input())
        defer { restored.stop() }
        window.contentView = restored.scroll
        restored.receive(input())
        try await settle { restored.table.rect(ofRow: rows.count - 1).height > 400 }
        #expect(restored.distanceFromBottom < 1)
        active = true
        restored.receive(input())
        rows.append(TranscriptMessage(id: "short", role: "assistant", text: "Done.", tools: []))
        active = false
        restored.receive(input())
        try await settle { restored.table.rect(ofRow: rows.count - 1).height < 90 }
        #expect(restored.distanceFromBottom < 1)
    }

    @Test func nestedWheelScrollingChainsAtBoundaries() {
        let outer = NativeTranscriptScrollView(frame: NSRect(x: 0, y: 0, width: 500, height: 400))
        let body = FlippedDocument(frame: NSRect(x: 0, y: 0, width: 500, height: 1200))
        outer.documentView = body
        let inner = NSScrollView(frame: NSRect(x: 30, y: 30, width: 350, height: 180))
        body.addSubview(inner)
        let output = FlippedDocument(frame: NSRect(x: 0, y: 0, width: 700, height: 600))
        inner.documentView = output
        inner.layoutSubtreeIfNeeded()
        #expect(outer.verticalRecipient(from: output, delta: 10) === outer)
        #expect(outer.verticalRecipient(from: output, delta: -10) === inner)
        inner.contentView.scroll(to: NSPoint(x: 0, y: 100))
        #expect(outer.verticalRecipient(from: output, delta: 10) === inner)
        #expect(outer.verticalRecipient(from: output, delta: -10) === inner)
        inner.contentView.scroll(to: NSPoint(x: 0, y: output.bounds.height - inner.contentSize.height))
        #expect(outer.verticalRecipient(from: output, delta: -10) === outer)
        output.frame.size.height = inner.contentSize.height
        inner.contentView.scroll(to: .zero)
        #expect(outer.verticalRecipient(from: output, delta: 10) === outer)
        #expect(outer.verticalRecipient(from: output, delta: -10) === outer)
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_NATIVE_TRANSCRIPT_TEST"] == "1"))
    func productionAdapterPreservesReadingAndFollowsGrowth() async throws {
        let presentation = TranscriptPresentationState()
        let workspace = WorkspaceState(demo: false)
        var outOfView = false
        var historyRequests = 0
        func messages(_ range: Range<Int>) -> [TranscriptMessage] {
            range.map { TranscriptMessage(id: "m\($0)", role: "assistant",
                text: "### Message \($0)\n\n" + String(repeating: "Wrapping **markdown** prose for the transcript.\n\n", count: $0 % 4 + 1), tools: []) }
        }
        var rows = messages(20..<1020)
        func input(resume: Int = 0) -> NativeTranscript {
            NativeTranscript(messages: rows, hasHistory: true, historyLoading: false,
                historyError: nil, active: true, presentation: presentation, workspace: workspace,
                resumeRequest: resume, latestOutOfView: Binding(get: { outOfView }, set: { outOfView = $0 }),
                loadHistory: { historyRequests += 1 }, theme: MicaTheme(dark: false))
        }
        let adapter = NativeTranscriptCoordinator(input())
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 700),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = adapter.scroll
        window.orderFront(nil)
        defer { adapter.stop(); window.close() }
        func settle() async throws { try await Task.sleep(for: .milliseconds(250)) }
        adapter.receive(input())
        try await settle()
        #expect(adapter.distanceFromBottom < 1)
        #expect(outOfView == false)
        for _ in 0..<3 {
            rows[rows.count - 1].text += "\n\nNew streamed paragraph."
            adapter.receive(input())
            try await settle()
            #expect(adapter.distanceFromBottom < 1)
        }
        // Repeated wheel input must continue moving while row measurements and
        // otherwise unchanged SwiftUI updates arrive between events.
        var progress = 0
        let upward = try #require(CGEvent(scrollWheelEvent2Source: nil, units: .pixel,
            wheelCount: 1, wheel1: 40, wheel2: 0, wheel3: 0))
        for _ in 0..<30 {
            let beforeWheel = try #require(adapter.anchor())
            adapter.scroll.scrollWheel(with: try #require(NSEvent(cgEvent: upward)))
            adapter.receive(input())
            try await Task.sleep(for: .milliseconds(35))
            let afterWheel = try #require(adapter.anchor())
            if afterWheel.id != beforeWheel.id || afterWheel.inset < beforeWheel.inset - 0.5 { progress += 1 }
        }
        #expect(progress >= 25)
        #expect(adapter.distanceFromBottom > 8)

        adapter.scroll.contentView.scroll(to: NSPoint(x: 0, y: 150))
        adapter.scroll.reflectScrolledClipView(adapter.scroll.contentView)
        let wheel = try #require(CGEvent(scrollWheelEvent2Source: nil, units: .pixel,
            wheelCount: 1, wheel1: 1, wheel2: 0, wheel3: 0))
        adapter.scroll.scrollWheel(with: try #require(NSEvent(cgEvent: wheel)))
        try await settle()
        #expect(historyRequests > 0)
        #expect(outOfView)
        let before = try #require(adapter.anchor())
        rows.insert(contentsOf: messages(0..<20), at: 0)
        adapter.receive(input())
        try await settle()
        let after = try #require(adapter.anchor())
        #expect(after.id == before.id)
        #expect(abs(after.inset - before.inset) < 1)
        // At the absolute top, the loading header must not become the anchor.
        adapter.scroll.contentView.scroll(to: .zero)
        adapter.scroll.reflectScrolledClipView(adapter.scroll.contentView)
        adapter.scroll.scrollWheel(with: try #require(NSEvent(cgEvent: upward)))
        try await settle()
        let atTop = try #require(adapter.anchor())
        #expect(atTop.id == "message:m0")
        let older = (1...20).reversed().map {
            TranscriptMessage(id: "older-\($0)", role: "assistant", text: "Earlier message \($0)", tools: [])
        }
        rows.insert(contentsOf: older, at: 0)
        adapter.receive(input())
        try await settle()
        let afterTopPage = try #require(adapter.anchor())
        let oldFirstRow = 21
        let oldFirstInset = adapter.scroll.contentView.bounds.minY - adapter.table.rect(ofRow: oldFirstRow).minY
        #expect(abs(oldFirstInset - atTop.inset) < 1)
        #expect(adapter.scroll.contentView.bounds.minY > 240)
        let reading = afterTopPage
        rows[rows.count - 1].text += "\n\nOffscreen update."
        adapter.receive(input())
        try await settle()
        #expect(adapter.anchor()?.id == reading.id)
        #expect(abs((adapter.anchor()?.inset ?? 0) - reading.inset) < 1)
        window.setContentSize(NSSize(width: 450, height: 700))
        try await settle()
        #expect(adapter.anchor()?.id == reading.id)
        adapter.receive(input(resume: 1))
        try await settle()
        #expect(adapter.distanceFromBottom < 1)
        #expect(outOfView == false)
        func cells(_ view: NSView) -> Int {
            (view is NativeTranscriptCell ? 1 : 0) + view.subviews.reduce(0) { $0 + cells($1) }
        }
        #expect(cells(adapter.table) < 40)
        let tools = (0..<40).map {
            ToolActivity(id: "tool-\($0)", name: "bash", summary: "go test ./...",
                output: String(repeating: "ok package tests passed\n", count: 30), failed: false)
        }
        rows.append(TranscriptMessage(id: "drawer", role: "tools", text: "", tools: tools))
        adapter.receive(input(resume: 1))
        try await settle()
        let drawerRow = rows.count
        let collapsedHeight = adapter.table.rect(ofRow: drawerRow).height
        let drawer = presentation.drawer(for: "drawer")
        drawer.expanded = true
        var previousHeight = collapsedHeight
        for _ in 0..<15 {
            try await Task.sleep(for: .milliseconds(16))
            let height = adapter.table.rect(ofRow: drawerRow).height
            #expect(height >= previousHeight)
            previousHeight = height
        }
        #expect(previousHeight > collapsedHeight)
        #expect(adapter.distanceFromBottom < 1)
        drawer.selectedTool = tools.last?.id
        try await settle()
        #expect(adapter.table.rect(ofRow: drawerRow).height > previousHeight)
        #expect(adapter.distanceFromBottom < 1)
        let cell = try #require(adapter.table.view(atColumn: 0, row: drawerRow, makeIfNecessary: false) as? NativeTranscriptCell)
        #expect(cell.layer?.masksToBounds == true)
        drawer.expanded = false
        try await settle()
        #expect(adapter.table.rect(ofRow: drawerRow).height == collapsedHeight)
        #expect(adapter.distanceFromBottom < 1)
        drawer.expanded = true
        try await settle()
        #expect(adapter.distanceFromBottom < 1)
        let bitmap = try #require(adapter.scroll.bitmapImageRepForCachingDisplay(in: adapter.scroll.bounds))
        adapter.scroll.cacheDisplay(in: adapter.scroll.bounds, to: bitmap)
        let png = try #require(bitmap.representation(using: .png, properties: [:]))
        try png.write(to: URL(fileURLWithPath: "/tmp/kit-native-transcript.png"))
    }
}
