import SwiftUI

struct SessionView: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    @State private var latestOutOfView = false
    @State private var resumeRequest = 0
    @State private var reading = TranscriptReadingState()
    @State private var height: CGFloat = .infinity

    var body: some View {
        VStack(spacing: 0) {
            TranscriptReadingNavigation(state: reading)
            transcript
            // The interaction dock grows to half the pane, then scrolls.
            SessionComposerRegion(state: state, dockHeight: height / 2)
        }
        .background(theme.surface)
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
    }

    private var transcript: some View {
        NativeTranscript(messages: state.messages,
                         hasHistory: state.hasEarlierHistory, historyLoading: state.historyLoading,
                         historyError: state.historyError, active: state.activity != nil,
                         presentation: state.ui.transcript, workspace: state.ui.workspace,
                         resumeRequest: resumeRequest, latestOutOfView: $latestOutOfView,
                         loadHistory: { state.loadHistory() }, theme: theme, attachmentClient: state.attachmentClient, attachmentSession: state.selectedID, reading: reading,
                         hasLoadedHistory: state.hasLoadedHistory, releaseHistory: { state.releaseLoadedHistory() },
                         historyReleaseDelay: state.historyReleaseDelay)
            .id(state.selectedID)
            .onChange(of: state.selectedID) { latestOutOfView = false }
            .overlay(alignment: .bottomTrailing) {
                if latestOutOfView {
                    Button("Latest", icon: .arrowDown) { resumeRequest += 1 }
                        .buttonStyle(MicaButtonStyle(compact: true)).padding(16)
                }
            }
    }
}

/// The Agent pane's composer, activity card, queued tray, and interaction
/// dock. Everything here addresses the parent session's conversation.
struct SessionComposerRegion: View {
    @Environment(\.mica) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Bindable var state: SessionStore
    /// The tallest the interaction dock may grow.
    var dockHeight: CGFloat = .infinity
    var body: some View {
        let docked = state.dockedInteraction
        VStack(spacing: 8) {
            if docked == nil {
                let line = state.unavailable ? nil : ActivityLine.resolve(
                    activity: state.activity,
                    compaction: state.compactionOperation.pending ? state.compactionOperation.title : nil,
                    retryAt: state.selected?.providerRetryAt,
                    retryAttempt: state.selected?.providerRetryCount,
                    startFailure: state.operations.startFailure,
                    stopFailure: state.operations.abortError,
                    notice: state.notice)
                PendingActivityView(line: line) {
                    if case .failedToStop = line { state.abortRun() } else { state.send() }
                }
            }
            if let flow = docked {
                queuedTray(joined: false)
                let workspace = state.ui.workspace
                // One dock for consecutive requests: the next one swaps in
                // place instead of remounting.
                InteractionDock(flow: flow, pendingCount: max(1, state.interactions.count),
                                takesFocus: workspace.tile(containing: .conversation) == workspace.focusedTile,
                                maxHeight: dockHeight) { state.finishInteraction(cancelled: $0) }
                    .frame(maxWidth: .infinity)
                    .transition(reduceMotion ? .opacity
                                : .asymmetric(insertion: .opacity.combined(with: .offset(y: 10)), removal: .opacity))
            } else {
                // The tray and composer share a border line.
                VStack(spacing: -1) {
                    queuedTray(joined: true)
                    ComposerView(state: state, joinedTop: showsQueue)
                }
                .transition(.opacity)
            }
            if !state.isDemo && !state.unavailable && state.connectionState != .connected {
                HStack {
                    Text(state.connectionStatus).foregroundStyle(theme.muted)
                    Spacer()
                    Button("Reconnect") { state.attach() }.buttonStyle(MicaButtonStyle(compact: true))
                }.font(.kit(size: 12)).padding(.horizontal, 4)
            }
        }
        .frame(maxWidth: 844)
        .padding([.horizontal, .bottom], 14)
        .frame(maxWidth: .infinity)
        .background(theme.surface)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: docked == nil)
    }

    private var showsQueue: Bool { !state.isDemo && (state.operations.queue?.count ?? 0) > 0 }

    @ViewBuilder private func queuedTray(joined: Bool) -> some View {
        if showsQueue, let queue = state.operations.queue {
            let operations = state.operations
            QueuedTray(queue: queue, error: operations.queueError,
                       enabled: !state.unavailable && !operations.queuePending && !operations.sending && !operations.uncertain,
                       running: state.running,
                       edit: { state.changeQueue(.edit) }, clear: { state.changeQueue(.remove) },
                       sendNow: { state.changeQueue(.promote) }, joined: joined)
        }
    }
}

/// The session's shell and queue monitors, which run whichever panes are shown.
struct SessionMonitors: View {
    @Bindable var state: SessionStore
    var body: some View {
        Color.clear
            .accessibilityHidden(true)
            .onChange(of: state.bashOperation.error, initial: true) { state.syncBashFeedback() }
            .onChange(of: state.bashOperation.pending) { state.syncBashFeedback() }
            .task(id: "bash/\(state.serverID)/\(state.selectedID)/\(state.unavailable)") {
                if !state.unavailable { await state.monitorBash() }
            }
            .task(id: "\(state.serverID)/\(state.selectedID)/\(state.unavailable)") {
                if !state.unavailable, let client = state.mutationClient { await state.operations.monitorQueue(client: client, session: state.selectedID) }
            }
    }
}
