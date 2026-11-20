import AppKit
@preconcurrency import CodeEditTextView
import SwiftUI

// A waiting comment's chip brings its words into view and opens its
// comment box at them (ADR 0070): it opens or selects the tab they're in,
// opens the tool output holding them, scrolls them to the middle, and
// opens the box at their mark. Views that show words comments can be on
// register as revealers with the session's `TextCommentDraft`.

/// Where words are on screen, to open a comment box at.
struct TextCommentPlace {
    /// The words' box, in screen coordinates.
    let rect: NSRect
    let window: NSWindow
}

/// A view showing words comments can be on, which can bring a comment's
/// words into view.
@MainActor protocol TextCommentRevealer: AnyObject {
    /// Whether it shows, or can open to show, the words of scope.
    func showsTextComments(in scope: String) -> Bool
    /// Brings mark's words in scope into view, opening what holds them, and
    /// says where they are; nil when it can't find them.
    func revealTextComment(_ mark: TextCommentMark, in scope: String) async -> TextCommentPlace?
}

extension WorkspaceState {
    /// Opens a waiting comment from its chip: brings its words into view,
    /// opening or selecting their tab when canOpen allows, and opens its
    /// comment box at them. Where they can't be shown, as in history that's
    /// been let go or a conversation that's ended, the box opens at the chip.
    func openTextComment(_ id: UUID, canOpen: (WorkspacePane) -> Bool, chip: NSRect?, window: NSWindow?,
                         theme: MicaTheme) async {
        let draft = textComments
        guard let item = draft.item(id) else { return }
        draft.revealing += 1
        let turn = draft.revealing
        draft.popover.dismiss()
        func atChip() {
            guard draft.revealing == turn, draft.item(id) != nil, let chip, let window else { return }
            draft.present(quote: item.annotation.quote, source: .init(label: item.annotation.source), editing: id,
                          at: chip, in: window, theme: theme)
        }
        guard let scope = item.scope else { atChip(); return }
        // A tab that was showing has its words already; one that's opened
        // or brought forward may build and load them first.
        var patience = Duration.milliseconds(300)
        if let pane = item.pane {
            if panes.contains(pane) {
                if !isVisible(pane) { select(pane); patience = .seconds(3) }
            } else if canOpen(pane) {
                open(pane); patience = .seconds(3)
            } else {
                atChip(); return
            }
        }
        var revealer = draft.revealer(for: scope)
        let clock = ContinuousClock(), deadline = clock.now + patience
        while revealer == nil, clock.now < deadline, draft.revealing == turn {
            try? await Task.sleep(for: .milliseconds(50))
            revealer = draft.revealer(for: scope)
        }
        guard draft.revealing == turn else { return }
        guard let revealer, let place = await revealer.revealTextComment(item.mark, in: scope),
              draft.revealing == turn, draft.item(id) != nil else { atChip(); return }
        draft.present(quote: item.annotation.quote, source: .init(label: item.annotation.source), editing: id,
                      at: place.rect, in: place.window, theme: theme)
    }
}

/// Finding a mark's words where SwiftUI draws them, in selectable text.
@MainActor enum TextCommentMarkFinder {
    /// The box around mark's words in the topmost field under view that
    /// shows them, in that field's coordinates, and the field.
    static func find(_ mark: TextCommentMark, in view: NSView) -> (field: NSTextField, rect: NSRect)? {
        var found: [(NSTextField, NSRect)] = []
        func visit(_ view: NSView) {
            guard !view.isHidden else { return }
            if let field = view as? NSTextField {
                let boxes = TextLinkHover.boxes(linkingTo: mark.url, in: field)
                if let first = boxes.first { found.append((field, boxes.dropFirst().reduce(first) { $0.union($1) })) }
            }
            view.subviews.forEach(visit)
        }
        visit(view)
        // The first in reading order: highest in the window.
        return found.max { a, b in
            a.0.convert(a.1, to: nil).maxY < b.0.convert(b.1, to: nil).maxY
        }
    }

    /// Scrolls the scroll views inside outer that rect of field is in, as
    /// a tool's output well is, so it shows.
    static func scrollInner(_ rect: NSRect, of field: NSTextField, outside outer: NSView) {
        var scroll = field.enclosingScrollView
        while let current = scroll, !outer.isDescendant(of: current), current !== outer {
            if let document = current.documentView {
                document.scrollToVisible(document.convert(rect, from: field).insetBy(dx: 0, dy: -8))
                current.reflectScrolledClipView(current.contentView)
            }
            scroll = current.enclosingScrollView
        }
    }
}

extension ScratchpadCommentPainter: TextCommentRevealer {
    func showsTextComments(in scope: String) -> Bool {
        scope == TextCommentSource.scratchpadScope && controller?.textView.window != nil
    }

    func revealTextComment(_ mark: TextCommentMark, in scope: String) async -> TextCommentPlace? {
        guard let view = controller?.textView, let window = view.window,
              let range = Self.ranges(of: [mark], in: view.string).first else { return nil }
        view.scrollToRange(range, center: true)
        view.layoutSubtreeIfNeeded()
        let boxes = view.layoutManager.rectsFor(range: range)
        guard let first = boxes.first else { return nil }
        let rect = boxes.dropFirst().reduce(first) { $0.union($1) }
        return TextCommentPlace(rect: window.convertToScreen(view.convert(rect, to: nil)), window: window)
    }
}
