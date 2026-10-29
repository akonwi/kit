import AppKit
import OSLog
import SwiftUI

struct MarkdownView: View {
    let source: String
    var onSections: (([TranscriptReadingSection]) -> Void)? = nil
    /// Body text size; the transcript reads at 14 pt, docked details at 12.
    var size: CGFloat = 14
    /// Draws headings at the body size, for summaries inside a detail well.
    var compactHeadings = false

    var body: some View {
        MarkdownBlocks(blocks: MarkdownContentCache.shared.content(for: source).blocks, measureSections: onSections != nil,
                       compactHeadings: compactHeadings, size: size)
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
            .font(.kit(size: size)).textSelection(.enabled)
    }
}

private struct MarkdownBlocks: View {
    @Environment(\.mica) private var theme
    let blocks: [MarkdownContent.Block]
    var measureSections = false
    var compactHeadings = false
    var size: CGFloat = 14

    var body: some View {
        VStack(alignment: .leading, spacing: MarkdownDocumentRow.blockSpacing) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { index, block in
                Group {
                    switch block {
                    case .paragraph(let value): MarkdownInline(value: value).lineSpacing(5)
                    case .heading(let level, let value):
                        MarkdownInline(value: value)
                            .font(.kit(size: compactHeadings ? size : level == 1 ? 20 : level == 2 ? 17 : 15, weight: .semibold))
                            .padding(.top, compactHeadings ? 2 : 4)
                    case .code(let language, let value): CodeBlock(language: language, source: value)
                    case .mermaid(let diagram): MermaidBlock(diagram: diagram)
                    case .figure(let figure): FigureBlockView(block: figure)
                    case .quote(let children):
                        MarkdownBlocks(blocks: children, compactHeadings: compactHeadings, size: size)
                            .padding(.leading, 14)
                            .overlay(alignment: .leading) { Rectangle().fill(theme.border).frame(width: 2) }
                    case .callout(let callout): CalloutBlock(callout: callout, compactHeadings: compactHeadings, size: size)
                    case .list(let items):
                        VStack(alignment: .leading, spacing: MarkdownDocumentRow.itemSpacing) {
                            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                                MarkdownListItem(item: item, compactHeadings: compactHeadings, size: size)
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

/// A GitHub alert as a tinted card: its kind's icon and name, its title,
/// then its blocks. The wash and edge mix the kind's color into the surface,
/// so a theme sets them through its accent, success, warning, and danger
/// colors; note and important share the accent.
private struct CalloutBlock: View {
    @Environment(\.mica) private var theme
    let callout: Callout
    var compactHeadings = false
    var size: CGFloat = 14

    private var color: Color {
        switch callout.kind {
        case .note, .important: theme.accent
        case .tip: theme.success
        case .warning: theme.warning
        case .caution: theme.danger
        }
    }

    private var icon: Icon {
        switch callout.kind {
        case .note: .info
        case .tip: .lightbulb
        case .important: .warningCircle
        case .warning: .warning
        case .caution: .warningOctagon
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                IconView(icon, size: 14).foregroundStyle(color)
                (Text(callout.kind.label).foregroundStyle(color)
                    + Text(callout.title.isEmpty ? "" : " · \(callout.title)").foregroundStyle(theme.text))
                    .font(.kit(size: 13, weight: .semibold))
            }
            if !callout.blocks.isEmpty {
                MarkdownBlocks(blocks: callout.blocks, compactHeadings: compactHeadings, size: size)
            }
        }
        .padding(.horizontal, 16).padding(.top, 12).padding(.bottom, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(theme.surface.mix(with: color, by: theme.dark ? 0.10 : 0.07), in: RoundedRectangle(cornerRadius: 4))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.surface.mix(with: color, by: theme.dark ? 0.35 : 0.30)))
    }
}

private struct MarkdownListItem: View {
    @Environment(\.mica) private var theme
    let item: MarkdownContent.Item
    var compactHeadings = false
    var size: CGFloat = 14

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(item.marker).foregroundStyle(theme.muted)
                .frame(minWidth: 18, alignment: .trailing)
            MarkdownBlocks(blocks: item.blocks, compactHeadings: compactHeadings, size: size)
        }
    }
}

/// One row of a Markdown document shown whole, as a file tab shows one: a
/// top-level block, or one item of a top-level list, so that a long list is
/// built only as far as it is shown. Each row keeps the space a message puts
/// before it, and knows the source lines it shows.
struct MarkdownDocumentRow: Identifiable, Equatable {
    enum Content: Equatable {
        case block(MarkdownContent.Block)
        case item(MarkdownContent.Item)
    }

    static let blockSpacing: CGFloat = 10
    static let itemSpacing: CGFloat = 8

    let id: Int
    let content: Content
    /// The space above the row.
    let gap: CGFloat
    /// The 1-based source lines the row shows, or nil where unknown.
    var lines: ClosedRange<Int>? = nil

    /// The rows of `content`, in order.
    static func rows(_ content: MarkdownContent) -> [Self] {
        var rows: [Self] = []
        for (index, block) in content.blocks.enumerated() {
            let gap: CGFloat = rows.isEmpty ? 0 : blockSpacing
            let lines = content.lines.indices.contains(index) ? content.lines[index] : nil
            if case .list(let items) = block, !items.isEmpty {
                for (offset, item) in items.enumerated() {
                    rows.append(.init(id: rows.count, content: .item(item), gap: offset == 0 ? gap : itemSpacing,
                                      lines: item.lines ?? lines))
                }
            } else {
                rows.append(.init(id: rows.count, content: .block(block), gap: gap, lines: lines))
            }
        }
        return rows
    }

    /// The row a 1-based source line shows in: the last row starting at or
    /// before it, or the first row for a line before them all.
    static func row(of line: Int, in rows: [Self]) -> Int? {
        rows.last { ($0.lines?.lowerBound ?? .max) <= line }?.id ?? rows.first?.id
    }
}

/// The comments on a Markdown file that its preview shows and takes:
/// anchored, as in its source, to source lines, here a block's.
@MainActor struct MarkdownPreviewAnnotations {
    let document: AnnotationDocument
    let state: AnnotationState
    let client: any AnnotationClient
    let session: String
    /// Whether the file as shown can take new comments.
    let canAnnotate: Bool

    /// The current comments on the file as shown.
    var notes: [FileAnnotation] {
        state.records.filter { !$0.stale && document.range($0.anchor) != nil }
    }

    /// Begins a comment on 1-based source lines.
    func begin(_ lines: ClosedRange<Int>) {
        guard let anchor = document.anchor((lines.lowerBound - 1)...(lines.upperBound - 1), side: nil) else { return }
        state.begin(anchor: anchor)
    }

    func edit(_ note: FileAnnotation) {
        let state = state, client = client, session = session
        Task { @MainActor in
            do {
                try await state.refresh(client: client, session: session)
                guard let fresh = state.records.first(where: { $0.id == note.id }), fresh.complete else { return }
                if fresh.stale { state.inspected = fresh } else { state.begin(anchor: fresh.anchor, editing: fresh) }
            } catch { state.report(error) }
        }
    }

    func delete(_ note: FileAnnotation) {
        let state = state, client = client, session = session
        Task { await state.remove(client: client, session: session, id: note.id) }
    }
}

/// A whole Markdown document, such as a file, rendered as the transcript
/// renders a message, in a reading column as wide as the transcript's. It
/// parses off the main actor and builds only the rows in view; while a new
/// source parses, the last one stays. A document past what renders (see
/// `MarkdownFiles.renderable`) says so, offering `showSource`.
///
/// With `annotations`, a hovered block offers a + in the margin that
/// comments on the block's source lines; a saved comment sits under the
/// block holding its last line, and a revealed one is scrolled to.
struct MarkdownDocumentView: View {
    @Environment(\.mica) private var theme
    let source: String
    var annotations: MarkdownPreviewAnnotations? = nil
    var showSource: (() -> Void)? = nil
    /// The reading column's widest.
    static let columnWidth: CGFloat = 844
    /// The margin a row's + sits in.
    static let gutterWidth: CGFloat = 18
    /// The rows of the source last parsed, or nil for one past what renders.
    @State private var parsed: (source: String, rows: [MarkdownDocumentRow]?)?
    @State private var flashing: UInt64?

    enum Entry: Identifiable {
        case row(MarkdownDocumentRow)
        case note(FileAnnotation)
        case editor
        var id: String {
            switch self {
            case .row(let row): "row-\(row.id)"
            case .note(let note): "note-\(note.id)"
            case .editor: "editor"
            }
        }
    }

    /// Each row, then the comments whose last line it shows, with the editor
    /// in place of the comment it edits or after a new one's row.
    func entries(_ rows: [MarkdownDocumentRow]) -> [Entry] {
        guard let annotations else { return rows.map(Entry.row) }
        let editor = annotations.state.editor
        var after: [Int: [Entry]] = [:]
        for note in annotations.notes {
            guard let row = MarkdownDocumentRow.row(of: note.endLine, in: rows) else { continue }
            after[row, default: []].append(editor?.editing == note.id ? .editor : .note(note))
        }
        if let editor, editor.editing == nil, let lines = annotations.document.range(editor.anchor),
           let row = MarkdownDocumentRow.row(of: lines.upperBound + 1, in: rows) {
            after[row, default: []].append(.editor)
        }
        return rows.flatMap { [Entry.row($0)] + (after[$0.id] ?? []) }
    }

    var body: some View {
        Group {
            if let parsed, parsed.rows == nil {
                VStack(spacing: 10) {
                    Text("This file is too large or too deeply nested to render.").foregroundStyle(theme.muted)
                    if let showSource { Button("Show Source", action: showSource).buttonStyle(FormButtonStyle()) }
                }
                .font(.kit(size: 12)).padding(20)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .onChange(of: annotations?.state.revealToken, initial: true) {
                    // A comment revealed on a file that can't render shows in its source.
                    guard let annotations, let id = annotations.state.revealed,
                          annotations.notes.contains(where: { $0.id == id }) else { return }
                    showSource?()
                }
            } else {
                let rows = parsed?.rows ?? []
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 0) {
                            ForEach(entries(rows)) { entry in entryView(entry) }
                        }
                        .frame(maxWidth: Self.columnWidth, alignment: .leading)
                        .padding(.leading, 24 - Self.gutterWidth - 6).padding(.trailing, 24).padding(.vertical, 20)
                        .frame(maxWidth: .infinity)
                    }
                    .font(.kit(size: 14)).textSelection(.enabled)
                    .onChange(of: annotations?.state.revealToken, initial: true) { reveal(rows, proxy: proxy) }
                    .onChange(of: parsed?.source) { reveal(rows, proxy: proxy) }
                }
            }
        }
        .task(id: source) {
            let source = source
            let rows = await Task.detached(priority: .userInitiated) { () -> [MarkdownDocumentRow]? in
                guard MarkdownFiles.renderable(source) else { return nil }
                return MarkdownDocumentRow.rows(MarkdownContent(source: source))
            }.value
            guard !Task.isCancelled else { return }
            parsed = (source, rows)
        }
    }

    @ViewBuilder private func entryView(_ entry: Entry) -> some View {
        switch entry {
        case .row(let row):
            MarkdownPreviewRow(row: row, annotations: annotations).padding(.top, row.gap)
        case .note(let note):
            if let annotations {
                AnnotationNote(annotation: note, edit: { annotations.edit(note) }, delete: { annotations.delete(note) })
                    .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.accent.opacity(flashing == note.id ? 1 : 0), lineWidth: 1))
                    .font(.kit(size: 13)).textSelection(.disabled)
                    .padding(.leading, Self.gutterWidth + 6).padding(.top, 8)
            }
        case .editor:
            if let annotations {
                AnnotationInput(annotations: annotations.state, client: annotations.client, session: annotations.session,
                                cancel: { annotations.state.cancelEditor() })
                    .font(.kit(size: 13))
                    .padding(.leading, Self.gutterWidth + 6).padding(.top, 8)
            }
        }
    }

    /// Scrolls to a comment revealed on this file, briefly outlining it.
    private func reveal(_ rows: [MarkdownDocumentRow], proxy: ScrollViewProxy) {
        guard let annotations, !rows.isEmpty, let id = annotations.state.revealed,
              let note = annotations.notes.first(where: { $0.id == id }) else { return }
        annotations.state.revealed = nil
        flashing = id
        withAnimation { proxy.scrollTo("note-\(id)", anchor: .center) }
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(1.2))
            if flashing == note.id { flashing = nil }
        }
    }
}

