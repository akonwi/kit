import AppKit
import SwiftUI
import Testing
@testable import Kit

/// Feedback that moved out of the toast stack to the surface that owns it.
@MainActor struct ToastReplacementTests {
    private func render<V: View>(_ view: V, width: CGFloat) -> NSHostingView<some View> {
        _ = NSApplication.shared
        let host = NSHostingView(rootView: view.environment(\.mica, MicaTheme(dark: true)).frame(width: width))
        host.frame = NSRect(origin: .zero, size: host.fittingSize)
        host.layoutSubtreeIfNeeded()
        return host
    }

    // MARK: Activity card

    @Test func shellFailuresAndRunningCommandsTakeTheActivityCard() {
        #expect(ActivityLine.resolve(activity: "Reading", compaction: "Compacting session…",
                                     shellFailure: "Resolve the previous shell command before starting another.",
                                     shellRetryable: true, command: "deploy")
                == .shellFailed("Resolve the previous shell command before starting another.", retryable: true))
        // A send or stop failure is about the turn itself and stays first.
        #expect(ActivityLine.resolve(activity: nil, startFailure: "offline", shellFailure: "offline")
                == .failedToStart("offline"))
        #expect(ActivityLine.resolve(activity: "Reading", command: "deploy") == .runningCommand("deploy"))
        #expect(ActivityLine.resolve(activity: "Reading", compaction: "Compacting session…", command: "deploy")
                == .compacting("Compacting session…"))
        #expect(ActivityLine.resolve(activity: nil, command: nil) == nil)
    }

    @Test func activityCardKeepsItsHeightForShellFailuresAndCommands() {
        for line: ActivityLine in [.runningCommand("deploy"), .shellFailed("The network connection was lost.", retryable: false),
                                   .shellFailed(String(repeating: "very long failure ", count: 20), retryable: true)] {
            let host = render(PendingActivityView(line: line), width: 560)
            #expect(host.bounds.size == CGSize(width: 560, height: PendingActivityView.height))
        }
    }

    // MARK: Command palette

    @Test func paletteRefusesArgumentsThatCantBeSent() throws {
        let prompt = PaletteCommand(id: "p", name: "fix", description: "",
            prompt: PromptCommand(name: "fix", description: "", source: "project", location: "/p"))
        let plugin = try #require(PluginCommand.project([WirePluginCommand(id: "demo.echo", localId: "echo", pluginId: "demo",
            instance: "host:1", description: "Echo", argName: nil, category: nil)]).first)
        let pluginRow = PaletteCommand(id: "q", name: "demo.echo", description: "", plugin: plugin)
        #expect(prompt.argumentProblem("issue 123") == nil)
        #expect(prompt.argumentProblem("a\0b") == "Arguments can’t contain a NUL character.")
        #expect(prompt.argumentProblem(String(repeating: "x", count: 128 * 1024)) == nil)
        #expect(prompt.argumentProblem(String(repeating: "x", count: 128 * 1024 + 1)) == "Arguments must fit within 128 KiB.")
        #expect(pluginRow.argumentProblem(String(repeating: "x", count: 64 * 1024 + 1)) == "Arguments must fit within 64 KiB.")
        // Built-ins take their arguments as text in their own screens.
        #expect(PaletteCommand(id: "cd", name: "cd", description: "").argumentProblem("a\0b") == nil)
    }

    @Test func refusingARowAgainFlashesItAgain() {
        let first = PaletteRefusal.none.next("compact")
        let second = first.next("compact")
        #expect(first == PaletteRefusal(rowID: "compact", generation: 1))
        #expect(second.generation == 2 && second.rowID == "compact")
        #expect(second != first)
    }

    @Test func paletteProblemIsAStripUnderTheSearchField() {
        let short = render(PaletteProblem(message: "Arguments must fit within 64 KiB."), width: PaletteMetrics.width)
        #expect(short.bounds.size == CGSize(width: PaletteMetrics.width, height: PaletteMetrics.rowHeight),
                "\(short.bounds.size)")
        let long = render(PaletteProblem(message: String(repeating: "Arguments can’t be sent as typed. ", count: 6)),
                          width: PaletteMetrics.width)
        #expect(long.bounds.height > short.bounds.height)
        #expect(long.bounds.height < PaletteMetrics.rowHeight * 2)
    }

    // MARK: Transcript

    @Test func turnEndRowsNameHowTheTurnEndedAndWrapTheReason() {
        #expect(TranscriptTurnEndRow(end: .failed, message: "x").title == "Turn failed")
        #expect(TranscriptTurnEndRow(end: .stopped, message: "x").title == "Turn stopped")
        #expect(TranscriptTurnEndRow(end: .interrupted, message: "x").title == "Turn interrupted")
        let short = render(TranscriptTurnEndRow(end: .failed, message: "Provider unavailable"), width: 600)
        let long = render(TranscriptTurnEndRow(end: .failed, message: String(repeating: "rate limit exceeded; ", count: 20)),
                          width: 600)
        #expect(short.bounds.width == 600)
        // The label line, then one line of reason.
        #expect(short.bounds.height >= 40 && short.bounds.height < 56)
        #expect(long.bounds.height > short.bounds.height + 14)
    }

    // MARK: Terminal notifications

    @Test func terminalNotificationsToastOnlyWhenTheTerminalIsOutOfSight() throws {
        let workspace = WorkspaceState()
        let feedback = SessionFeedback()
        workspace.feedback = feedback
        var active = true
        workspace.appIsActive = { active }
        let build = workspace.openTerminal(directory: "/tmp")
        #expect(workspace.isVisible(.terminal(build.id)))
        build.onRequest?(.notification(title: "Build finished", body: "0 errors"))
        #expect(feedback.items.isEmpty)
        active = false
        build.onRequest?(.notification(title: "Build finished", body: "0 errors"))
        #expect(feedback.items.map(\.title) == ["Build finished"])
        #expect(!build.bell)

        let other = workspace.openTerminal(directory: "/tmp")
        #expect(!workspace.isVisible(.terminal(build.id)))
        active = true
        build.onRequest?(.notification(title: "Tests passed", body: ""))
        let toast = try #require(feedback.visible)
        #expect(toast.title == "Tests passed")
        #expect(toast.actionTitle == "Show")
        #expect(build.bell)
        toast.action?()
        #expect(workspace.isVisible(.terminal(build.id)))
        _ = other
    }
}
