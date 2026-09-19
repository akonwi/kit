import SwiftUI

/// Session-scoped status and workspace location, matching the TUI footer roles.
struct SessionFooter: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore

    var body: some View {
        let compaction = state.compactionOperation
        let status = Self.status(approval: state.approval, replaying: state.replaying)
        VStack(spacing: 0) {
            Rule()
            HStack(spacing: 12) {
                if state.bashOperation.pending || state.activeBashID != nil {
                    KitSpinner()
                    Text(state.bashOperation.stopping ? "Stopping shell…" : state.bashOperation.pending ? "Starting shell…" : "Running shell…")
                } else if compaction.pending {
                    KitSpinner()
                    Text(compaction.title)
                } else if let retryAt = state.selected?.providerRetryAt {
                    TimelineView(.periodic(from: .now, by: 1)) { context in
                        Text("Provider retry in \(Self.secondsRemaining(retryAt, now: context.date))s")
                            .foregroundStyle(theme.warning)
                    }
                } else if let status {
                    Label(status.title, systemImage: status.symbol)
                        .foregroundStyle(state.approval ? theme.warning : theme.accent)
                        .fixedSize()
                }
                if !state.notice.isEmpty {
                    Text(state.notice)
                        .lineLimit(1).truncationMode(.tail)
                        .help(state.notice)
                }
                SessionNoticeButton(feedback: state.feedback)
                Spacer(minLength: 8)
                WorkspaceLocation(session: state.selected)
                    .layoutPriority(1)
            }
            .font(.kit(size: 12))
            .foregroundStyle(theme.muted)
            .padding(.horizontal, 20)
            .frame(minHeight: 34)
            .background(theme.raised)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session status")
        .help(state.selected?.sourceNotice ?? "Session status")
    }

    static func secondsRemaining(_ timestamp: String, now: Date) -> Int {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let date = formatter.date(from: timestamp) ?? ISO8601DateFormatter().date(from: timestamp)
        return max(0, Int(ceil((date ?? now).timeIntervalSince(now))))
    }

    static func status(approval: Bool, replaying: Bool) -> (title: String, symbol: String)? {
        if approval { return ("Awaiting response", "hand.raised") }
        if replaying { return ("Replaying", "play.circle") }
        return nil
    }
}
