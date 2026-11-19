import AppKit
import KitTranscript
import SwiftUI

// Comments on selected text (ADR 0070). A view that can be commented on
// says what it is with a `CommentSourceRegion` behind it, or, for an editor
// whose lines take server annotations, by registering its text view with
// `SelectionLineComments`. Selections elsewhere, such as in a terminal,
// offer no Comment.

/// Where selected words came from, as their comment names it, and what to
/// mark them in while the comment waits.
struct TextCommentSource: Equatable {
    /// "Assistant reply", "Tool output · go test ./…", "Scratchpad".
    let label: String
    /// The scope the words are marked in, such as a transcript message's
    /// ID; nil where they aren't marked.
    var scope: String? = nil

    /// The scope of a tool call's output.
    static func toolScope(_ id: String) -> String { "tool:" + id }
    /// The scope of a shell run's output.
    static func shellScope(_ id: String) -> String { "shell:" + id }
    /// The scope of the scratchpad.
    static let scratchpadScope = "scratchpad"
}

/// Marks the view it's behind as a source of text comments, or, with
/// lines, as source lines a selection's comment annotates.
struct CommentSourceRegion: NSViewRepresentable {
    var source: TextCommentSource?
    /// How to begin a line annotation on the region's lines, or nil when
    /// they can't take one now.
    var lines: (() -> (() -> Void)?)? = nil
    func makeNSView(context: Context) -> CommentSourceView { CommentSourceView() }
    func updateNSView(_ view: CommentSourceView, context: Context) { view.source = source; view.lines = lines }
}

final class CommentSourceView: NSView {
    var source: TextCommentSource?
    var lines: (() -> (() -> Void)?)?
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    /// The smallest region in view over point, in window coordinates.
    static func containing(_ point: NSPoint, in view: NSView) -> CommentSourceView? {
        var best: CommentSourceView?
        func visit(_ view: NSView) {
            guard !view.isHidden else { return }
            if let region = view as? CommentSourceView, region.source != nil || region.lines != nil,
               region.bounds.contains(region.convert(point, from: nil)),
               best.map({ region.bounds.width * region.bounds.height < $0.bounds.width * $0.bounds.height }) ?? true {
                best = region
            }
            view.subviews.forEach(visit)
        }
        visit(view)
        return best
    }
}

/// Text views whose selections become line annotations (ADR 0022): each
/// registers how to begin one for a selected range.
@MainActor enum SelectionLineComments {
    /// How to begin a line annotation on the range, or nil when the range
    /// can't take one now.
    typealias Begin = (NSRange) -> (() -> Void)?

    private static let handlers = NSMapTable<NSView, Handler>.weakToStrongObjects()

    private final class Handler {
        let begin: Begin
        init(_ begin: @escaping Begin) { self.begin = begin }
    }

    static func register(_ view: NSView, begin: @escaping Begin) { handlers.setObject(Handler(begin), forKey: view) }
    static func unregister(_ view: NSView) { handlers.removeObject(forKey: view) }

    /// How to begin a line annotation on a selection in view or a view it's in.
    static func handler(for view: NSView?) -> Begin? {
        var current = view
        while let view = current {
            if let handler = handlers.object(forKey: view) { return handler.begin }
            current = view.superview
        }
        return nil
    }

    /// The 0-based lines the UTF-16 range of content covers. A selection
    /// ending at the start of a line doesn't take that line.
    static func lines(of range: NSRange, in content: String) -> ClosedRange<Int>? {
        let text = content as NSString
        guard range.location >= 0, range.length > 0, NSMaxRange(range) <= text.length else { return nil }
        func line(at offset: Int) -> Int {
            var count = 0, index = 0
            while index < offset {
                let found = text.range(of: "\n", options: [], range: NSRange(location: index, length: offset - index))
                guard found.location != NSNotFound else { break }
                count += 1; index = found.location + 1
            }
            return count
        }
        let first = line(at: range.location)
        var end = NSMaxRange(range)
        if end > range.location, text.character(at: end - 1) == 10 { end -= 1 }
        return first...max(first, line(at: end))
    }
}

extension EnvironmentValues {
    /// The words waiting comments are on, to mark where they're shown.
    @Entry var textCommentMarks: [TextCommentMark] = []
    /// The session's waiting comments on selected text, where words can be
    /// commented on and marked.
    @Entry var textComments: TextCommentDraft? = nil
}

