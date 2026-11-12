import AppKit
@preconcurrency import CodeEditSourceEditor
import KitClient
import SwiftUI

/// A request that a file tab show `lines`, as when a link to them is
/// followed. The tab's view carries it out once, then finishes it; following
/// the same link again makes a new one.
struct FileLineReveal: Equatable {
    let lines: FileLineRange
    let id = UUID()
}

/// Selects and centres a file's lines in a code editor.
@MainActor enum FileLineRevealer {
    /// The text `lines` cover in `text`, from the start of the first to the
    /// end of the last, without its line break, in UTF-16 units. A range
    /// running past the text's last line stops there; nil when it starts
    /// past it.
    static func range(of lines: FileLineRange, in text: String) -> NSRange? {
        let string = text as NSString
        var ranges: [NSRange] = []
        var start = 0
        for index in 0..<string.length where string.character(at: index) == 0x0A {
            // A CRLF line's break is two units.
            let end = index > start && string.character(at: index - 1) == 0x0D ? index - 1 : index
            ranges.append(NSRange(location: start, length: end - start))
            start = index + 1
        }
        // A final line break ends the last line rather than starting another.
        if start < string.length || ranges.isEmpty { ranges.append(NSRange(location: start, length: string.length - start)) }
        guard lines.start <= ranges.count else { return nil }
        let first = ranges[lines.start - 1], last = ranges[min(lines.end, ranges.count) - 1]
        return NSRange(location: first.location, length: NSMaxRange(last) - first.location)
    }

    /// Selects `lines` in `controller`'s text, scrolls them to the middle of
    /// its view, or to its top when they don't fit, and makes the text
    /// first responder so the selection shows in the accent. Returns false
    /// when the text has no such lines.
    @discardableResult
    static func reveal(_ lines: FileLineRange, in controller: TextViewController) -> Bool {
        let text = controller.textView.string
        guard let range = range(of: lines, in: text) else { return false }
        controller.setCursorPositions([CursorPosition(range: range)])
        let manager = controller.textView.layoutManager!
        guard let first = manager.textLineForOffset(range.location),
              let last = manager.textLineForOffset(max(range.location, NSMaxRange(range) - (range.length > 0 ? 1 : 0)))
        else { return true }
        let top = first.yPos
        let bottom = last.yPos + last.height - (manager.blockHeights[last.index] ?? 0)
        let clip = controller.scrollView.contentView
        let visible = clip.bounds.height
        let inset = clip.contentInsets.top
        let target = bottom - top > visible ? top - inset : (top + bottom) / 2 - visible / 2
        let maximum = max(-inset, controller.textView.frame.height - visible)
        clip.scroll(to: NSPoint(x: clip.bounds.minX, y: min(max(target, -inset), maximum)))
        controller.scrollView.reflectScrolledClipView(clip)
        if let window = controller.view.window { window.makeFirstResponder(controller.textView) }
        return true
    }
}

/// Calls `action` on each mouse-down within the area it backs, without
/// taking the click from what's there. Put it behind a view.
struct MouseDownMonitor: NSViewRepresentable {
    let action: @MainActor () -> Void

    func makeNSView(context: Context) -> MonitorView { MonitorView(action: action) }
    func updateNSView(_ view: MonitorView, context: Context) { view.action = action }
    static func dismantleNSView(_ view: MonitorView, coordinator: ()) { view.stop() }

    final class MonitorView: NSView {
        var action: @MainActor () -> Void
        private var monitor: Any?

        init(action: @escaping @MainActor () -> Void) {
            self.action = action
            super.init(frame: .zero)
        }
        required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

        override func hitTest(_ point: NSPoint) -> NSView? { nil }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            stop()
            guard window != nil else { return }
            monitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]) { [weak self] event in
                MainActor.assumeIsolated { self?.observe(event) }
                return event
            }
        }

        /// Stops watching for clicks.
        func stop() {
            if let monitor { NSEvent.removeMonitor(monitor) }
            monitor = nil
        }

        /// Whether `event` lands on this view, shown in its window.
        func observe(_ event: NSEvent) {
            guard let window, event.window === window, !isHiddenOrHasHiddenAncestor,
                  visibleRect.contains(convert(event.locationInWindow, from: nil)) else { return }
            action()
        }
    }
}
