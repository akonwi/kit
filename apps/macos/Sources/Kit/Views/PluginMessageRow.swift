import SwiftUI

/// A plugin-submitted message presented like a recorded tool call: the plugin
/// symbol, the plugin name, and the message's first line as the summary.
/// Expanding it shows the full message as a tool-call detail. The message is
/// machine input that starts the turn, so it shares the tool-call vocabulary
/// but stays its own row ahead of the turn's tool group.
struct PluginMessageRow: View {
    @Environment(\.mica) private var theme
    let message: TranscriptMessage
    let origin: PluginMessageOrigin
    /// Keyed by turn so the choice survives the live-to-persisted row change.
    @Bindable var state: ToolDrawerState
    var onExpand: () -> Void = {}
    @State private var hovering = false
    @State private var contentHeight: CGFloat = 1

    var body: some View {
        let expanded = state.expanded ?? false
        VStack(alignment: .leading, spacing: 10) {
            Button {
                state.expanded = !expanded
                if !expanded { onExpand() }
            } label: {
                DisclosureChipLabel(icon: "puzzlepiece.extension", iconColor: theme.muted, title: origin.pluginID,
                                    summary: Self.summary(message.text), monospacedSummary: false,
                                    expanded: expanded, hovering: hovering)
            }
            .buttonStyle(.plain).onHover { hovering = $0 }
            .help(Self.summary(message.text))
            .accessibilityLabel("Message from \(origin.pluginID)")
            .accessibilityValue(expanded ? "Expanded" : "Collapsed")
            .accessibilityHint("Show the full message")
            if expanded {
                ScrollView(.vertical) {
                    MarkdownView(source: message.text)
                        .frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 8)
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
                }
                .defaultScrollAnchor(.topLeading, for: .alignment)
                .frame(height: min(max(1, contentHeight), 280))
                .disclosureDetail()
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
}
