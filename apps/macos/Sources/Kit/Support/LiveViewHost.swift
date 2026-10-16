import AppKit
import SwiftUI

/// Shows an AppKit view that a model owns and keeps alive, such as a web
/// page or a terminal. The view moves into whichever host shows it, so it
/// keeps its state when its tab moves, its tile changes, or its session tab
/// is switched away and back, and a host going away releases it only if it
/// still holds it.
struct LiveViewHost: NSViewRepresentable {
    let view: NSView

    func makeNSView(context: Context) -> Container { Container() }

    func updateNSView(_ container: Container, context: Context) { container.show(view) }

    static func dismantleNSView(_ container: Container, coordinator: ()) { container.release() }

    final class Container: NSView {
        private weak var shown: NSView?

        override var isFlipped: Bool { true }

        func show(_ view: NSView) {
            if shown !== view { shown.map(release(_:)) }
            shown = view
            // Another host may hand the view over; anything else that holds
            // it, such as the page's full-screen window, keeps it until done.
            guard view.superview !== self, view.superview == nil || view.superview is Container else { return }
            // Taking the view from another host moves it; AppKit keeps its state.
            view.removeFromSuperview()
            view.frame = bounds
            view.autoresizingMask = [.width, .height]
            addSubview(view)
        }

        func release() { shown.map(release(_:)) }

        private func release(_ view: NSView) {
            if view.superview === self { view.removeFromSuperview() }
        }

        override func layout() {
            super.layout()
            for view in subviews where view.frame != bounds { view.frame = bounds }
        }
    }
}
