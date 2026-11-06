import KitClient
import SwiftUI
import UIKit

/// A file's lines under their numbers, colored by syntax. Long lines scroll
/// sideways with the numbers held in place, unless they wrap. A marked line
/// is washed in the accent. Only the lines on screen are drawn, so a long
/// file opens quickly.
///
/// A tap on a number selects its line for a comment, and the handle under
/// the selection drags it longer. What `below` gives for a line sits under
/// it at the screen's width; `below(0)` leads the file.
struct FileLines<Below: View>: View {
    let lines: [String]
    /// Each line colored by syntax, once highlighting finishes.
    var colored: [AttributedString]?
    let wrap: Bool
    /// The line to mark, counted from 1.
    var marked: Int?
    /// The lines chosen for a comment, counted from 1.
    var selected: ClosedRange<Int>?
    /// The line the selection handle hangs under, when it can be dragged.
    var handle: Int?
    var tap: (Int) -> Void
    var drag: (Int) -> Void
    var drop: () -> Void
    @ViewBuilder var below: (Int) -> Below
    /// How far the code has scrolled sideways, so the numbers stay put.
    @State private var offset: CGFloat = 0
    @State private var viewport: CGFloat = 0
    /// Where each line drawn ends, for the handle and what it's dragged over.
    @State private var bottoms: [Int: CGFloat] = [:]

    static var rowHeight: CGFloat { 20 }
    static var font: Font { .system(size: 12, design: .monospaced) }
    private static var space: String { "file-lines" }
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

    /// The lines, each with what sits under it. `width` is the code's
    /// width when it scrolls sideways.
    private func stack(gutter: CGFloat, width: CGFloat?) -> some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            pinned(below(0), width: width)
            ForEach(lines.indices, id: \.self) { index in
                VStack(alignment: .leading, spacing: 0) {
                    line(index, gutter: gutter, width: width)
                        .onGeometryChange(for: CGFloat.self) { $0.frame(in: .named(Self.space)).maxY } action: {
                            bottoms[index + 1] = $0
                        }
                        .modifier(LineAccessibility(number: index + 1, text: lines[index], comment: { tap(index + 1) }))
                    pinned(below(index + 1), width: width)
                }
                .id(index + 1)
            }
        }
        .overlay(alignment: .topLeading) {
            if let handle, let bottom = bottoms[handle] {
                SelectionHandle()
                    .offset(x: gutter - SelectionHandle.size / 2 + (width == nil ? 0 : offset),
                            y: bottom - SelectionHandle.size / 2)
                    .gesture(DragGesture(minimumDistance: 0, coordinateSpace: .named(Self.space))
                        .onChanged { drag(line(at: $0.location.y)) }
                        .onEnded { _ in drop() })
            }
        }
        .coordinateSpace(.named(Self.space))
        .padding(.vertical, 8)
    }

    /// What sits under a line, held at the screen's width while the code
    /// scrolls sideways.
    @ViewBuilder
    private func pinned(_ content: Below, width: CGFloat?) -> some View {
        if width == nil {
            content
        } else {
            content
                .frame(width: viewport, alignment: .leading)
                .offset(x: offset)
        }
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
                    .background { if chosen(index) { Theme.accent } }
                Text(text(index))
                    .padding(.vertical, 2)
                    .padding(.trailing, 16)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(minHeight: Self.rowHeight)
            .background { wash(index) }
        }
    }

    private func chosen(_ index: Int) -> Bool { selected?.contains(index + 1) == true }

    private func number(_ index: Int, width: CGFloat) -> some View {
        let isMarked = marked == index + 1
        return Text("\(index + 1)")
            .font(.system(size: 11, weight: isMarked ? .semibold : .regular, design: .monospaced))
            .foregroundStyle(chosen(index) ? Color.white : isMarked ? Theme.accent : Theme.muted)
            .padding(.trailing, 12)
            .frame(width: width, alignment: .trailing)
            .contentShape(Rectangle())
            .onTapGesture { tap(index + 1) }
    }

    @ViewBuilder
    private func wash(_ index: Int) -> some View {
        if marked == index + 1 {
            Theme.userWash.overlay(alignment: .leading) { Theme.accent.frame(width: 3) }
        }
    }

    /// Behind a held number: the selection, the mark, or the surface the
    /// code slides under.
    @ViewBuilder
    private func gutterFill(_ index: Int) -> some View {
        if chosen(index) { Theme.accent } else if marked == index + 1 { wash(index) } else { Theme.surface }
    }

    /// A line's text, colored once highlighting finishes.
    private func text(_ index: Int) -> AttributedString {
        if let colored, colored.count == lines.count { return colored[index] }
        return AttributedString(lines[index])
    }

    /// The line under a point in the stack: the first drawn line ending at
    /// or below it. The handle sits on a line's bottom edge, so a point on
    /// an edge is the line above it.
    private func line(at y: CGFloat) -> Int {
        let drawn = bottoms.sorted { $0.key < $1.key }
        return drawn.first { $0.value >= y }?.key ?? drawn.last?.key ?? 1
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
    let comment: () -> Void

    func body(content: Content) -> some View {
        content
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Line \(number)")
            .accessibilityValue(text.isEmpty ? "Blank" : text)
            .accessibilityAction(named: "Comment", comment)
    }
}
