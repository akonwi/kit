import AppKit
import CodeEditLanguages
@preconcurrency import CodeEditSourceEditor
import SwiftUI

/// Replace the document on content changes while retaining native selection
/// and scroll state outside that document identity.
struct ReadOnlyFileEditor: View {
    let path: String
    let source: String
    var position: Binding<SourceEditorState>? = nil
    /// Lines to select and centre once, as a followed link asks.
    var lineReveal: FileLineReveal? = nil
    /// Called once `lineReveal` shows.
    var lineRevealed: ((FileLineReveal) -> Void)? = nil
    /// The tab's find bar, whose matches the editor shows.
    var find: FileFind? = nil
    @State private var retainedState = SourceEditorState()
    private struct Document: Hashable { let path: String; let source: String }

    var body: some View {
        FileEditorDocument(path: path, source: source, editorState: position ?? $retainedState,
                           lineReveal: lineReveal, lineRevealed: lineRevealed, find: find)
            .id(Document(path: path, source: source))
            // AppKit's gutter can extend past the representable during scrolling.
            // The file editor owns only the area below the workspace headers.
            .clipped()
    }

    static func language(for path: String) -> CodeLanguage {
        let filename = (path as NSString).lastPathComponent
        let ext = (filename as NSString).pathExtension.lowercased()
        return CodeLanguage.allLanguages.first { $0.extensions.contains(filename) }
            ?? CodeLanguage.allLanguages.first { $0.extensions.contains(ext) }
            ?? .default
    }
}

private struct FileEditorDocument: View {
    @Environment(\.mica) private var theme
    let path: String
    let source: String
    @Binding var editorState: SourceEditorState
    let lineReveal: FileLineReveal?
    let lineRevealed: ((FileLineReveal) -> Void)?
    let find: FileFind?
    @State private var ardHighlighter = ArdHighlightProvider()
    @State private var revealer = LineRevealCoordinator()
    @State private var finder = FileFindPainter()

    static func clamped(_ state: SourceEditorState, source: String) -> SourceEditorState {
        var result = state
        let length = source.utf16.count
        result.cursorPositions = state.cursorPositions?.map { cursor in
            let start = min(max(0, cursor.range.location), length)
            return CursorPosition(range: NSRange(location: start, length: min(max(0, cursor.range.length), length - start)))
        }
        return result
    }

    var body: some View {
        SourceEditor(
            .constant(source), language: ReadOnlyFileEditor.language(for: path),
            configuration: FileEditorTheme.configuration(theme), state: Binding(
                get: { Self.clamped(editorState, source: source) }, set: { editorState = $0 }),
            // One provider for the document's life; nil keeps the editor's own.
            highlightProviders: ArdHighlightProvider.colors(path) ? [ardHighlighter] : nil,
            coordinators: [revealer, finder]
        )
        .accessibilityLabel("Read-only file: \(path)")
        .onChange(of: FindSignal(find), initial: true) {
            finder.update(find: find, source: source, theme: theme)
        }
        .onChange(of: theme) { finder.update(find: find, source: source, theme: theme) }
        .onChange(of: lineReveal, initial: true) {
            revealer.revealed = { [$editorState, lineRevealed] reveal, controller in
                // The editor's own state follows a scroll only later, and
                // would scroll back to where it was in the meantime.
                $editorState.wrappedValue = SourceEditorState(cursorPositions: controller.cursorPositions,
                    scrollPosition: controller.scrollView.contentView.bounds.origin)
                lineRevealed?(reveal)
            }
            revealer.request(lineReveal)
        }
    }
}

/// Selects and centres a followed link's lines in a read-only editor, once
/// it's on screen with a size to centre them in.
@MainActor private final class LineRevealCoordinator: @preconcurrency TextViewCoordinator {
    private weak var controller: TextViewController?
    private var pending: FileLineReveal?
    private var shown: UUID?
    private var frameObserver: NSObjectProtocol?
    var revealed: ((FileLineReveal, TextViewController) -> Void)?

    func prepareCoordinator(controller: TextViewController) {
        self.controller = controller
        schedule()
    }
    func controllerDidAppear(controller: TextViewController) {
        // A request waits for the editor to have a size to centre in.
        if frameObserver == nil, let clip = controller.scrollView?.contentView {
            clip.postsFrameChangedNotifications = true
            frameObserver = NotificationCenter.default.addObserver(forName: NSView.frameDidChangeNotification,
                object: clip, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.attempt() }
            }
        }
        schedule()
    }
    func destroy() {
        if let frameObserver { NotificationCenter.default.removeObserver(frameObserver) }
        frameObserver = nil
    }

    func request(_ reveal: FileLineReveal?) {
        guard let reveal, reveal.id != shown else { return }
        pending = reveal
        schedule()
    }

    private func schedule() {
        DispatchQueue.main.async { [weak self] in self?.attempt() }
    }

    private func attempt() {
        guard let reveal = pending, let controller, controller.isViewLoaded, controller.view.window != nil,
              let clip = controller.scrollView?.contentView, clip.bounds.height > 0 else { return }
        pending = nil
        shown = reveal.id
        FileLineRevealer.reveal(reveal.lines, in: controller)
        revealed?(reveal, controller)
    }
}

@MainActor
enum FileEditorTheme {
    static func configuration(_ theme: MicaTheme, isEditable: Bool = false, wrapLines: Bool = false) -> SourceEditorConfiguration {
        .init(
            appearance: .init(theme: colors(theme), font: Typography.shared.font(size: 12, mono: true),
                              wrapLines: wrapLines, bracketPairEmphasis: nil),
            behavior: .init(isEditable: isEditable, isSelectable: true),
            layout: .init(editorOverscroll: 0),
            peripherals: .init(showGutter: true, showMinimap: false, showReformattingGuide: false, showFoldingRibbon: false)
        )
    }

    static func colors(_ theme: MicaTheme) -> EditorTheme {
        // The editor computes RGB brightness even with its minimap hidden.
        func rgb(_ color: Color) -> NSColor {
            NSColor(color).usingColorSpace(.sRGB) ?? NSColor(srgbRed: 0, green: 0, blue: 0, alpha: 1)
        }
        func attribute(_ name: String, _ fallback: Color) -> EditorTheme.Attribute {
            .init(color: rgb(theme.syntax(name, fallback: fallback)))
        }
        return EditorTheme(
            text: attribute("text", theme.text), insertionPoint: rgb(theme.accent),
            invisibles: .init(color: rgb(theme.muted)), background: rgb(theme.surface),
            lineHighlight: rgb(theme.raised), selection: rgb(theme.accent.opacity(0.2)),
            keywords: attribute("keyword", theme.accent), commands: attribute("function", theme.accent),
            types: attribute("type", theme.accent), attributes: attribute("attribute", theme.accent),
            variables: attribute("variable", theme.text), values: attribute("builtin", theme.accent),
            numbers: attribute("number", theme.accent), strings: attribute("string", theme.success),
            characters: attribute("escape", theme.success), comments: attribute("comment", theme.muted)
        )
    }
}
