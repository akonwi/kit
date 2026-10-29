import AppKit

/// Places a window's standard traffic lights in the 44 pt chrome row: 16 pt
/// from the leading edge and vertically centered. AppKit re-lays out the
/// titlebar on activation, resize, full-screen and title changes, so the
/// placement follows those changes for the window's lifetime.
@MainActor
final class TrafficLightPlacement: NSObject {
    static let leading: CGFloat = 16
    private weak var window: NSWindow?
    private var observedViews: Set<ObjectIdentifier> = []
    private var placing = false
    private var pending = false

    // Selector-based observers are removed when this object deallocates.
    init(window: NSWindow) {
        self.window = window
        super.init()
        for name in [NSWindow.didResizeNotification, NSWindow.didBecomeKeyNotification,
                     NSWindow.didResignKeyNotification, NSWindow.didExitFullScreenNotification] {
            NotificationCenter.default.addObserver(self, selector: #selector(layoutChanged), name: name, object: window)
        }
        place()
    }

    @objc private func layoutChanged(_ notification: Notification) {
        place()
        // A change reported while AppKit lays out the titlebar, as it does
        // when the title changes, can be undone by the rest of that layout,
        // so the buttons are placed again once it is done.
        guard !pending else { return }
        pending = true
        DispatchQueue.main.async { [weak self] in
            self?.pending = false
            self?.place()
        }
    }

    func place() {
        guard let window, !placing, !window.styleMask.contains(.fullScreen),
              let close = window.standardWindowButton(.closeButton),
              let miniaturize = window.standardWindowButton(.miniaturizeButton),
              let zoom = window.standardWindowButton(.zoomButton),
              let titlebar = close.superview, let container = titlebar.superview else { return }
        placing = true
        defer { placing = false }
        observe([container, titlebar, close])
        let height = SessionChrome.rowHeight
        // Frames are set only when they differ, since setting one reports a
        // change, which places the buttons again.
        let row = NSRect(x: 0, y: window.frame.height - height, width: window.frame.width, height: height)
        if container.frame != row { container.frame = row }
        if titlebar.frame != container.bounds { titlebar.frame = container.bounds }
        let spacing = miniaturize.frame.minX - close.frame.minX
        for (index, button) in [close, miniaturize, zoom].enumerated() {
            let origin = NSPoint(x: Self.leading + CGFloat(index) * spacing,
                                 y: ((height - button.frame.height) / 2).rounded())
            if button.frame.origin != origin { button.setFrameOrigin(origin) }
        }
    }

    private func observe(_ views: [NSView]) {
        for view in views where observedViews.insert(ObjectIdentifier(view)).inserted {
            view.postsFrameChangedNotifications = true
            NotificationCenter.default.addObserver(self, selector: #selector(layoutChanged),
                                                   name: NSView.frameDidChangeNotification, object: view)
        }
    }
}
