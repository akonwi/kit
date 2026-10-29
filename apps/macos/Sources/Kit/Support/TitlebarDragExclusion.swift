import AppKit
import SwiftUI

/// Keeps a drag that starts on content in the window's title bar from
/// moving the window, so the content's own drag gesture gets it.
///
/// With a hidden title bar, the top of the window is still its title bar.
/// AppKit hands the window server a drag region there, and the window
/// server moves the window for a drag that starts inside it before the app
/// sees the press. AppKit leaves a view out of that region only when the
/// view can't move the window, accepts first responder, and overrides
/// `mouseDown(with:)`; SwiftUI's views do none of these. Place this as the
/// background of the content, such as a top session tab, to cut its area
/// out of the drag region. It takes no clicks and never becomes first
/// responder, so the content above it handles the press as anywhere else.
struct TitlebarDragExclusion: NSViewRepresentable {
    func makeNSView(context: Context) -> Region { Region() }
    func updateNSView(_ nsView: Region, context: Context) {}

    final class Region: NSView {
        override var mouseDownCanMoveWindow: Bool { false }
        /// AppKit cuts a title-bar view out of the drag region only if it
        /// accepts first responder; `becomeFirstResponder` keeps it from
        /// ever taking focus.
        override var acceptsFirstResponder: Bool { true }
        override func becomeFirstResponder() -> Bool { false }
        override var canBecomeKeyView: Bool { false }
        /// Overridden for the drag region; the region never gets a press,
        /// as it isn't hit.
        override func mouseDown(with event: NSEvent) { nextResponder?.mouseDown(with: event) }
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
    }
}
