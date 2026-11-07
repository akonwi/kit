import AppKit
@preconcurrency import CodeEditSourceEditor
import Foundation
import KitHighlighting
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

/// Ard is colored by tree-sitter-ard: in transcript fences and tool reads
/// through `CodeSyntaxHighlighter`, and in file tabs and diffs through
/// `ArdHighlightProvider`.
@MainActor
@Suite(.serialized)
struct ArdHighlightingTests {
    static let sample = """
    use ard/io
    // Greets someone.
    fn greet(name: Str) Int {
      let count = 42
      io::print("hi")
      true
    }
    """

    /// The role each listed word is colored with in the transcript.
    static let transcriptRoles: [(String, String)] = [
        ("use", "keyword"), ("ard/io", "string"), ("// Greets someone.", "comment"), ("fn", "keyword"),
        ("greet", "function"), ("name", "variable"), ("Str", "keywordType"), ("Int", "keywordType"),
        ("let", "keyword"), ("42", "number"), ("\"hi\"", "string"), ("true", "keyword")
    ]

    private static func roles(of spans: [SyntaxSpan], in source: String) -> [(String, String?)] {
        transcriptRoles.map { word, _ in
            let range = (source as NSString).range(of: word)
            return (word, spans.last { $0.range == range }?.role)
        }
    }

    @Test func anArdFenceIsColored() async throws {
        let content = MarkdownContent(source: "Here:\n\n```ard\n\(Self.sample)\n```\n")
        let fence = try #require(content.blocks.compactMap { block -> (String, String)? in
            if case .code(let language, let source) = block { (language, source) } else { nil }
        }.first)
        #expect(fence.0 == "ard")
        let spans = try await CodeSyntaxHighlighter().spans(for: .init(source: fence.1, language: fence.0))
        let roles = Self.roles(of: spans, in: fence.1)
        #expect(roles.map(\.0) == Self.transcriptRoles.map(\.0))
        #expect(roles.map(\.1) == Self.transcriptRoles.map(\.1))
    }

    @Test func anArdFileReadIsColored() async throws {
        let arguments = #"{"path":"std/greet.ard"}"#
        let read = ToolPresentation(ToolActivity(id: "tool", name: "read", summary: "read", output: Self.sample,
                                                 arguments: arguments, failed: false))
        #expect(read.language == "ard")
        let spans = try await CodeSyntaxHighlighter().spans(for: .init(source: read.readContent, language: read.language))
        #expect(Self.roles(of: spans, in: read.readContent).map(\.1) == Self.transcriptRoles.map(\.1))
    }

    /// The editor colors only the capture names it knows, as it does for
    /// every other language: `function.definition`, `type.builtin`,
    /// `variable.parameter` and `string.special` stay plain.
    @Test func theEditorProviderNamesCapturesStrictly() {
        let source = "fn greet(name: Str) Int {\n  let n = 42 // done\n  \"hi\"\n}"
        let highlights = ArdHighlightProvider.highlights(in: source)
        // In document order, as the editor applies them. tree-sitter-ard's
        // binding and comment nodes start at the whitespace before them.
        let named = highlights.map { ((source as NSString).substring(with: $0.range), $0.capture) }
        #expect(named.map(\.0) == ["fn", "let", " n", "42", " // done", "\"hi\"", "hi"])
        #expect(named.map(\.1) == [.keyword, .keyword, .variable, .number, .comment, .string, .string])
    }

    @Test func onlyArdFilesUseTheArdProvider() {
        #expect(ArdHighlightProvider.colors("std/list.ard"))
        #expect(ArdHighlightProvider.colors("Main.ARD"))
        #expect(!ArdHighlightProvider.colors("main.go"))
        #expect(!ArdHighlightProvider.colors("ard"))
    }

    private static func editors(in view: NSView) -> [TextViewController] {
        if let editor = view.nextResponder as? TextViewController { return [editor] }
        return view.subviews.flatMap { editors(in: $0) }
    }

    /// The foreground color at the first occurrence of a word in an editor.
    private static func color(of word: String, in editor: TextViewController) -> NSColor? {
        let location = (editor.textView.string as NSString).range(of: word).location
        guard location != NSNotFound, let storage = editor.textView.textStorage else { return nil }
        return storage.attribute(.foregroundColor, at: location, effectiveRange: nil) as? NSColor
    }

    private static func expectArdColors(in host: NSView) async throws {
        var editor: TextViewController?
        try await eventually {
            host.layoutSubtreeIfNeeded()
            editor = editors(in: host).first
            guard let editor else { return false }
            return color(of: "42", in: editor) == editor.attributesFor(.number)[.foregroundColor] as? NSColor
        }
        let rendered = try #require(editor)
        func expected(_ capture: CaptureName?) -> NSColor? { rendered.attributesFor(capture)[.foregroundColor] as? NSColor }
        #expect(color(of: "fn", in: rendered) == expected(.keyword))
        #expect(color(of: "let", in: rendered) == expected(.keyword))
        #expect(color(of: "42", in: rendered) == expected(.number))
        #expect(color(of: "// Greets", in: rendered) == expected(.comment))
        #expect(color(of: "\"hi\"", in: rendered) == expected(.string))
        #expect(color(of: "true", in: rendered) == expected(.boolean))
        #expect(color(of: "greet", in: rendered) == expected(nil))
    }

    @Test(arguments: [false, true])
    func anArdFileTabIsColored(dark: Bool) async throws {
        let theme = MicaTheme(dark: dark)
        let host = NSHostingView(rootView: ReadOnlyFileEditor(path: "std/greet.ard", source: Self.sample)
            .environment(\.mica, theme))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 300))
        window.contentView = host; window.orderFront(nil)
        defer { window.close() }
        try await Self.expectArdColors(in: host)
    }

    @Test func anArdDiffIsColored() async throws {
        let file = DiffTestData.file.replacingOccurrences(of: "main.swift", with: "std/greet.ard")
        let lines = Self.sample.split(separator: "\n", omittingEmptySubsequences: false).enumerated().map { index, line in
            let content = String(data: try! JSONEncoder().encode(String(line)), encoding: .utf8)!
            return #"{"kind":"context","oldLine":\#(index + 1),"newLine":\#(index + 1),"content":\#(content),"hasTerminatingLF":true}"#
        }
        let count = lines.count
        let hunk = #"{"oldStart":1,"oldCount":\#(count),"newStart":1,"newCount":\#(count),"continuedBefore":false,"continuedAfter":false,"lines":[\#(lines.joined(separator: ","))]}"#
        let json = "{\"observation\":\(DiffTestData.observation),\"file\":\(file),\"computation\":{\"state\":\"complete\"},\"hunks\":[\(hunk)]}"
        let page = try JSONDecoder().decode(WireFileDiffPage.self, from: Data(json.utf8))
        let document = DiffDocument(observation: page.observation, file: page.file, hunks: try #require(page.hunks))
        let host = NSHostingView(rootView: ArdDiffHarness(document: document).environment(\.mica, MicaTheme(dark: false)))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 700, height: 300))
        window.contentView = host; window.orderFront(nil)
        defer { window.close() }
        try await Self.expectArdColors(in: host)
    }
}

private struct ArdDiffHarness: View {
    let document: DiffDocument
    @State private var position = SourceEditorState()
    @State private var annotations = AnnotationState()
    var body: some View {
        AnnotatedFileEditor(diff: document, position: $position, annotations: annotations,
            client: DiffTestClient(), session: "session_test", canAnnotate: false)
    }
}
