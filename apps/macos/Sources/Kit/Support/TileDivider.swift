import AppKit
import SwiftUI

/// The gap between two tiles. AppKit tracks the drag and owns the resize
/// cursor, so a drag in the gap never moves the window or reaches the tiles.
/// It reports the pointer's position along the split's axis, measured from
/// the split's leading edge.
struct TileDivider: NSViewRepresentable {
    let axis: SplitAxis
    /// The gap's offset from the split's leading edge along the axis.
    let offset: CGFloat
    let color: Color
    let onDrag: (CGFloat) -> Void
    let onDoubleClick: () -> Void

    func makeNSView(context: Context) -> Handle { Handle() }

    func updateNSView(_ view: Handle, context: Context) {
        view.axis = axis
        view.offset = offset
        view.color = NSColor(color)
        view.onDrag = onDrag
        view.onDoubleClick = onDoubleClick
        view.window?.invalidateCursorRects(for: view)
    }

    final class Handle: NSView {
        var axis = SplitAxis.horizontal
        var offset: CGFloat = 0
        var color = NSColor.controlAccentColor
        var onDrag: (CGFloat) -> Void = { _ in }
        var onDoubleClick: () -> Void = {}
        private var start: (pointer: CGFloat, offset: CGFloat)?
        private var hovering = false { didSet { needsDisplay = true } }
        private var tracking: NSTrackingArea?

        override var mouseDownCanMoveWindow: Bool { false }
        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

        override func resetCursorRects() {
            addCursorRect(bounds, cursor: axis == .horizontal ? .resizeLeftRight : .resizeUpDown)
        }

        override func updateTrackingAreas() {
            super.updateTrackingAreas()
            if let tracking { removeTrackingArea(tracking) }
            let area = NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .activeInKeyWindow, .inVisibleRect], owner: self)
            addTrackingArea(area)
            tracking = area
        }

        override func mouseEntered(with event: NSEvent) { hovering = true }
        override func mouseExited(with event: NSEvent) { if start == nil { hovering = false } }

        override func mouseDown(with event: NSEvent) {
            if event.clickCount == 2 { onDoubleClick(); return }
            start = (position(event), offset)
            needsDisplay = true
        }

        override func mouseDragged(with event: NSEvent) {
            guard let start else { return }
            // Track relative to the press so the gap doesn't jump under the pointer.
            onDrag(start.offset + bounds.length(axis) / 2 + position(event) - start.pointer)
        }

        override func mouseUp(with event: NSEvent) {
            start = nil
            hovering = bounds.contains(convert(event.locationInWindow, from: nil))
            needsDisplay = true
        }

        override func draw(_ dirtyRect: NSRect) {
            guard hovering || start != nil else { return }
            color.withAlphaComponent(0.7).setFill()
            let line: CGFloat = 2
            switch axis {
            case .horizontal: NSRect(x: (bounds.midX - line / 2).rounded(), y: 0, width: line, height: bounds.height).fill()
            case .vertical: NSRect(x: 0, y: (bounds.midY - line / 2).rounded(), width: bounds.width, height: line).fill()
            }
        }

        private func position(_ event: NSEvent) -> CGFloat {
            let point = event.locationInWindow
            return axis == .horizontal ? point.x : -point.y
        }
    }
}

private extension CGRect {
    func length(_ axis: SplitAxis) -> CGFloat { axis == .horizontal ? width : height }
}
