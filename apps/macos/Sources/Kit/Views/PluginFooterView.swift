import SwiftUI

/// Only the right-hand footer is configurable; session controls and status remain
/// outside this region. Overflow opens the complete styled content in a popover.
struct PluginFooterView: View {
    @Environment(\.mica) private var theme
    let session: SessionExcerpt?
    @State private var showingOverflow = false
    private var items: [WirePluginFooterItem] { session?.pluginFooter?.items ?? [] }
    private var hideLocation: Bool { session?.pluginFooter?.locationHidden == true }
    var body: some View {
        Group {
            if items.isEmpty {
                if !hideLocation { WorkspaceLocation(session: session) }
            } else {
                ViewThatFits(in: .horizontal) {
                    // Prefer the largest ordered prefix that fits; an item
                    // never disappears merely because a later item is wide.
                    ForEach(Array((0...items.count).reversed()), id: \.self) { count in
                        HStack(spacing: 8) {
                            if !hideLocation { WorkspaceLocation(session: session) }
                            if count > 0 {
                                Text(Self.styledContent(items: Array(items.prefix(count)), theme: theme)).fixedSize()
                            }
                            if let overflow = Self.overflowLabel(total: items.count, visible: count) {
                                overflowButton(overflow).fixedSize()
                            }
                        }.fixedSize()
                    }
                    HStack(spacing: 8) {
                        if !hideLocation { WorkspaceLocation(session: session) }
                        overflowButton("… \(items.count) more").fixedSize().layoutPriority(1)
                    }
                    // Even the location-plus-label row can be too wide. Drop
                    // location before allowing the count itself to truncate.
                    overflowButton("… \(items.count) more")
                        .lineLimit(1).truncationMode(.tail)
                }
                .help(items.map { $0.pluginId + ": " + ($0.content ?? []).map(\.text).joined() }.joined(separator: "\n"))
            }
        }
        .lineLimit(1).frame(maxWidth: .infinity, alignment: .trailing)
        .popover(isPresented: $showingOverflow, arrowEdge: .bottom) {
            PluginFooterDetails(session: session)
        }
        .onChange(of: session?.id) { showingOverflow = false }
        .onChange(of: items.count) { if items.isEmpty { showingOverflow = false } }
    }
    private func overflowButton(_ label: String) -> some View {
        Button { showingOverflow = true } label: {
            Text(label).font(.kit(size: 12))
        }
        .buttonStyle(MicaButtonStyle(compact: true))
        .accessibilityLabel("Show all footer content")
        .help("Show all footer content")
    }

    static func overflowLabel(total: Int, visible: Int) -> String? {
        visible < total ? "… \(total - visible) more" : nil
    }

    static func styledContent(items: [WirePluginFooterItem], theme: MicaTheme) -> AttributedString {
        var result = AttributedString()
        for (index, item) in items.enumerated() {
            if index > 0 { result.append(AttributedString(" · ")) }
            for segment in item.content ?? [] {
                var part = AttributedString(segment.text)
                let style = segment.style
                var font = Font.kit(size: 12, weight: style.bold == true ? .bold : .regular)
                if style.italic == true { font = font.italic() }
                part.font = font
                let foreground = color(style.fg, fallback: theme.muted, theme: theme)
                part.foregroundColor = style.dim == true ? foreground.opacity(0.65) : foreground
                if let bg = style.bg { part.backgroundColor = color(bg, fallback: theme.raised, theme: theme) }
                if style.underline == true { part.underlineStyle = .single }
                if style.strikethrough == true { part.strikethroughStyle = .single }
                result.append(part)
            }
        }
        return result
    }
    private static func color(_ token: String?, fallback: Color, theme: MicaTheme) -> Color {
        guard let token, !token.isEmpty else { return fallback }
        let base: Color
        switch token {
        case "bgTransparent": base = .clear
        case "bg": base = theme.surface
        case "bgSurface": base = theme.raised
        case "bgMuted", "pickerFocusedBg": base = theme.hover
        case "errorText", "progressCritical": base = theme.danger
        case "warningText", "progressWarning": base = theme.warning
        case "toolText", "progressNormal", "toggleOn": base = theme.success
        case "textPrimary", "assistantText", "userText", "pickerFocusedText", "panelText": base = theme.text
        case "borderDefault", "pickerBorder": base = theme.border
        case "borderFocused", "borderAccent", "bgAccent", "reviewText", "subagentText", "attachmentText": base = theme.accent
        default: base = fallback
        }
        return theme.token(token, fallback: base)
    }
}