/// Waiting comments' words in rendered text: an accent wash and underline,
/// and a link that reopens the comment.
enum TextCommentMarking {
    /// text with marks applied. A mark with a locator applies only where
    /// text, or shown, the text as it's drawn, is what its words were
    /// selected in.
    @MainActor static func marked(_ text: AttributedString, _ marks: [TextCommentMark], theme: MicaTheme,
                                  shown: String? = nil) -> AttributedString {
        var result = text
        let plain = String(text.characters)
        for mark in marks {
            var ranges = mark.ranges(in: plain)
            if ranges.isEmpty, let shown, let locator = mark.locator, locator.context == shown {
                ranges = TextCommentMark(id: mark.id, quote: mark.quote,
                                         locator: TextCommentLocator(context: plain, occurrence: locator.occurrence))
                    .ranges(in: plain)
            }
            for found in ranges {
                guard let swiftRange = Range(found, in: plain) else { continue }
                let start = plain.distance(from: plain.startIndex, to: swiftRange.lowerBound)
                let length = plain.distance(from: swiftRange.lowerBound, to: swiftRange.upperBound)
                let lower = result.characters.index(result.startIndex, offsetBy: start)
                let range = lower..<result.characters.index(lower, offsetBy: length)
                // A link's own text keeps its link.
                guard result[range].runs.allSatisfy({ $0.link == nil }) else { continue }
                result[range].backgroundColor = theme.accent.opacity(theme.dark ? 0.24 : 0.16)
                result[range].underlineStyle = Text.LineStyle(pattern: .solid, color: theme.accent)
                result[range].foregroundColor = theme.text
                result[range].link = mark.url
            }
        }
        return result
    }
}

/// Every session's waiting comments, for a mark's tooltip to find its
/// comment.
@MainActor enum TextCommentDrafts {
    private static let drafts = NSHashTable<TextCommentDraft>.weakObjects()
    static func register(_ draft: TextCommentDraft) { drafts.add(draft) }

    /// The draft holding the comment id, and the comment.
    static func find(_ id: UUID) -> (draft: TextCommentDraft, item: TextCommentDraft.Item)? {
        for draft in drafts.allObjects { if let item = draft.item(id) { return (draft, item) } }
        return nil
    }
}

extension TextCommentDraft {
    /// Opens the comment box beside rect, a selection on screen: a new
    /// comment on quote from source, or, with editing, that waiting comment.
    func present(quote: String, source: TextCommentSource, locator: TextCommentLocator? = nil, editing: UUID? = nil,
                 at rect: NSRect, in window: NSWindow, theme: MicaTheme) {
        let existing = editing.flatMap(item)
        let editor = TextCommentEditor(comment: existing?.annotation.comment ?? "", editing: editing)
        let popover = popover
        let roomBelow = rect.minY - (window.screen?.visibleFrame.minY ?? 0)
        let box = TextCommentBox(editor: editor, quote: existing?.annotation.quote ?? quote, submit: { [weak self] in
            guard let self else { return }
            if let editing { self.update(editing, comment: editor.comment) }
            else { self.add(comment: editor.comment, source: source.label, quote: quote, scope: source.scope, locator: locator) }
            popover.dismiss()
        }, cancel: { popover.dismiss() }, delete: editing.map { id in { [weak self] in
            self?.remove(id); popover.dismiss()
        } })
            .popoverCard(theme)
            .environment(\.mica, theme)
        popover.show(anchor: rect, parent: window, edge: roomBelow < 180 ? .above : .below, content: box) {}
    }
}

extension OpenURLAction {
    /// action, but a waiting comment's mark reopens its comment where the
    /// pointer is.
    @MainActor static func reopeningTextComments(_ action: OpenURLAction, theme: MicaTheme) -> OpenURLAction {
        OpenURLAction { url in
            guard let id = TextCommentMark.id(in: url) else { action(url); return .handled }
            guard let (draft, _) = TextCommentDrafts.find(id), let window = NSApp.keyWindow else { return .handled }
            let point = NSEvent.mouseLocation
            draft.present(quote: "", source: .init(label: ""), editing: id,
                          at: NSRect(x: point.x - 1, y: point.y - 10, width: 2, height: 20), in: window, theme: theme)
            return .handled
        }
    }
}
