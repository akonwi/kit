import SwiftUI

/// What a conversation pane's activity slot reports, most urgent first.
enum ActivityLine: Equatable {
    /// The provider failed and the server retries at this ISO 8601 time.
    case retrying(at: String)
    case compacting(String)
    /// The agent's latest thinking or tool activity, as markdown.
    case working(String)
    case replaying
    case notice(String)

    static func resolve(activity: String?, compaction: String? = nil, retryAt: String? = nil,
                        replaying: Bool = false, notice: String = "") -> ActivityLine? {
        if let retryAt { return .retrying(at: retryAt) }
        if let compaction { return .compacting(compaction) }
        if let activity, !activity.isEmpty { return .working(activity) }
        if replaying { return .replaying }
        if !notice.isEmpty { return .notice(notice) }
        return nil
    }

    static func secondsRemaining(_ timestamp: String, now: Date) -> Int {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let date = formatter.date(from: timestamp) ?? ISO8601DateFormatter().date(from: timestamp)
        return max(0, Int(ceil((date ?? now).timeIntervalSince(now))))
    }
}

/// A conversation pane's activity card above its composer. The slot keeps its
/// height while idle, so the card appearing never shifts the transcript.
struct PendingActivityView: View {
    @Environment(\.mica) private var theme
    let line: ActivityLine?

    init(line: ActivityLine?) { self.line = line }
    init(activity: String?) { line = ActivityLine.resolve(activity: activity) }

    var body: some View {
        if let line {
            HStack(spacing: 8) {
                content(line)
                Spacer(minLength: 0)
            }
            .font(.kit(size: 13)).foregroundStyle(theme.muted)
            .padding(.leading, 13).padding(.trailing, 10)
            .frame(height: Self.height)
            .background(theme.frame, in: RoundedRectangle(cornerRadius: 4))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
            .accessibilityElement(children: .combine)
        } else {
            Color.clear.frame(height: Self.height).accessibilityHidden(true)
        }
    }

    @ViewBuilder private func content(_ line: ActivityLine) -> some View {
        switch line {
        case .retrying(let at):
            Image(systemName: "arrow.clockwise").foregroundStyle(theme.warning)
            TimelineView(.periodic(from: .now, by: 1)) { context in
                Text("Provider retry in \(ActivityLine.secondsRemaining(at, now: context.date))s")
                    .foregroundStyle(theme.warning)
            }
        case .compacting(let title):
            KitSpinner()
            Text(title)
        case .working(let activity):
            KitSpinner()
            Text((try? AttributedString(markdown: activity)) ?? AttributedString(activity))
                .lineLimit(1).truncationMode(.tail)
                .help(activity)
        case .replaying:
            Image(systemName: "play.circle").foregroundStyle(theme.accent)
            Text("Replaying")
        case .notice(let text):
            Text(text).lineLimit(1).truncationMode(.tail).help(text)
        }
    }

    static let height: CGFloat = 30
}
