import SwiftUI

/// A plugin-submitted message as one quiet disclosure row naming the plugin and
/// the message's first line. Expanding it shows the full message beneath a
/// rule. The message is context the plugin gave the agent, so it stays below
/// user and assistant messages in visual weight, like a tool group.
struct PluginMessageRow: View {
    @Environment(\.mica) private var theme
    let message: TranscriptMessage
    let origin: PluginMessageOrigin
    /// Keyed by turn so the choice survives the live-to-persisted row change.
    @Bindable var state: ToolDrawerState
    var onExpand: () -> Void = {}
    @State private var hovering = false

    var body: some View {
        let expanded = state.expanded ?? false
        VStack(alignment: .leading, spacing: 10) {
            Button {
                state.expanded = !expanded
                if !expanded { onExpand() }
            } label: {
                HStack(spacing: 9) {
                    Image(systemName: "puzzlepiece.extension").font(.kit(size: 12)).frame(width: 16)
                    Text(origin.pluginID).font(.kit(size: 12, weight: .medium))
                        .foregroundStyle(hovering ? theme.accent : theme.text).fixedSize()
                    Text(glyphMiddleDot)
                    Text(Self.summary(message.text)).lineLimit(1).truncationMode(.tail)
                    Spacer(minLength: 0)
                    Image(systemName: expanded ? "chevron.down" : "chevron.right")
                        .font(.kit(size: 8, weight: .semibold)).frame(width: 16)
                }.padding(.vertical, 5).contentShape(Rectangle())
            }
            .buttonStyle(.plain).foregroundStyle(theme.muted)
            .onHover { hovering = $0 }
            .help("\(expanded ? "Hide" : "Show") the message from \(origin.pluginID)")
            .accessibilityLabel("Message from \(origin.pluginID)")
            .accessibilityValue(expanded ? "Expanded" : "Collapsed")
            if expanded {
                MarkdownView(source: message.text)
                    .foregroundStyle(theme.muted)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.leading, 12)
                    .overlay(alignment: .leading) { Rectangle().fill(theme.border).frame(width: 2) }
                    .padding(.leading, 25)
            }
        }
        .font(.kit(size: 12))
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 4)
        .background(MessageCopyRegion(markdown: message.text))
    }

    /// The first non-blank line of a plugin message, without Markdown syntax.
    nonisolated static func summary(_ text: String) -> String {
        MessageText.plain(text).split(separator: "\n", omittingEmptySubsequences: true)
            .lazy.map { $0.trimmingCharacters(in: .whitespaces) }
            .first { !$0.isEmpty } ?? ""
    }

    private let glyphMiddleDot = "·"
}
