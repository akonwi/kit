import AppKit
@preconcurrency import CodeEditSourceEditor
@preconcurrency import CodeEditTextView
import SwiftUI

/// Draws a file tab's find matches in its code editor and carries out the
/// find bar's requests there. Matches are tinted beneath the text, only
/// those on screen, the current one more strongly; showing a match selects
/// it and scrolls it to the middle of the view when it's out of sight.
@MainActor final class FileFindPainter: @preconcurrency TextViewCoordinator {
    private(set) weak var controller: TextViewController?
    private(set) var find: FileFind?
    var matchColor = NSColor.clear
    var currentColor = NSColor.clear
    private var shownReveal = 0
    private var shownDismiss = 0

    /// The tints for a theme: the accent, faint for every match and strong
    /// for the current one.
    static func colors(_ theme: MicaTheme) -> (match: NSColor, current: NSColor) {
        (NSColor(theme.accent.opacity(0.22)), NSColor(theme.accent.opacity(0.55)))
    }

    func prepareCoordinator(controller: TextViewController) { attach(controller) }
    func controllerDidAppear(controller: TextViewController) { sync() }
    func destroy() {}

    /// Paints into `controller`'s text view, which must not draw its
    /// background otherwise; a view that does calls `draw(in:)` itself.
    func attach(_ controller: TextViewController, drawsBackground: Bool = true) {
        self.controller = controller
        if drawsBackground {
            controller.textView.drawBackground = { [weak self] rect in
                MainActor.assumeIsolated { self?.draw(in: rect) }
            }
        }
        connect()
    }

    /// Follows `find`, searching `source`, in `theme`'s colors.
    func update(find: FileFind?, source: String, theme: MicaTheme) {
        (matchColor, currentColor) = Self.colors(theme)
        if self.find !== find {
            self.find = find
            // Requests made before this view showed aren't for it.
            shownReveal = find?.revealRequest ?? 0
            shownDismiss = find?.dismissRequest ?? 0
            connect()
        }
        find?.setPieces([source], corpus: .source)
        sync()
    }

    /// Redraws the tints and carries out requests not yet carried out.
    func sync() {
        guard let controller else { return }
        controller.textView.needsDisplay = true
        guard let find, controller.view.window != nil else { return }
        if find.revealRequest != shownReveal {
            shownReveal = find.revealRequest
            if let match = find.currentMatch { reveal(match.nsRange) }
        }
        if find.dismissRequest != shownDismiss {
            shownDismiss = find.dismissRequest
            controller.view.window?.makeFirstResponder(controller.textView)
        }
    }

    /// The indices of the matches starting in the text on screen, found by
    /// binary search so a file's count doesn't slow drawing.
    func matchesOnScreen() -> Range<Int> {
        guard let controller, let find, find.isOpen, find.corpus == .source,
              let visible = controller.textView.visibleTextRange,
              let first = FileFind.following(find.matches, anchor: FindPosition(piece: 0, offset: visible.location)),
              find.matches[first].range.lowerBound >= visible.location else { return 0..<0 }
        let end = FileFind.following(find.matches, anchor: FindPosition(piece: 0, offset: NSMaxRange(visible)))
        return first..<(end.flatMap { $0 > first ? $0 : nil } ?? find.matches.count)
    }

    /// The tints to draw where `rect` meets the text on screen, in the text
    /// view's coordinates, current or not.
    func tints(in rect: NSRect) -> [(rect: NSRect, current: Bool)] {
        guard let controller, let find else { return [] }
        let manager = controller.textView.layoutManager!
        return matchesOnScreen().flatMap { index in
            manager.rectsFor(range: find.matches[index].nsRange).filter { $0.intersects(rect) }
                .map { ($0, index == find.current) }
        }
    }

    func draw(in rect: NSRect) {
        for tint in tints(in: rect) {
            (tint.current ? currentColor : matchColor).setFill()
            NSBezierPath(roundedRect: tint.rect, xRadius: 2, yRadius: 2).fill()
        }
    }

    /// Selects `range` and, unless it already shows whole, scrolls it to the
    /// middle of the view. Only lines on screen are laid out, so the match's
    /// line places it vertically, and once there, its own box horizontally.
    func reveal(_ range: NSRange) {
        guard let controller else { return }
        controller.setCursorPositions([CursorPosition(range: range)])
        let textView = controller.textView!
        let manager = textView.layoutManager!
        guard let line = manager.textLineForOffset(range.location) else { return }
        let clip = controller.scrollView.contentView
        let inset = clip.contentInsets.top
        let top = line.yPos, bottom = line.yPos + line.height - (manager.blockHeights[line.index] ?? 0)
        if top < clip.bounds.minY + inset || bottom > clip.bounds.maxY {
            let maximum = max(-inset, textView.frame.height - clip.bounds.height)
            let target = (top + bottom) / 2 - (clip.bounds.height + inset) / 2
            clip.scroll(to: NSPoint(x: clip.bounds.minX, y: min(max(target, -inset), maximum)))
            controller.scrollView.reflectScrolledClipView(clip)
            textView.layoutSubtreeIfNeeded()
        }
        let boxes = manager.rectsFor(range: range)
        guard let first = boxes.first else { return }
        let box = boxes.dropFirst().reduce(first) { $0.union($1) }
        let left = clip.bounds.minX + manager.edgeInsets.left
        if box.minX < left || box.maxX > clip.bounds.maxX {
            let maximum = max(0, textView.frame.width - clip.bounds.width)
            let target = box.midX - (clip.bounds.width + manager.edgeInsets.left) / 2
            clip.scroll(to: NSPoint(x: min(max(target, 0), maximum), y: clip.bounds.minY))
            controller.scrollView.reflectScrolledClipView(clip)
        }
    }

    /// Lets the find bar ask where the view is and what it has selected.
    private func connect() {
        guard let find, controller != nil else { return }
        find.visibleAnchor = { [weak self] in
            FindPosition(piece: 0, offset: self?.controller?.textView.visibleTextRange?.location ?? 0)
        }
        find.selectedText = { [weak self] in
            guard let textView = self?.controller?.textView else { return nil }
            let range = textView.selectedRange()
            guard range.location != NSNotFound, range.length > 0 else { return nil }
            return ((textView.string as NSString).substring(with: range), FindPosition(piece: 0, offset: range.location))
        }
    }
}

/// What a view showing a find bar's matches changes with: reading it in a
/// view's body follows each of them.
struct FindSignal: Equatable {
    var open = false
    var searches = 0
    var current: Int?
    var reveal = 0
    var dismiss = 0

    @MainActor init(_ find: FileFind?) {
        guard let find else { return }
        open = find.isOpen
        searches = find.searches
        current = find.current
        reveal = find.revealRequest
        dismiss = find.dismissRequest
    }
}
