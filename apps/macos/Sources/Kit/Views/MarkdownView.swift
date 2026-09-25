import AppKit
import OSLog
import SwiftUI

struct MarkdownView: View {
    let source: String
    var onSections: (([TranscriptReadingSection]) -> Void)? = nil

    var body: some View {
        MarkdownBlocks(blocks: MarkdownContentCache.shared.content(for: source).blocks, measureSections: onSections != nil)
            .coordinateSpace(name: "markdownSections")
            .onPreferenceChange(MarkdownSectionFrames.self) { frames in
                guard onSections != nil else { return }
                let blocks = MarkdownContentCache.shared.content(for: source).blocks
                let sections = blocks.enumerated().compactMap { index, block -> TranscriptReadingSection? in
                    guard let offset = frames[index] else { return nil }
                    if case .heading(_, let title) = block {
                        return TranscriptReadingSection(id: index, title: String(title.characters), offset: offset)
                    }
                    return index == 0 ? TranscriptReadingSection(id: index, title: "Overview", offset: offset) : nil
                }
                onSections?(sections)
            }
            .font(.kit(size: 14)).textSelection(.enabled)
    }
}

private struct MarkdownBlocks: View {
    @Environment(\.mica) private var theme
    let blocks: [MarkdownContent.Block]
    var measureSections = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { index, block in
                Group {
                    switch block {
                    case .paragraph(let value): MarkdownInline(value: value).lineSpacing(5)
                    case .heading(let level, let value):
                        MarkdownInline(value: value)
                            .font(.kit(size: level == 1 ? 20 : level == 2 ? 17 : 15, weight: .semibold))
                            .padding(.top, 4)
                    case .code(let language, let value): CodeBlock(language: language, source: value)
                    case .quote(let children):
                        MarkdownBlocks(blocks: children)
                            .padding(.leading, 14)
                            .overlay(alignment: .leading) { Rectangle().fill(theme.border).frame(width: 2) }
                    case .list(let items):
                        VStack(alignment: .leading, spacing: 8) {
                            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                                HStack(alignment: .firstTextBaseline, spacing: 10) {
                                    Text(item.marker).foregroundStyle(theme.muted)
                                        .frame(minWidth: 18, alignment: .trailing)
                                    MarkdownBlocks(blocks: item.blocks)
                                }
                            }
                        }
                    case .table(let table): MarkdownTable(table: table)
                    case .rule: Rule().padding(.vertical, 4)
                    }
                }.background {
                    if measureSections && isSectionStart(block, index: index) {
                        GeometryReader { geometry in
                            Color.clear.preference(key: MarkdownSectionFrames.self,
                                value: [index: geometry.frame(in: .named("markdownSections")).minY])
                        }
                    }
                }
            }
        }.frame(maxWidth: .infinity, alignment: .leading)
    }
    private func isSectionStart(_ block: MarkdownContent.Block, index: Int) -> Bool {
        if index == 0 { return true }
        if case .heading = block { return true }
        return false
    }
}

private struct MarkdownInline: View {
    @Environment(\.mica) private var theme
    let value: AttributedString

    var body: some View {
        Text(styled).tint(theme.accent)
    }

    private var styled: AttributedString {
        var text = value
        for run in Array(text.runs) where run.inlinePresentationIntent?.contains(.code) == true {
            text[run.range].font = .kit(size: 12, design: .monospaced)
            text[run.range].backgroundColor = theme.raised
        }
        return text
    }
}

private struct MarkdownTable: View {
    @Environment(\.mica) private var theme
    let table: MarkdownContent.TableContent

