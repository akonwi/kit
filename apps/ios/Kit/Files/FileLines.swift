import KitClient
import SwiftUI
import UIKit

/// A file's lines under their numbers, colored by syntax. Long lines scroll
/// sideways with the numbers held in place, unless they wrap. A marked line
/// is washed in the accent. Only the lines on screen are drawn, so a long
/// file opens quickly.
struct FileLines: View {
    let lines: [String]
    /// Each line colored by syntax, once highlighting finishes.
    var colored: [AttributedString]?
    let wrap: Bool
    /// The line to mark, counted from 1.
    var marked: Int?
    /// How far the code has scrolled sideways, so the numbers stay put.
    @State private var offset: CGFloat = 0
    @State private var viewport: CGFloat = 0

    static var rowHeight: CGFloat { 20 }
    static var font: Font { .system(size: 12, design: .monospaced) }
    private static var advance: CGFloat {
        ("0" as NSString).size(withAttributes: [.font: UIFont.monospacedSystemFont(ofSize: 12, weight: .regular)]).width
    }

    var body: some View {
        let gutter = FileLineMetrics.gutterWidth(lines.count)
        Group {
            if wrap {
                ScrollView(.vertical) { stack(gutter: gutter, width: nil) }
            } else {
                let width = max(viewport, gutter + CGFloat(FileLineMetrics.longest(lines)) * Self.advance + 16)
                ScrollView([.horizontal, .vertical]) { stack(gutter: gutter, width: width) }
                    .onScrollGeometryChange(for: CGFloat.self) { $0.contentOffset.x + $0.contentInsets.leading } action: { _, x in
                        offset = max(0, x)
                    }
                    .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { viewport = $0 }
            }
        }
        .font(Self.font)
        .foregroundStyle(Theme.text)
        .accessibilityIdentifier("file-lines")
    }

    /// The lines. `width` is the code's width when it scrolls sideways.
    private func stack(gutter: CGFloat, width: CGFloat?) -> some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(lines.indices, id: \.self) { index in
                line(index, gutter: gutter, width: width)
                    .modifier(LineAccessibility(number: index + 1, text: lines[index]))
                    .id(index + 1)
            }
        }
        .padding(.vertical, 8)
    }

    @ViewBuilder
    private func line(_ index: Int, gutter: CGFloat, width: CGFloat?) -> some View {
        if let width {
            HStack(spacing: 0) {
                // Drawn after the code so it covers what scrolls under it.
                Color.clear.frame(width: gutter)
                Text(text(index))
                    .lineLimit(1)
                    .fixedSize()
                    .padding(.trailing, 16)
                Spacer(minLength: 0)
            }
            .frame(width: width, height: Self.rowHeight, alignment: .leading)
            .background { wash(index) }
            .overlay(alignment: .leading) {
                number(index, width: gutter)
                    .frame(height: Self.rowHeight)
                    .background { gutterFill(index) }
                    .offset(x: offset)
            }
        } else {
            HStack(alignment: .firstTextBaseline, spacing: 0) {
                number(index, width: gutter)
                    .frame(minHeight: Self.rowHeight)
                Text(text(index))
                    .padding(.vertical, 2)
                    .padding(.trailing, 16)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(minHeight: Self.rowHeight)
            .background { wash(index) }
        }
    }

    private func number(_ index: Int, width: CGFloat) -> some View {
        let isMarked = marked == index + 1
        return Text("\(index + 1)")
            .font(.system(size: 11, weight: isMarked ? .semibold : .regular, design: .monospaced))
            .foregroundStyle(isMarked ? Theme.accent : Theme.muted)
            .padding(.trailing, 12)
            .frame(width: width, alignment: .trailing)
    }

    @ViewBuilder
    private func wash(_ index: Int) -> some View {
        if marked == index + 1 {
            Theme.userWash.overlay(alignment: .leading) { Theme.accent.frame(width: 3) }
        }
    }

    /// Behind a held number: the mark, or the surface the code slides under.
    @ViewBuilder
    private func gutterFill(_ index: Int) -> some View {
        if marked == index + 1 { wash(index) } else { Theme.surface }
    }

    /// A line's text, colored once highlighting finishes.
    private func text(_ index: Int) -> AttributedString {
        if let colored, colored.count == lines.count { return colored[index] }
        return AttributedString(lines[index])
    }
}

/// How wide a file's numbers and lines are.
enum FileLineMetrics {
    /// Wide enough for the largest number, with room after it.
    static func gutterWidth(_ count: Int) -> CGFloat {
        max(44, CGFloat(String(count).count) * 6.7 + 18)
    }

    /// The most characters in one line.
    static func longest(_ lines: [String]) -> Int {
        lines.reduce(0) { max($0, $1.count) }
    }
}

/// A line read by VoiceOver as its number and its text.
private struct LineAccessibility: ViewModifier {
    let number: Int
    let text: String

    func body(content: Content) -> some View {
        content
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Line \(number)")
            .accessibilityValue(text.isEmpty ? "Blank" : text)
    }
}
