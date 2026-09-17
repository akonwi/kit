import SwiftUI

/// Reserved activity slot between the transcript and composer, matching the TUI.
struct PendingActivityView: View {
    @Environment(\.mica) private var theme
    let activity: String?

    var body: some View {
        HStack(spacing: 8) {
            if let activity, !activity.isEmpty {
                KitSpinner()
                Text((try? AttributedString(markdown: activity)) ?? AttributedString(activity))
                    .lineLimit(1).truncationMode(.tail)
                    .help(activity)
            }
            Spacer(minLength: 0)
        }
        .font(.kit(size: 12)).foregroundStyle(theme.muted)
        .frame(height: 24)
        .accessibilityElement(children: .combine)
    }
}