    var body: some View {
        ScrollView(.horizontal) {
            Grid(alignment: .topLeading, horizontalSpacing: 0, verticalSpacing: 0) {
                GridRow(alignment: .top) {
                    cells(table.header, header: true)
                }
                Rule().gridCellUnsizedAxes(.horizontal)
                ForEach(Array(table.rows.enumerated()), id: \.offset) { _, cells in
                    GridRow(alignment: .top) { self.cells(cells, header: false) }
                    Rule().gridCellUnsizedAxes(.horizontal)
                }
            }.fixedSize(horizontal: true, vertical: false)
        }
        .background(theme.surface)
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.border, lineWidth: 1))
    }

    private func cells(_ values: [AttributedString], header: Bool) -> some View {
        ForEach(Array(values.enumerated()), id: \.offset) { index, cell in
            MarkdownInline(value: cell)
                .fontWeight(header ? .semibold : .regular)
                .multilineTextAlignment(textAlignment(index))
                .frame(maxWidth: .infinity, alignment: alignment(index))
                .fixedSize(horizontal: false, vertical: true)
                .padding(10)
                .containerRelativeFrame(.horizontal, alignment: alignment(index)) { width, _ in
                    MarkdownTableSizing.columnWidth(viewport: width, columns: table.header.count)
                }
                .background(header ? theme.raised : theme.surface)
        }
    }

    private func textAlignment(_ index: Int) -> TextAlignment {
        guard table.alignments.indices.contains(index) else { return .leading }
        switch table.alignments[index] {
        case .leading: return .leading
        case .center: return .center
        case .trailing: return .trailing
        }
    }

    private func alignment(_ index: Int) -> Alignment {
        guard table.alignments.indices.contains(index) else { return .leading }
        switch table.alignments[index] {
        case .leading: return .leading
        case .center: return .center
        case .trailing: return .trailing
        }
    }
}

/// Padded columns fill the viewport, overflowing only to preserve readable cell widths.
enum MarkdownTableSizing {
    static let minimumColumnWidth: CGFloat = 180

    static func columnWidth(viewport: CGFloat, columns: Int) -> CGFloat {
        max(minimumColumnWidth, viewport / CGFloat(max(1, columns)))
    }
}

struct CodeBlock: View {
    @Environment(\.mica) private var theme
    let language: String
    let source: String
    @State private var copied = false

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                MetaLabel(text: language.isEmpty ? "CODE" : language.uppercased())
                Spacer()
                Button(copied ? "Copied" : "Copy", systemImage: copied ? "checkmark" : "document.on.document") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(source, forType: .string)
                    copied = true
                }.buttonStyle(.plain).font(.kit(size: 10)).foregroundStyle(theme.muted)
            }.padding(.horizontal, 12).padding(.vertical, 8)
            Rule()
            ScrollView(.horizontal) {
                HighlightedCodeText(language: language, source: source).padding(14).frame(maxWidth: .infinity, alignment: .leading)
            }
        }.background(theme.raised).clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.border, lineWidth: 1))
    }
}

/// Shared native code text for fenced Markdown and expanded tool snippets.
struct HighlightedCodeText: View {
    @Environment(\.mica) private var theme
    let language: String
    let source: String
    var fontSize: CGFloat = 12
    @State private var spans: [SyntaxSpan] = []
    @State private var highlightRequest: CodeHighlightRequest?
    private var request: CodeHighlightRequest { .init(source: source, language: language) }

    private var highlighted: AttributedString {
        let value = NSMutableAttributedString(string: source, attributes: [
            .font: Typography.shared.font(size: fontSize, mono: true),
            .foregroundColor: NSColor(theme.syntax("text", fallback: theme.text))
        ])
        if highlightRequest == request {
            for span in spans {
                let fallback: Color = switch span.role {
                case "comment": theme.muted
                case "string", "escape": theme.success
                case "keyword", "keywordType", "function", "type", "builtin": theme.accent
                default: theme.text
                }
                value.addAttribute(.foregroundColor,
                    value: NSColor(theme.syntax(span.role, fallback: fallback)), range: span.range)
            }
        }
        return AttributedString(value)
    }

    var body: some View {
        Text(highlighted).textSelection(.enabled).multilineTextAlignment(.leading)
            .task(id: request) {
                let current = request
                do {
                    let result = try await CodeSyntaxHighlighter.shared.spans(for: current)
                    guard !Task.isCancelled else { return }
                    spans = result
                    highlightRequest = current
                } catch {
                    Logger(subsystem: AppIdentity.bundleIdentifier, category: "highlighting")
                        .error("Code highlighting unavailable: \(String(describing: error), privacy: .public)")
                }
            }
    }
}
