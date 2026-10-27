import AppKit

/// An off-screen window holding a page the agent acts on while the page
/// isn't in any window, as when its session tab isn't shown. The page lays
/// out and takes native input there, and its dialogs wait until a person
/// shows it. The window is never ordered on screen.
@MainActor final class BrowserAgentStage {
    private let window: NSWindow
    private let view: NSView

    /// Stages `view` when it has no window; returns nil when it has one.
    static func stage(_ view: NSView, size: CGSize) -> BrowserAgentStage? {
        guard view.window == nil else { return nil }
        return BrowserAgentStage(view, size: size)
    }

    private init(_ view: NSView, size: CGSize) {
        self.view = view
        let frame = NSRect(origin: CGPoint(x: -20_000, y: -20_000), size: size)
        window = NSWindow(contentRect: frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        let container = NSView(frame: NSRect(origin: .zero, size: size))
        window.contentView = container
        view.frame = container.bounds
        container.addSubview(view)
    }

    /// Lets the page go, unless a pane has taken it to show meanwhile.
    func release() {
        if view.superview === window.contentView { view.removeFromSuperview() }
        window.close()
    }
}
