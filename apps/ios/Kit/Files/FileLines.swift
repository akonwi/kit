import KitClient
import SwiftUI
import UIKit

/// A file's lines under their numbers, colored by syntax. Long lines scroll
/// sideways with the numbers held in place, unless they wrap. Marked lines
/// are washed in the accent. Only the lines on screen are drawn, so a long
/// file opens quickly.
///
/// A tap on a number selects its line for a comment, and the handle under
/// the selection drags it longer. What `below` gives for a line sits under
/// it at the screen's width; `below(0)` leads the file.
///
/// Find matches draw over the text, and a find reveal scrolls its match
/// into view, sideways too, unless it's on screen.
struct FileLines<Below: View>: View {
    let lines: [String]
    /// Each line colored by syntax, once highlighting finishes.
    var colored: [AttributedString]?
    let wrap: Bool
    /// The lines to mark, counted from 1.
    var marked: ClosedRange<Int>?
    /// The lines chosen for a comment, counted from 1.
    var selected: ClosedRange<Int>?
    /// The line the selection handle hangs under, when it can be dragged.
    var handle: Int?
    /// The find marks in each line, by its index.
    var findMarks: (Int) -> [FindMark] = { _ in [] }
    /// The find match to bring into view.
    var reveal: FindReveal?
    /// Where find reads and writes the scroll.
    var viewport: FindViewport?
    var tap: (Int) -> Void
    var drag: (Int) -> Void
    var drop: () -> Void
    @ViewBuilder var below: (Int) -> Below
    /// How far the code has scrolled sideways, so the numbers stay put.
    @State private var offset: CGFloat = 0
    @State private var screenWidth: CGFloat = 0
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
        let width = wrap ? nil : max(screenWidth, gutter + CGFloat(FileLineMetrics.longest(lines)) * Self.advance + 16)
        ScrollViewReader { proxy in
            Group {
                if let width {
                    ScrollView([.horizontal, .vertical]) { stack(gutter: gutter, width: width) }
                        .onScrollGeometryChange(for: CGFloat.self) { $0.contentOffset.x + $0.contentInsets.leading } action: { _, x in
                            offset = max(0, x)
                        }
                        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { screenWidth = $0 }
                } else {
                    ScrollView(.vertical) { stack(gutter: gutter, width: nil) }
                }
            }
            .onScrollGeometryChange(for: CGRect.self) { $0.visibleRect } action: { _, visible in
                viewport?.visible = visible
                viewport?.topLine = line(at: visible.minY - Self.inset + 1)
            }
            .onChange(of: reveal) { _, reveal in
                guard let reveal else { return }
                show(reveal, gutter: gutter, width: width, proxy: proxy)
            }
        }
        .font(Self.font)
        .foregroundStyle(Theme.text)
        .accessibilityIdentifier("file-lines")
    }

    /// The stack's padding above its first line.
    private static var inset: CGFloat { 8 }

    /// Scrolls a find match into view, its line two fifths down the screen
    /// and its text a third across, unless it's on screen already.
    private func show(_ reveal: FindReveal, gutter: CGFloat, width: CGFloat?, proxy: ScrollViewProxy) {
        let visible = viewport?.visible ?? .zero
        let shownDown = bottoms[reveal.line].map { bottom in
            bottom - Self.rowHeight + Self.inset >= visible.minY && bottom + Self.inset <= visible.maxY
        } ?? false
        var across: CGFloat = 0
        var shownAcross = true
        if let width, width > screenWidth {
            let start = CGFloat(reveal.start) * Self.advance, end = CGFloat(reveal.end) * Self.advance
            shownAcross = start >= offset && gutter + end <= offset + screenWidth
            let wanted = min(max(0, start - (screenWidth - gutter) / 3), width - screenWidth)
            across = shownAcross ? offset / (width - screenWidth) : wanted / (width - screenWidth)
        }
        guard !(shownDown && shownAcross) else { return }
        withAnimation(.snappy(duration: 0.25)) {
            proxy.scrollTo(reveal.line, anchor: UnitPoint(x: across, y: 0.4))
        }
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
        .padding(.vertical, Self.inset)
    }

    /// What sits under a line, held at the screen's width while the code
    /// scrolls sideways.
    @ViewBuilder
    private func pinned(_ content: Below, width: CGFloat?) -> some View {
        if width == nil {
            content
        } else {
            content
                .frame(width: screenWidth, alignment: .leading)
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
        let isMarked = marked?.contains(index + 1) == true
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
        if marked?.contains(index + 1) == true {
            Theme.userWash.overlay(alignment: .leading) { Theme.accent.frame(width: 3) }
        }
    }

    /// Behind a held number: the selection, the mark, or the surface the
    /// code slides under.
    @ViewBuilder
    private func gutterFill(_ index: Int) -> some View {
        if chosen(index) { Theme.accent } else if marked?.contains(index + 1) == true { wash(index) } else { Theme.surface }
    }

    /// A line's text, colored once highlighting finishes, with its find
    /// marks.
    private func text(_ index: Int) -> AttributedString {
        let text = if let colored, colored.count == lines.count { colored[index] } else { AttributedString(lines[index]) }
        return FindMarks.apply(findMarks(index), to: text)
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
