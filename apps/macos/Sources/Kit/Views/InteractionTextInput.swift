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

    func makeNSView(context: Context) -> InteractionInputBox {
        let box = InteractionInputBox()
        box.field.delegate = context.coordinator
        DispatchQueue.main.async { [weak box] in
            guard let box else { return }
            box.window?.makeFirstResponder(box.field)
        }
        return box
    }

    func updateNSView(_ box: InteractionInputBox, context: Context) {
        context.coordinator.parent = self
        let field = box.field
        if field.stringValue != text { field.stringValue = text }
        field.font = Typography.shared.font(size: 13)
        field.textColor = NSColor(theme.text)
        field.isEnabled = context.environment.isEnabled
        field.setAccessibilityLabel(prompt)
    }

    func makeCoordinator() -> Coordinator { Coordinator(self) }
    final class Coordinator: NSObject, NSTextFieldDelegate {
        var parent: InteractionTextInput
        init(_ parent: InteractionTextInput) { self.parent = parent }
        func controlTextDidChange(_ notification: Notification) {
            if let field = notification.object as? NSTextField { parent.text = field.stringValue }
        }
        func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
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

/// The box owns its padding so clicks beside the single-line field still focus it.
final class InteractionInputBox: NSView {
    let field = NSTextField()

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.usesSingleLineMode = true
        field.cell?.wraps = false
        field.cell?.isScrollable = true
        field.translatesAutoresizingMaskIntoConstraints = false
        addSubview(field)
        NSLayoutConstraint.activate([
            field.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 12),
            field.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -12),
            field.centerYAnchor.constraint(equalTo: centerYAnchor)
        ])
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override func mouseDown(with event: NSEvent) {
        guard field.isEnabled else { return }
        window?.makeFirstResponder(field)
        field.selectText(nil)
    }
}
