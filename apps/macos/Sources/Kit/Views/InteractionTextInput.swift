import AppKit
import SwiftUI

/// Own native editing commands so Tab navigates questions, not the key-view loop.
struct InteractionTextInput: NSViewRepresentable {
    @Environment(\.mica) private var theme
    @Binding var text: String
    let prompt: String
    let submit: () -> Void
    let previous: () -> Void
    let cancel: () -> Void

    func makeNSView(context: Context) -> NSTextView {
        let view = NSTextView()
        view.isRichText = false
        view.drawsBackground = false
        view.delegate = context.coordinator
        view.textContainerInset = .zero
        view.textContainer?.lineFragmentPadding = 0
        view.textContainer?.widthTracksTextView = true
        view.isHorizontallyResizable = false
        DispatchQueue.main.async { [weak view] in
            guard let view else { return }
            view.window?.makeFirstResponder(view)
        }
        return view
    }

    func updateNSView(_ view: NSTextView, context: Context) {
        context.coordinator.parent = self
        if view.string != text { view.string = text }
        view.font = Typography.shared.font(size: 13)
        view.textColor = NSColor(theme.text)
        view.insertionPointColor = NSColor(theme.text)
        view.setAccessibilityLabel(prompt)
    }

    func makeCoordinator() -> Coordinator { Coordinator(self) }
    final class Coordinator: NSObject, NSTextViewDelegate {
        var parent: InteractionTextInput
        init(_ parent: InteractionTextInput) { self.parent = parent }
        func textDidChange(_ notification: Notification) {
            if let view = notification.object as? NSTextView { parent.text = view.string }
        }
        func textView(_ textView: NSTextView, doCommandBy selector: Selector) -> Bool {
            switch selector {
            case #selector(NSResponder.insertNewline(_:)), #selector(NSResponder.insertTab(_:)):
                parent.submit()
            case #selector(NSResponder.insertBacktab(_:)): parent.previous()
            case #selector(NSResponder.cancelOperation(_:)): parent.cancel()
            default: return false
            }
            return true
        }
    }
}