/// A preview row: a margin holding, while the pointer is over the row and
/// it can take a comment, a + that comments on the row's source lines, and
/// an accent bar where comments cover them; then the rendered row.
private struct MarkdownPreviewRow: View {
    @Environment(\.mica) private var theme
    let row: MarkdownDocumentRow
    let annotations: MarkdownPreviewAnnotations?
    @State private var hovering = false

    private var lines: ClosedRange<Int>? {
        guard let lines = row.lines, lines.count <= AnnotationSelection.maximumLines else { return nil }
        return lines
    }
    private var annotated: Bool {
        guard let annotations, let lines = row.lines else { return false }
        return annotations.notes.contains { $0.startLine <= lines.upperBound && $0.endLine >= lines.lowerBound }
    }
    private var offers: Bool {
        guard let annotations, lines != nil else { return false }
        let state = annotations.state
        return hovering && annotations.canAnnotate && state.editor == nil && !state.pending && !state.uncertain
    }

    var body: some View {
        HStack(alignment: .top, spacing: 6) {
            ZStack(alignment: .top) {
                if annotated {
                    RoundedRectangle(cornerRadius: 1.5).fill(theme.accent).frame(width: 3).frame(maxHeight: .infinity)
                }
                if offers, let lines, let annotations {
                    Button { annotations.begin(lines) } label: {
                        IconView(.plus, size: 10).foregroundStyle(theme.surface)
                            .frame(width: 16, height: 16)
                            .background(theme.accent, in: RoundedRectangle(cornerRadius: 3))
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help(lines.count == 1 ? "Comment on line \(lines.lowerBound)" : "Comment on lines \(lines.lowerBound)–\(lines.upperBound)")
                    .accessibilityLabel("Comment on this block")
                }
            }
            .frame(width: MarkdownDocumentView.gutterWidth)
            .frame(maxHeight: .infinity, alignment: .top)
            Group {
                switch row.content {
                case .block(let block): MarkdownBlocks(blocks: [block])
                case .item(let item): MarkdownListItem(item: item)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .fixedSize(horizontal: false, vertical: true)
        .contentShape(Rectangle())
        .onHover { hovering = $0 }
    }
}

private struct MarkdownInline: View {
    @Environment(\.mica) private var theme
    @Environment(\.transcriptSessionLink) private var sessionLink
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
        return SessionMentions.presented(text, link: sessionLink, theme: theme)
    }
}

extension SessionMentions {
    /// `text` with each mention link drawn as `#` and its session's title in
    /// accent on the composer's mention tint. A session the app doesn't know
    /// reads `#unknown session`, muted and not a link. Without a session
    /// context, as outside a transcript, the token stays as written.
    @MainActor static func presented(_ text: AttributedString, link: TranscriptSessionLink?, theme: MicaTheme) -> AttributedString {
        var result = text
        // Replace from the end, by offset, so earlier spans stay where they were.
        for (url, original) in text.runs[\.link].reversed() {
            guard let url, let id = session(in: url) else { continue }
            let start = text.characters.distance(from: text.startIndex, to: original.lowerBound)
            let length = text.characters.distance(from: original.lowerBound, to: original.upperBound)
            let lower = result.characters.index(result.startIndex, offsetBy: start)
            let range = lower..<result.characters.index(lower, offsetBy: length)
            guard let link else { result[range].link = nil; continue }
            let title = link.title(id)
            var piece = AttributedString(label(title: title))
            piece.inlinePresentationIntent = text[original].runs.first?.inlinePresentationIntent
            if title != nil {
                piece.link = url
                piece.foregroundColor = theme.accent
                piece.backgroundColor = theme.accent.opacity(0.14)
            } else {
                piece.foregroundColor = theme.muted
                piece.backgroundColor = theme.raised
            }
            result.replaceSubrange(range, with: piece)
        }
        return result
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
            }
            // Rows take their tallest cell's height rather than sharing out any
            // extra height a parent proposes to the cells stretched below.
            .fixedSize()
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
                // Fill the row's height so a row's background is one band
                // when its cells wrap to different heights.
                .frame(maxHeight: .infinity, alignment: .top)
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

/// Whether Markdown code blocks drop their header and copy button, for short
/// details inside a card such as the interaction dock.
private struct CompactCodeBlocksKey: EnvironmentKey { static let defaultValue = false }

extension EnvironmentValues {
    var compactCodeBlocks: Bool {
        get { self[CompactCodeBlocksKey.self] }
        set { self[CompactCodeBlocksKey.self] = newValue }
    }
}

struct CodeBlock: View {
    @Environment(\.mica) private var theme
    @Environment(\.compactCodeBlocks) private var compact
    let language: String
    let source: String

    var body: some View {
        if compact {
            Text(source.hasSuffix("\n") ? String(source.dropLast()) : source)
                .font(.kit(size: 12, design: .monospaced)).foregroundStyle(theme.text).lineSpacing(3)
                .textSelection(.enabled)
                .compactFence()
        } else {
            FenceCard(label: language.isEmpty ? "CODE" : language.uppercased(), copy: source) {
                ScrollView(.horizontal) {
                    HighlightedCodeText(language: language, source: source).padding(14).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
    }
}

/// The card fenced Markdown blocks are shown in: a label and a copy action
/// above their content.
struct FenceCard<Content: View>: View {
    @Environment(\.mica) private var theme
    let label: String
    /// The text the copy action puts on the pasteboard.
    let copy: String
    @ViewBuilder let content: Content
    @State private var copied = false

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                MetaLabel(text: label)
                Spacer()
                Button(copied ? "Copied" : "Copy", icon: copied ? .check : .copy, size: 10) {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(copy, forType: .string)
                    copied = true
                }.buttonStyle(.plain).font(.kit(size: 10)).foregroundStyle(theme.muted)
            }.padding(.horizontal, 12).padding(.vertical, 8)
            Rule()
            content
        }.background(theme.raised).clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.border, lineWidth: 1))
    }
}

/// A fenced block's short, headerless frame inside a card such as the
/// interaction dock.
private struct CompactFence: ViewModifier {
    @Environment(\.mica) private var theme

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, 10).padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
            .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
    }
}

extension View {
    func compactFence() -> some View { modifier(CompactFence()) }
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
