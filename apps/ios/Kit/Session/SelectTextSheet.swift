import KitHighlighting
import SwiftUI
import UIKit

/// A message opened to select part of it, as SwiftUI text on iOS can only be
/// copied whole.
struct SelectTextSheet: View {
    let text: String
    @Environment(\.dismiss) private var dismiss
    /// Each code segment's syntax spans, by segment index, once highlighted.
    @State private var spans: [Int: [SyntaxSpan]] = [:]

    var body: some View {
        let segments = Self.segments(text)
        NavigationStack {
            SelectableText(content: .init(segments: segments, spans: spans))
                .ignoresSafeArea(edges: .bottom)
                .background(Theme.surface)
                .navigationTitle("Select Text")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done", role: .confirm) { dismiss() }
                    }
                }
        }
        .accessibilityIdentifier("select-text")
        .task(id: text) {
            var found: [Int: [SyntaxSpan]] = [:]
            for (index, segment) in segments.enumerated() where segment.code {
                if let result = await CodeColoring.spans(for: .init(source: segment.text, language: segment.language)) {
                    found[index] = result
                }
            }
            if !Task.isCancelled, !found.isEmpty { spans = found }
        }
    }

    /// A run of the message: prose, or the lines of a fenced code block and
    /// its fence's language.
    struct Segment: Equatable {
        let text: String
        let code: Bool
        var language = ""
    }

    /// The message without inline Markdown: emphasis, code spans, and links
    /// read as their text. Lines and block markers stay as written, and
    /// fenced code keeps its lines without its fences.
    nonisolated static func plainText(_ markdown: String) -> String {
        segments(markdown).map(\.text).joined(separator: "\n")
    }

    /// The message as prose and code runs, in order, as `plainText` reads it.
    nonisolated static func segments(_ markdown: String) -> [Segment] {
        var output: [Segment] = [], lines: [String] = []
        var fence: String?, language = ""
        func flush(code: Bool) {
            guard !lines.isEmpty else { return }
            let text = lines.joined(separator: "\n")
            output.append(Segment(text: code ? text : inline(text), code: code, language: code ? language : ""))
            lines = []
        }
        for line in markdown.components(separatedBy: "\n") {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if let open = fence {
                if trimmed.hasPrefix(open), trimmed.allSatisfy({ String($0) == open.prefix(1) }) {
                    flush(code: true)
                    fence = nil
                } else {
                    lines.append(line)
                }
            } else if trimmed.hasPrefix("```") || trimmed.hasPrefix("~~~") {
                flush(code: false)
                fence = String(trimmed.prefix(3))
                language = trimmed.drop { $0 == "`" || $0 == "~" }.trimmingCharacters(in: .whitespaces)
            } else {
                lines.append(line)
            }
        }
        // An unclosed fence runs to the end of the message.
        flush(code: fence != nil)
        return output
    }

    nonisolated private static func inline(_ markdown: String) -> String {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        guard let parsed = try? AttributedString(markdown: markdown, options: options) else { return markdown }
        return String(parsed.characters)
    }
}

/// Read-only text with the system's selection handles and menu. Code runs
/// are monospaced and colored by their syntax.
private struct SelectableText: UIViewRepresentable {
    struct Content: Equatable {
        let segments: [SelectTextSheet.Segment]
        /// Each code segment's syntax spans, by segment index.
        var spans: [Int: [SyntaxSpan]] = [:]
    }

    let content: Content

    func makeUIView(context: Context) -> UITextView {
        let view = UITextView()
        view.isEditable = false
        view.isSelectable = true
        view.backgroundColor = .clear
        view.textContainerInset = UIEdgeInsets(top: 16, left: 16, bottom: 32, right: 16)
        view.adjustsFontForContentSizeCategory = true
        view.dataDetectorTypes = [.link]
        view.accessibilityIdentifier = "select-text-body"
        return view
    }

    func updateUIView(_ view: UITextView, context: Context) {
        guard context.coordinator.shown != content else { return }
        context.coordinator.shown = content
        view.attributedText = Self.text(content)
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    final class Coordinator {
        var shown: Content?
    }

    static func text(_ content: Content) -> NSAttributedString {
        let segments = content.segments
        let prose = NSMutableParagraphStyle()
        prose.lineSpacing = 4
        let code = NSMutableParagraphStyle()
        code.lineSpacing = 2
        let metrics = UIFontMetrics(forTextStyle: .body)
        let result = NSMutableAttributedString()
        for (index, segment) in segments.enumerated() {
            let attributes: [NSAttributedString.Key: Any] = [
                .font: segment.code
                    ? metrics.scaledFont(for: .monospacedSystemFont(ofSize: 14, weight: .regular))
                    : metrics.scaledFont(for: .systemFont(ofSize: 17)),
                .foregroundColor: UIColor(Theme.text),
                .paragraphStyle: segment.code ? code : prose,
            ]
            if index > 0 { result.append(NSAttributedString(string: "\n", attributes: attributes)) }
            let start = result.length
            result.append(NSAttributedString(string: segment.text, attributes: attributes))
            for span in content.spans[index] ?? [] where NSMaxRange(span.range) <= (segment.text as NSString).length {
                result.addAttribute(.foregroundColor, value: UIColor(CodeColoring.color(span.role)),
                                    range: NSRange(location: start + span.range.location, length: span.range.length))
            }
        }
        return result
    }
}

/// A selectable text for the session screen to open in a sheet.
struct SelectedText: Identifiable {
    let id = UUID()
    let text: String
}

extension EnvironmentValues {
    /// Opens a message's text to select from.
    @Entry var selectText: @MainActor (String) -> Void = { _ in }
}
