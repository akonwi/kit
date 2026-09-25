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
        }.background(theme.surface)
    }

    private var transcript: some View {
        NativeTranscript(messages: state.messages,
                         hasHistory: state.hasEarlierHistory, historyLoading: state.historyLoading,
                         historyError: state.historyError, active: state.selected?.activity != nil,
                         presentation: state.ui.transcript, workspace: state.ui.workspace,
                         resumeRequest: resumeRequest, latestOutOfView: $latestOutOfView,
                         loadHistory: { state.loadHistory() }, theme: theme, attachmentClient: state.attachmentClient, attachmentSession: state.selectedID, reading: reading)
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

struct SessionComposerRegion: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    var body: some View {
        VStack(spacing: 0) {
            if state.interactions.isEmpty {
                PendingActivityView(activity: state.unavailable ? nil : state.selected?.activity)
                    .frame(maxWidth: 780).padding(.horizontal, 32)
                    .frame(maxWidth: .infinity)
            }
            SessionFeedbackView(feedback: state.feedback)
            if let flow = state.interactions.first {
                InteractionCard(flow: flow, pendingCount: state.interactions.count) { state.finishInteraction(cancelled: $0) }
                    .id(flow.id).frame(maxWidth: .infinity)
            } else {
                ComposerView(state: state).frame(maxWidth: 844).frame(maxWidth: .infinity)
            }
            if !state.isDemo && !state.unavailable && state.connectionState != .connected {
                HStack {
                    Text(state.connectionStatus).foregroundStyle(theme.muted)
                    Spacer()
                    Button("Reconnect") { state.attach() }.buttonStyle(MicaButtonStyle(compact: true))
                }.font(.kit(size: 12)).padding(.horizontal, 28).padding(.vertical, 12)
            }
            SessionFooter(state: state).padding(.top, 12)
        }.background(theme.surface)
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
