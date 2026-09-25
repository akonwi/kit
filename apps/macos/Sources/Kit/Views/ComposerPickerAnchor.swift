import AppKit
import SwiftUI

/// The composer owns this anchor; completion panels only read its screen bounds.
@MainActor final class ComposerPickerAnchor {
    weak var view: NSView?
    var changed: (() -> Void)?
    var screenFrame: NSRect? {
        guard let view, let window = view.window else { return nil }
        return window.convertToScreen(view.convert(view.bounds, to: nil))
    }
}

struct ComposerPickerAnchorView: NSViewRepresentable {
    let anchor: ComposerPickerAnchor
    func makeNSView(context: Context) -> AnchorView {
        let view = AnchorView()
        view.anchor = anchor; anchor.view = view
        return view
    }
    func updateNSView(_ view: AnchorView, context: Context) { anchor.view = view }

    final class AnchorView: NSView {
        weak var anchor: ComposerPickerAnchor?
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
        override func layout() {
            super.layout()
            // Wait until the SwiftUI layout transaction has positioned the editor too.
            DispatchQueue.main.async { [weak self] in self?.anchor?.changed?() }
        }
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            DispatchQueue.main.async { [weak self] in self?.anchor?.changed?() }
        }
    }
}
