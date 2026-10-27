import SwiftUI

/// The follow-ups queued behind the running turn, joined to the top edge of
/// the composer. The server acts on the queue as a whole, so the rows are
/// previews and only the header's actions change it. When the first message
/// could not start, the queue is held and the tray says why until it is
/// edited or cleared.
struct QueuedTray: View {
    @Environment(\.mica) private var theme
    let queue: FollowUpState
    /// Why the last queue change failed.
    var error: String? = nil
    /// Whether Edit, Clear, and Send now can act.
    var enabled = true
    /// Send now steers the running turn, so it needs one.
    var running = false
    var edit: () -> Void = {}
    var clear: () -> Void = {}
    var sendNow: () -> Void = {}
    /// Joined to the composer below; a free-standing tray above the
    /// interaction dock rounds all four corners.
    var joined = true

    /// The tray shows this many previews, then counts the rest.
    static let visibleRows = 3

    /// The previews shown, and how many queued messages they leave out.
    static func rows(_ queue: FollowUpState) -> (previews: [String], more: Int) {
        let previews = Array(queue.previews.prefix(visibleRows))
        return (previews, max(0, queue.count - previews.count))
    }

    /// The header's count, which says when the queue is held.
    static func title(_ queue: FollowUpState) -> String {
        String(queue.count) + (queue.admissionFailure == nil ? " queued" : " held")
    }

    /// Why a held queue's first message could not start.
    static func failure(_ queue: FollowUpState) -> String? {
        queue.admissionFailure.map { "Couldn't start: " + $0 }
    }

    var body: some View {
        let rows = Self.rows(queue)
        VStack(spacing: 0) {
            header
            if let failure = Self.failure(queue) {
                Rule()
                Text(failure).font(.kit(size: 12)).foregroundStyle(theme.danger)
                    .lineLimit(2).help(failure)
                    .padding(.horizontal, 12).padding(.vertical, 5)
                    .frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
            }
            ForEach(Array(rows.previews.enumerated()), id: \.offset) { index, preview in
                Rule()
                HStack(spacing: 10) {
                    Text(String(index + 1)).font(.kit(size: 11, design: .monospaced))
                        .foregroundStyle(theme.strongBorder).frame(width: 10, alignment: .leading)
                    Text(preview).font(.kit(size: 12)).foregroundStyle(theme.text)
                        .lineLimit(1).truncationMode(.tail)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .padding(.horizontal, 12).frame(height: 26)
                .accessibilityElement(children: .combine)
            }
            if rows.more > 0 {
                Rule()
                Text(String(rows.more) + " more").font(.kit(size: 12)).foregroundStyle(theme.muted)
                    .padding(.leading, 32).padding(.trailing, 12)
                    .frame(maxWidth: .infinity, minHeight: 24, alignment: .leading)
            }
            if let error {
                Rule()
                Text(error).font(.kit(size: 12)).foregroundStyle(theme.danger)
                    .lineLimit(2).help(error)
                    .padding(.horizontal, 12).padding(.vertical, 5)
                    .frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
            }
        }
        .background(theme.frame, in: shape)
        .overlay(shape.strokeBorder(theme.border))
    }

    private var header: some View {
        HStack(spacing: 8) {
            IconView(.stack, size: 12).foregroundStyle(theme.muted)
            Text(Self.title(queue)).font(.kit(size: 12)).foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, alignment: .leading)
            Group {
                Button("Edit", action: edit).buttonStyle(SmallButtonStyle())
                    .help("Move the queued messages back into the composer")
                Button("Clear", action: clear).buttonStyle(SmallButtonStyle())
                    .help("Remove the queued messages")
                Button("Send now", action: sendNow).buttonStyle(SmallButtonStyle(bordered: true))
                    .help("Steer the running turn with the queued messages")
                    .disabled(!running)
            }
            .disabled(!enabled)
        }
        .padding(.leading, 12).padding(.trailing, 6).frame(height: 30)
    }

    /// Square at the bottom when joined, where the composer's border continues it.
    private var shape: UnevenRoundedRectangle {
        let bottom: CGFloat = joined ? 0 : 4
        return UnevenRoundedRectangle(topLeadingRadius: 4, bottomLeadingRadius: bottom,
                                      bottomTrailingRadius: bottom, topTrailingRadius: 4)
    }
}
