import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor struct ProgressiveToolGroupTests {
    private func tools(_ count: Int, failed: Set<Int> = [], running: Set<Int> = []) -> [ToolActivity] {
        (0..<count).map {
            ToolActivity(id: "call-\($0)", name: "read", summary: "file\($0).go", output: "contents",
                         failed: failed.contains($0), status: running.contains($0) ? "Running…" : "Completed")
        }
    }

    /// The rendered height of a batch: 4 points above and below, a 24-point
    /// header for a group, and a 28-point row per visible call, 2 points apart.
    private func height(_ tools: [ToolActivity], state: ToolDrawerState, inProgress: Bool = false) async throws -> CGFloat {
        let host = NSHostingView(rootView: ToolActivityView(tools: tools, workspace: WorkspaceState(demo: false),
                                                            state: state, inProgress: inProgress)
            .frame(width: 600).environment(\.mica, MicaTheme(dark: false)))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 600, height: 400),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        try await Task.sleep(for: .milliseconds(50))
        host.layoutSubtreeIfNeeded()
        return host.fittingSize.height
    }

    @Test func singleCallIsItsRowWithoutHeader() async throws {
        let state = ToolDrawerState()
        #expect(state.disclosure(count: 1) == .single)
        #expect(try await height(tools(1), state: state) == 4 + 28 + 4)
        #expect(try await height(tools(1, failed: [0]), state: state) == 4 + 28 + 4)
        #expect(try await height(tools(1, running: [0]), state: state, inProgress: true) == 4 + 28 + 4)
    }

    @Test func groupsThroughThresholdAreOpenLiveOrFinished() async throws {
        let state = ToolDrawerState()
        #expect((2...5).map { state.disclosure(count: $0) } == Array(repeating: .group(expanded: true), count: 4))
        #expect(try await height(tools(2), state: state) == 4 + 24 + 2 * (2 + 28) + 4)
        #expect(try await height(tools(5, running: [4]), state: state, inProgress: true) == 4 + 24 + 5 * (2 + 28) + 4)
    }

    @Test func groupsAboveThresholdAreCollapsed() async throws {
        let state = ToolDrawerState()
        #expect((6...9).map { state.disclosure(count: $0) } == Array(repeating: .group(expanded: false), count: 4))
        #expect(try await height(tools(6, failed: [0]), state: state) == 4 + 24 + 4)
        #expect(try await height(tools(6, running: [5]), state: state, inProgress: true) == 4 + 24 + 4)
    }

    @Test func explicitChoicesOverrideGroupDefaultsButNotSingleCalls() async throws {
        let state = ToolDrawerState()
        state.expanded = false
        #expect(state.disclosure(count: 3) == .group(expanded: false))
        #expect(try await height(tools(3), state: state) == 4 + 24 + 4)
        #expect(state.disclosure(count: 1) == .single)
        #expect(try await height(tools(1), state: state) == 4 + 28 + 4)
        state.expanded = true
        #expect(state.disclosure(count: 9) == .group(expanded: true))
        #expect(try await height(tools(6), state: state) == 4 + 24 + 6 * (2 + 28) + 4)
        state.expanded = nil
        #expect(state.disclosure(count: 6) == .group(expanded: false))
    }

    @Test func liveBatchGrowsIntoGroupAndCollapsesAboveThreshold() {
        let state = ToolDrawerState()
        #expect((1...7).map { state.disclosure(count: $0) }
                == [.single] + Array(repeating: .group(expanded: true), count: 4) + [.group(expanded: false), .group(expanded: false)])
        // Opening a live group before it passes the threshold keeps it open.
        state.expanded = true
        #expect((5...7).map { state.disclosure(count: $0) } == Array(repeating: .group(expanded: true), count: 3))
    }

    @Test func latestGroupStaysLiveBetweenCallsWithoutRevivingPreviousTurn() {
        let tool = ToolActivity(id: "call", name: "read", summary: "file.go", output: "contents", failed: false, status: "Completed")
        let first = TranscriptMessage(id: "first", role: "tools", text: "", tools: [tool])
        let prose = TranscriptMessage(id: "prose", role: "assistant", text: "Next step", tools: [])
        let second = TranscriptMessage(id: "second", role: "tools", text: "", tools: [tool])
        #expect(ToolGroupActivity.liveGroups(in: [first, prose], active: true) == ["first"])
        #expect(ToolGroupActivity.liveGroups(in: [first, prose, second], active: true) == ["second"])
        #expect(ToolGroupActivity.liveGroups(in: [first, prose, second], active: false) == [])
        let user = TranscriptMessage(id: "user", role: "user", text: "Next turn", tools: [])
        #expect(ToolGroupActivity.liveGroups(in: [first, prose, second, user], active: true) == [])
    }
}
