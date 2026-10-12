import SwiftUI

/// A conversation pane's activity card above its composer: what the agent
/// is doing while a turn runs. The slot keeps its height while idle, so the
/// card appearing never shifts the transcript.
struct PendingActivityView: View {
    @Environment(\.mica) private var theme
    let activity: String?

    var body: some View {
        if let activity, !activity.isEmpty {
            HStack(spacing: 8) {
                KitSpinner()
                Text((try? AttributedString(markdown: activity)) ?? AttributedString(activity))
                    .lineLimit(1).truncationMode(.tail)
                    .help(activity)
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

    static let height: CGFloat = 30
}
