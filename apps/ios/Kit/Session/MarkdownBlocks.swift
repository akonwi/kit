import KitClient
import KitTranscript
import SwiftUI

/// A message's Markdown, drawn block by block from the shared projection.
struct MarkdownBlocks: View {
    let source: String

    var body: some View {
        BlockList(blocks: MarkdownContentCache.shared.content(for: source).blocks)
    }
}

private struct BlockList: View {
    let blocks: [MarkdownContent.Block]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                MarkdownBlockView(block: block)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

extension EnvironmentValues {
    /// Whether Markdown shows as a document, such as a Markdown file, with
    /// larger headings than a message's.
    @Entry var markdownDocument = false
}

private struct MarkdownBlockView: View {
    let block: MarkdownContent.Block
    @Environment(\.markdownDocument) private var document

    var body: some View {
        switch block {
        case .paragraph(let text):
            Prose(text: text)
        case .heading(let level, let text):
            let (size, line, weight) = Self.heading(level, document: document)
            Text(text)
                .font(.system(size: size, weight: weight))
                .lineSpacing(document ? line - size * 1.2 : 0)
                .foregroundStyle(Theme.text)
                .padding(.top, document && level > 1 ? 6 : 4)
                .accessibilityAddTraits(.isHeader)
        case .code(let language, let source):
            CodeBlock(language: language, source: source)
        case .mermaid(let diagram):
            MermaidBlock(diagram: diagram)
        case .figure(let figure):
            FigureBlockView(block: figure)
        case .quote(let blocks):
            HStack(alignment: .top, spacing: 12) {
                RoundedRectangle(cornerRadius: 1).fill(Theme.strongBorder).frame(width: 2)
                BlockList(blocks: blocks).foregroundStyle(Theme.muted)
            }
            .fixedSize(horizontal: false, vertical: true)
        case .callout(let callout):
            CalloutView(callout: callout)
        case .list(let items):
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(item.marker)
                            .font(.system(size: 16).monospacedDigit())
                            .foregroundStyle(Theme.muted)
                            .frame(minWidth: 14, alignment: .trailing)
                        BlockList(blocks: item.blocks)
                    }
                }
            }
        case .table(let table):
            TableBlock(table: table)
        case .rule:
            Rectangle().fill(Theme.border).frame(height: 0.5).padding(.vertical, 6)
        }
    }
}

extension MarkdownBlockView {
    /// A heading's size, line height, and weight: a message's headings stay
    /// close to its text, on the font's own lines; a document's step down
    /// from a title.
    static func heading(_ level: Int, document: Bool) -> (CGFloat, CGFloat, Font.Weight) {
        if document {
            switch level {
            case 1: return (26, 32, .bold)
            case 2: return (20, 26, .bold)
            default: return (17, 22, .semibold)
            }
        }
        return (level == 1 ? 20 : level == 2 ? 18 : 16, 0, .semibold)
    }
}

/// Body text: 16 points on 24-point lines. A long press on its message
/// copies it or opens it to select from.
struct Prose: View {
    let text: AttributedString
    var size: CGFloat = 16
    var lineHeight: CGFloat = 24

    var body: some View {
        Text(text)
            .font(.system(size: size))
            .lineSpacing(lineHeight - size * 1.2)
            .foregroundStyle(Theme.text)
            .tint(Theme.accent)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityIdentifier("prose")
    }
}

/// A GitHub alert: its kind in its color, then its blocks.
private struct CalloutView: View {
    let callout: Callout

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(callout.title.isEmpty ? callout.kind.label : "\(callout.kind.label) · \(callout.title)")
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(color)
            BlockList(blocks: callout.blocks)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.controlRadius))
        .overlay(alignment: .leading) {
            Rectangle().fill(color).frame(width: 2)
        }
        .clipShape(RoundedRectangle(cornerRadius: Theme.controlRadius))
    }

    private var color: Color {
        switch callout.kind {
        case .note, .important: Theme.accent
        case .tip: Theme.success
        case .warning: Theme.warning
        case .caution: Theme.danger
        }
    }
}

/// A Markdown table, scrolling sideways when it's wider than the screen.
private struct TableBlock: View {
    let table: MarkdownContent.TableContent

    var body: some View {
        ScrollView(.horizontal) {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 8) {
                GridRow {
                    ForEach(Array(table.header.enumerated()), id: \.offset) { index, cell in
                        Text(cell).font(.system(size: 15, weight: .semibold))
                            .gridColumnAlignment(alignment(index))
                    }
                }
                Rectangle().fill(Theme.border).frame(height: 0.5).gridCellUnsizedAxes(.horizontal)
                ForEach(Array(table.rows.enumerated()), id: \.offset) { _, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                            Text(cell).font(.system(size: 15))
                        }
                    }
                }
            }
            .foregroundStyle(Theme.text)
            .fixedSize()
        }
        .scrollIndicators(.hidden)
    }

    private func alignment(_ column: Int) -> HorizontalAlignment {
        guard column < table.alignments.count else { return .leading }
        switch table.alignments[column] {
        case .leading: return .leading
        case .center: return .center
        case .trailing: return .trailing
        }
    }
}
