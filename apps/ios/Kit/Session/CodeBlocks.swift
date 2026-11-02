import KitGrammars
import KitHighlighting
import KitTranscript
import SwiftUI

extension CodeSyntaxHighlighter {
    /// The app's highlighter, parsing with the grammars bundled in KitGrammars.
    static let shared = CodeSyntaxHighlighter(grammars: BundledSyntaxGrammars())
}

/// One line of a code card: its text, colored by syntax, and its diff fill.
struct CodeLine: Equatable {
    enum Fill: Equatable { case none, added, removed }

    var text: AttributedString
    var fill: Fill = .none
}

/// Fenced code or a tool's file content, colored by `syntax` once
/// highlighting finishes. A diff fills its added and removed lines.
struct CodeBlock: View {
    /// The fence's language, shown above the code. Empty shows none.
    let language: String
    let source: String
    /// The language to color the code as, when it differs from the label.
    var syntax: String?
    /// The color of text that has no syntax color.
    var tone: Color = Theme.text
    /// The colored lines and the code they were colored for.
    @State private var colored: (request: CodeHighlightRequest, lines: [AttributedString])?

    private var request: CodeHighlightRequest { .init(source: source, language: syntax ?? language) }

    private var lines: [CodeLine] {
        let texts = colored?.request == request ? colored!.lines : CodeColoring.lines(source, spans: [])
        return texts.map { text in
            CodeLine(text: text, fill: language == "diff" ? CodeColoring.fill(String(text.characters)) : .none)
        }
    }

    var body: some View {
        CodeCard(label: language, lines: lines, copy: source, tone: tone)
            .task(id: request) {
                let current = request
                guard let spans = await CodeColoring.spans(for: current) else { return }
                colored = (current, CodeColoring.lines(current.source, spans: spans))
            }
    }
}

/// An edit's changed lines, filled by kind and colored by `syntax`: removed
/// lines as the old text reads, added and unchanged lines as the new text.
struct EditDiffBlock: View {
    let lines: [ToolDiffLine]
    let syntax: String
    @State private var colored: (lines: [ToolDiffLine], code: [CodeLine])?

    var body: some View {
        CodeCard(label: "", lines: colored?.lines == lines ? colored!.code : CodeColoring.diff(lines),
                 copy: CodeColoring.diffText(lines))
            .task(id: lines) {
                let current = lines
                let sides = CodeColoring.sides(current)
                guard let old = await CodeColoring.spans(for: .init(source: sides.old, language: syntax)),
                      let new = await CodeColoring.spans(for: .init(source: sides.new, language: syntax)),
                      !Task.isCancelled else { return }
                colored = (current, CodeColoring.diff(current, oldSpans: old, newSpans: new))
            }
    }
}

/// Code lines on a raised card that scrolls sideways. A long press copies
/// `copy`.
private struct CodeCard: View {
    let label: String
    let lines: [CodeLine]
    let copy: String
    var tone: Color = Theme.text
    /// The visible width, so line fills reach the card's edge.
    @State private var width: CGFloat = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !label.isEmpty {
                Text(label)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Theme.muted)
                    .padding(.horizontal, 12)
                    .padding(.top, 2)
                    .padding(.bottom, 6)
            }
            ScrollView(.horizontal) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                        Text(line.text.characters.isEmpty ? AttributedString(" ") : line.text)
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(tone)
                            .lineSpacing(18 - 12 * 1.2)
                            .fixedSize()
                            .padding(.horizontal, 12)
                            .padding(.vertical, 1)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(fill(line.fill))
                    }
                }
                .frame(minWidth: width, alignment: .leading)
            }
            .scrollIndicators(.hidden)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        }
        .padding(.vertical, 6)
        .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.controlRadius))
        .overlay { RoundedRectangle(cornerRadius: Theme.controlRadius).strokeBorder(Theme.border, lineWidth: 0.5) }
        .clipShape(RoundedRectangle(cornerRadius: Theme.controlRadius))
        .contextMenu {
            Button("Copy Code", systemImage: "doc.on.doc") { UIPasteboard.general.string = copy }
        }
    }

    private func fill(_ fill: CodeLine.Fill) -> Color {
        switch fill {
        case .added: Theme.diffAdded
        case .removed: Theme.diffRemoved
        case .none: .clear
        }
    }
}

