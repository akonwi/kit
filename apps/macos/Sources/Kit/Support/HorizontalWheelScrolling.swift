import AppKit
import SwiftUI

/// Lets a mouse's vertical wheel scroll a horizontal strip, as Shift+wheel
/// does. A horizontal SwiftUI scroll view ignores vertical wheel deltas, so
/// without this a mouse can't reach tabs that overflow.
///
/// Place it as the scroll view's background. While the pointer is over it,
/// the window's vertical mouse-wheel events scroll the strip across instead.
/// Trackpads scroll both axes already, and their events pass through.
struct HorizontalWheelScrolling: NSViewRepresentable {
    func makeNSView(context: Context) -> Region { Region() }
    func updateNSView(_ nsView: Region, context: Context) {}
    static func dismantleNSView(_ nsView: Region, coordinator: ()) { nsView.stop() }

    /// The horizontal equivalent of a vertical mouse-wheel event, or nil
    /// when the event should scroll as it is.
    static func horizontal(_ event: NSEvent) -> NSEvent? {
        guard event.type == .scrollWheel, !event.hasPreciseScrollingDeltas,
              event.scrollingDeltaX == 0, event.scrollingDeltaY != 0,
              !event.modifierFlags.contains(.shift),
              let copy = event.cgEvent?.copy() else { return nil }
        for (vertical, horizontal) in [(CGEventField.scrollWheelEventDeltaAxis1, CGEventField.scrollWheelEventDeltaAxis2),
                                       (.scrollWheelEventPointDeltaAxis1, .scrollWheelEventPointDeltaAxis2)] {
            copy.setIntegerValueField(horizontal, value: copy.getIntegerValueField(vertical))
            copy.setIntegerValueField(vertical, value: 0)
        }
        copy.setDoubleValueField(.scrollWheelEventFixedPtDeltaAxis2,
                                 value: copy.getDoubleValueField(.scrollWheelEventFixedPtDeltaAxis1))
        copy.setDoubleValueField(.scrollWheelEventFixedPtDeltaAxis1, value: 0)
        return NSEvent(cgEvent: copy)
    }

    /// The strip's area. It takes no clicks; it only watches the wheel.
    final class Region: NSView {
        private var token: Token?

        override func hitTest(_ point: NSPoint) -> NSView? { nil }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            stop()
            guard window != nil else { return }
            // The handler holds the token, so a region that goes without
            // being dismantled still removes its monitor.
            let token = Token()
            token.monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] event in
                guard let self else { token.remove(); return event }
                guard event.window === self.window else { return event }
                return self.redirect(event)
            }
            self.token = token
        }

        /// Scrolls the strip under a vertical mouse-wheel event across,
        /// consuming the event; any other event passes on. Only the strip
        /// itself under the pointer counts, not a panel over it. The
        /// horizontal copy goes straight to the strip, as a rebuilt event
        /// has no window for AppKit to route it by.
        func redirect(_ event: NSEvent) -> NSEvent? {
            guard let window, !isHiddenOrHasHiddenAncestor,
                  bounds.contains(convert(event.locationInWindow, from: nil)),
                  let horizontal = HorizontalWheelScrolling.horizontal(event),
                  let hit = window.contentView?.hitTest(event.locationInWindow),
                  let strip = (hit as? NSScrollView) ?? hit.enclosingScrollView,
                  Self.same(strip.convert(strip.bounds, to: nil), convert(bounds, to: nil))
            else { return event }
            strip.scrollWheel(with: horizontal)
            return nil
        }

        func stop() {
            token?.remove()
            token = nil
        }

        /// The region is the strip's background, so it shares its frame.
        private static func same(_ a: CGRect, _ b: CGRect) -> Bool {
            abs(a.minX - b.minX) < 0.5 && abs(a.minY - b.minY) < 0.5
                && abs(a.width - b.width) < 0.5 && abs(a.height - b.height) < 0.5
        }
    }

    private final class Token {
        var monitor: Any?
        func remove() {
            if let monitor { NSEvent.removeMonitor(monitor) }
            monitor = nil
        }
    }
}
