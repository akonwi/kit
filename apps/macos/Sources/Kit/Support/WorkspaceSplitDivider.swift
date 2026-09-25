import AppKit
import SwiftUI

/// AppKit owns divider tracking; SwiftUI retains every pane at a stable identity
/// above the empty native columns, using the same split fraction for geometry.
struct WorkspaceSplitDivider: NSViewRepresentable {
    @Binding var fraction: Double
    let color: Color

    func makeCoordinator() -> Coordinator { Coordinator(self) }
    func makeNSView(context: Context) -> SplitView {
        let view = SplitView()
        view.isVertical = true
        view.dividerStyle = .thin
        view.addArrangedSubview(NSView())
        view.addArrangedSubview(NSView())
        view.delegate = context.coordinator
        view.fraction = fraction
        return view
    }
    func updateNSView(_ view: SplitView, context: Context) {
        context.coordinator.parent = self
        view.lineColor = NSColor(color)
        view.fraction = fraction
        view.needsDisplay = true
    }
    final class SplitView: NSSplitView {
        var fraction = 0.5
        var lineColor = NSColor.separatorColor
        override var dividerThickness: CGFloat { 5 }
        override func drawDivider(in rect: NSRect) {
            lineColor.setFill()
            NSRect(x: rect.midX.rounded(), y: rect.minY, width: 1, height: rect.height).fill()
        }
        override func resizeSubviews(withOldSize oldSize: NSSize) {
            guard subviews.count == 2 else { return }
            let available = max(0, bounds.width - dividerThickness)
            let left = (available * fraction).rounded()
            subviews[0].frame = NSRect(x: 0, y: 0, width: left, height: bounds.height)
            subviews[1].frame = NSRect(x: left + dividerThickness, y: 0, width: available - left, height: bounds.height)
        }
    }
    final class Coordinator: NSObject, NSSplitViewDelegate {
        var parent: WorkspaceSplitDivider
        init(_ parent: WorkspaceSplitDivider) { self.parent = parent }
        func splitView(_ splitView: NSSplitView, constrainMinCoordinate proposedMinimumPosition: CGFloat, ofSubviewAt dividerIndex: Int) -> CGFloat {
            min(280, splitView.bounds.width * 0.3)
        }
        func splitView(_ splitView: NSSplitView, constrainMaxCoordinate proposedMaximumPosition: CGFloat, ofSubviewAt dividerIndex: Int) -> CGFloat {
            splitView.bounds.width - min(280, splitView.bounds.width * 0.3)
        }
        func splitViewDidResizeSubviews(_ notification: Notification) {
            guard let view = notification.object as? SplitView, view.bounds.width > 5 else { return }
            let value = view.subviews[0].frame.width / (view.bounds.width - 5)
            view.fraction = value
            guard abs(parent.fraction - value) > 0.001 else { return }
            // Native divider tracking occurs outside SwiftUI's update pass.
            DispatchQueue.main.async { [weak self] in self?.parent.fraction = value }
        }
    }
}