/// Colors code by its syntax roles, the way the Mac's default palette does.
enum CodeColoring {
    /// The spans of a request, or nil when its language has no grammar or
    /// highlighting fails, so the code stays plain.
    static func spans(for request: CodeHighlightRequest) async -> [SyntaxSpan]? {
        guard CodeSyntaxHighlighter.canonicalLanguage(request.language) != nil,
              let spans = try? await CodeSyntaxHighlighter.shared.spans(for: request),
              !spans.isEmpty else { return nil }
        return spans
    }

    /// Each line of `source` with its spans colored. A trailing newline adds
    /// no line.
    static func lines(_ source: String, spans: [SyntaxSpan]) -> [AttributedString] {
        var texts = source.components(separatedBy: "\n")
        if texts.count > 1, texts.last == "" { texts.removeLast() }
        return lines(texts, spans: spans)
    }

    /// Each of `texts`, the lines of one source, with its spans colored.
    /// Spans are applied in order, so a later span wins where two overlap.
    static func lines(_ texts: [String], spans: [SyntaxSpan]) -> [AttributedString] {
        var starts: [Int] = []
        var offset = 0
        for text in texts {
            starts.append(offset)
            offset += text.utf16.count + 1
        }
        var lines = texts.map { AttributedString($0) }
        guard !lines.isEmpty else { return lines }
        for span in spans {
            // The last line starting at or before the span.
            var low = 0, high = starts.count - 1
            while low < high {
                let middle = (low + high + 1) / 2
                if starts[middle] <= span.range.location { low = middle } else { high = middle - 1 }
            }
            var index = low
            while index < lines.count, starts[index] < NSMaxRange(span.range) {
                let lineRange = NSRange(location: starts[index], length: texts[index].utf16.count)
                let overlap = NSIntersectionRange(lineRange, span.range)
                if overlap.length > 0,
                   let range = Range(NSRange(location: overlap.location - starts[index], length: overlap.length),
                                     in: lines[index]) {
                    lines[index][range].foregroundColor = color(span.role)
                }
                index += 1
            }
        }
        return lines
    }

    /// The old and new text an edit's lines come from: removed and unchanged
    /// lines, then added and unchanged ones.
    static func sides(_ lines: [ToolDiffLine]) -> (old: String, new: String) {
        (lines.filter { $0.kind != .added }.map(\.text).joined(separator: "\n"),
         lines.filter { $0.kind != .removed }.map(\.text).joined(separator: "\n"))
    }

    /// An edit's lines, each after its +, -, or blank marker, colored with
    /// the spans of its side.
    static func diff(_ lines: [ToolDiffLine], oldSpans: [SyntaxSpan] = [],
                     newSpans: [SyntaxSpan] = []) -> [CodeLine] {
        let old = Self.lines(lines.filter { $0.kind != .added }.map(\.text), spans: oldSpans)
        let new = Self.lines(lines.filter { $0.kind != .removed }.map(\.text), spans: newSpans)
        var oldIndex = 0, newIndex = 0
        return lines.map { line in
            switch line.kind {
            case .removed:
                defer { oldIndex += 1 }
                return CodeLine(text: AttributedString("- ") + old[oldIndex], fill: .removed)
            case .added:
                defer { newIndex += 1 }
                return CodeLine(text: AttributedString("+ ") + new[newIndex], fill: .added)
            case .context:
                defer { oldIndex += 1; newIndex += 1 }
                return CodeLine(text: AttributedString("  ") + new[newIndex])
            }
        }
    }

    /// An edit's lines as diff text, for copying.
    static func diffText(_ lines: [ToolDiffLine]) -> String {
        lines.map { line in
            switch line.kind {
            case .added: "+ " + line.text
            case .removed: "- " + line.text
            case .context: "  " + line.text
            }
        }.joined(separator: "\n")
    }

    /// The fill of a line of fenced diff text.
    static func fill(_ line: String) -> CodeLine.Fill {
        if line.hasPrefix("+") && !line.hasPrefix("+++") { return .added }
        if line.hasPrefix("-") && !line.hasPrefix("---") { return .removed }
        return .none
    }

    /// A syntax role's color.
    static func color(_ role: String) -> Color {
        switch role {
        case "comment": Theme.muted
        case "string", "escape": Theme.success
        case "keyword", "keywordType", "function", "type", "builtin": Theme.accent
        default: Theme.text
        }
    }
}
