import AppKit

/// Session activity only; window identity and the session name remain stable.
enum SessionTabStatus: String, Decodable, Sendable {
    case idle, running, awaitingResponse

    var label: String? {
        switch self {
        case .idle: nil
        case .running: "Working"
        case .awaitingResponse: "Awaiting response"
        }
    }
}

@MainActor enum SessionTabPresentation {
    static func update(_ window: NSWindow, title: String, status: SessionTabStatus) {
        window.tab.title = title
        window.tab.toolTip = status.label.map { "\(title) — \($0)" } ?? title
        guard status != .idle else {
            window.tab.accessoryView = nil
            return
        }
        if window.tab.accessoryView?.accessibilityLabel() == status.label { return }
        let view: NSView
        if status == .running {
            let spinner = TabActivitySpinner()
            view = spinner
        } else {
            let indicator = NSImageView()
            indicator.image = NSImage(systemSymbolName: "questionmark.circle.fill", accessibilityDescription: status.label)
            indicator.contentTintColor = .secondaryLabelColor
            view = indicator
        }
        view.frame = NSRect(x: 0, y: 0, width: 14, height: 14)
        view.setAccessibilityLabel(status.label)
        view.toolTip = status.label
        window.tab.accessoryView = view
        NSLayoutConstraint.activate([
            view.widthAnchor.constraint(equalToConstant: 14),
            view.heightAnchor.constraint(equalToConstant: 14)
        ])
    }
}

/// Native tab accessories draw through AppKit rather than a SwiftUI canvas host.
@MainActor final class TabActivitySpinner: NSView {
    private var timer: Timer?
    override var isFlipped: Bool { true }
    override var intrinsicContentSize: NSSize { NSSize(width: 14, height: 14) }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        timer?.invalidate(); timer = nil
        guard window != nil else { return }
        let timer = Timer(timeInterval: KitSpinner.interval, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.needsDisplay = true }
        }
        self.timer = timer
        RunLoop.main.add(timer, forMode: .common)
        needsDisplay = true
    }

    override func draw(_ dirtyRect: NSRect) {
        NSColor.secondaryLabelColor.setFill()
        let frame = NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
            ? KitSpinner.frames[0] : KitSpinner.frame(at: Date.timeIntervalSinceReferenceDate)
        for rect in KitSpinner.dots(for: frame) { NSBezierPath(ovalIn: rect).fill() }
    }
}
