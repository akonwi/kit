import SwiftUI

struct SessionView: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    @State private var latestOutOfView = false
    @State private var resumeRequest = 0
    @State private var reading = TranscriptReadingState()

    var body: some View {
        VStack(spacing: 0) {
            TranscriptReadingNavigation(state: reading)
            transcript
            SessionComposerRegion(state: state)
        }.background(theme.surface)
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
                    Button("Latest", systemImage: "arrow.down") { resumeRequest += 1 }
                        .buttonStyle(MicaButtonStyle(compact: true)).padding(16)
                }
            }
    }
}

/// The Agent pane's composer, activity card, queued tray, and interaction
/// dock. Everything here addresses the parent session's conversation.
struct SessionComposerRegion: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    var body: some View {
        VStack(spacing: 8) {
            if state.interactions.isEmpty {
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
            if let flow = state.interactions.first {
                queuedTray(joined: false)
                InteractionCard(flow: flow, pendingCount: state.interactions.count) { state.finishInteraction(cancelled: $0) }
                    .id(flow.id).frame(maxWidth: .infinity)
            } else {
                // The tray and composer share a border line.
                VStack(spacing: -1) {
                    queuedTray(joined: true)
                    ComposerView(state: state, joinedTop: showsQueue)
                }
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
