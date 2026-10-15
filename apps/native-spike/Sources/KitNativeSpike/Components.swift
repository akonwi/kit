import SwiftUI

/// Braille spinner drawn as dots, like the boards (not a font glyph).
struct BrailleSpinner: View {
    var color: Color
    var animating = true
    private static let frames: [UInt8] = [0x0B, 0x19, 0x39, 0x38, 0x3C, 0x34, 0x26, 0x27, 0x07, 0x0F]
    // Bit order follows Unicode braille: dots 1-3 left column, 4-6 right, 7/8 bottom row.
    private static let positions: [(CGFloat, CGFloat)] = [
        (3.25, 2.25), (3.25, 5.75), (3.25, 9.25), (8.25, 2.25), (8.25, 5.75), (8.25, 9.25), (3.25, 12.75), (8.25, 12.75),
    ]

    var body: some View {
        TimelineView(.animation(minimumInterval: 0.08, paused: !animating)) { context in
            let index = Int(context.date.timeIntervalSinceReferenceDate / 0.08) % Self.frames.count
            let bits = Self.frames[animating ? index : 0]
            Canvas { ctx, _ in
                for (bit, point) in Self.positions.enumerated() where bits & (1 << bit) != 0 {
                    ctx.fill(Path(ellipseIn: CGRect(x: point.0 - 1.25, y: point.1 - 1.25, width: 2.5, height: 2.5)),
                             with: .color(color))
                }
            }
        }
        .frame(width: 12, height: 14)
        .accessibilityLabel("Running")
    }
}

/// Session tab marker: spinner while running, ? while waiting, check when done.
struct StatusMarker: View {
    @Environment(\.theme) private var theme
    let status: SessionStatus

    var body: some View {
        switch status {
        case .running: BrailleSpinner(color: theme.accent)
        case .waiting: Icon(glyph: .questionMark, size: 12, color: theme.warning)
        case .finished: Icon(glyph: .check, size: 12, color: theme.success)
        case .idle: Color.clear.frame(width: 12, height: 12)
        }
    }
}

/// 24 px square icon button with a hover fill (3 px corners).
struct IconButton: View {
    @Environment(\.theme) private var theme
    let glyph: Ph
    var size: CGFloat = 24
    var iconSize: CGFloat = 14
    var help: String = ""
    var tint: Color? = nil
    let action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            Icon(glyph: glyph, size: iconSize, color: tint ?? (hover ? theme.text : theme.muted))
                .frame(width: size, height: size)
                .background(RoundedRectangle(cornerRadius: 3).fill(hover ? theme.frameHover : .clear))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
        .help(help)
    }
}

/// Small bordered button (22 px), used by toasts, docks and the queue tray.
struct SmallButton: View {
    @Environment(\.theme) private var theme
    let title: String
    var key: String? = nil
    var primary = false
    let action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Text(title).font(Fonts.ui(12))
                if let key {
                    Text(key).font(Fonts.mono(10)).foregroundStyle(primary ? theme.surface.opacity(0.6) : theme.muted)
                }
            }
            .padding(.horizontal, 8)
            .frame(height: 22)
            .foregroundStyle(primary ? theme.surface : theme.text)
            .background(RoundedRectangle(cornerRadius: 3).fill(primary ? theme.text : (hover ? theme.frameHover : .clear)))
            .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(primary ? .clear : theme.border))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
    }
}

/// Monospaced summary chip (3 px corners).
struct CodeChip: View {
    @Environment(\.theme) private var theme
    let text: String

    var body: some View {
        Text(text)
            .font(Fonts.mono(11))
            .foregroundStyle(theme.text)
            .lineLimit(1)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(RoundedRectangle(cornerRadius: 3).fill(theme.chip))
    }
}

/// Inline Markdown-ish text: `code` spans become chips in a Text run.
func inlineCode(_ string: String, theme: Theme, size: CGFloat = 13, color: Color? = nil) -> Text {
    var result = Text("")
    for (index, part) in string.components(separatedBy: "`").enumerated() {
        if index % 2 == 1 {
            var code = AttributedString(part)
            code.font = Fonts.mono(size - 1)
            code.backgroundColor = theme.chip
            code.foregroundColor = theme.text
            result = result + Text(code)
        } else {
            result = result + Text(part).font(Fonts.ui(size)).foregroundColor(color ?? theme.text)
        }
    }
    return result
}
