import AppKit
@preconcurrency import CodeEditSourceEditor
@preconcurrency import CodeEditTextView
import SwiftUI

/// Marks the words in the scratchpad that waiting comments are on (ADR
/// 0070): a wash beneath them and an accent underline, as in a reply.
@MainActor final class ScratchpadCommentPainter: @preconcurrency TextViewCoordinator {
    private(set) weak var controller: TextViewController?
    private(set) var marks: [TextCommentMark] = []
    private var wash = NSColor.clear
    private var line = NSColor.clear

    func prepareCoordinator(controller: TextViewController) {
        self.controller = controller
        controller.textView.drawBackground = { [weak self] rect in
            MainActor.assumeIsolated { self?.draw(in: rect) }
        }
    }
    func controllerDidAppear(controller: TextViewController) { controller.textView.needsDisplay = true }
    func textViewDidChangeText(controller: TextViewController) { controller.textView.needsDisplay = true }
    func destroy() {}

    /// Marks `marks` in `theme`'s colors.
    func update(marks: [TextCommentMark], theme: MicaTheme) {
        let wash = NSColor(theme.accent.opacity(theme.dark ? 0.24 : 0.16)), line = NSColor(theme.accent)
        guard marks != self.marks || wash != self.wash || line != self.line else { return }
        self.marks = marks; self.wash = wash; self.line = line
        controller?.textView.needsDisplay = true
    }

    /// Where `marks` are in `text`. Each is the place it was selected while
    /// the scratchpad is unchanged; once it's edited, the words where they
    /// appear only once.
    static func ranges(of marks: [TextCommentMark], in text: String) -> [NSRange] {
        marks.flatMap { mark in
            let found = mark.ranges(in: text)
            guard found.isEmpty, mark.locator != nil else { return found }
            let all = TextCommentMark(id: mark.id, quote: mark.quote).ranges(in: text)
                .filter { (text as NSString).substring(with: $0) == mark.quote }
            return all.count == 1 ? all : []
        }
    }

    func draw(in rect: NSRect) {
        guard let controller, !marks.isEmpty, let manager = controller.textView.layoutManager else { return }
        for range in Self.ranges(of: marks, in: controller.textView.string) {
            for box in manager.rectsFor(range: range) where box.intersects(rect) {
                wash.setFill()
                NSBezierPath(roundedRect: box, xRadius: 2, yRadius: 2).fill()
                line.setFill()
                NSRect(x: box.minX, y: box.maxY - 1, width: box.width, height: 1).fill()
            }
        }
    }
}
