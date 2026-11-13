import AppKit
import SwiftUI

/// A file tab's find bar, a strip under the tab's header: the field with
/// its count, Match Case (Aa) and Whole Word (ab), previous and next, and
/// Done. In the field Return goes to the next match, Shift-Return to the
/// previous, and Esc closes the bar, leaving the current match selected and
/// the keyboard back in the file.
struct FileFindBar: View {
    @Environment(\.mica) private var theme
    @Bindable var find: FileFind

    var body: some View {
        HStack(spacing: 8) {
            HStack(spacing: 6) {
                IconView(.magnifyingGlass, size: 11).foregroundStyle(theme.muted)
                FindField(find: find)
                Text(find.label).font(.kit(size: 11)).foregroundStyle(theme.muted).lineLimit(1)
                    .fixedSize().accessibilityIdentifier("find-count")
            }
            .padding(.horizontal, 8).frame(height: 24)
            .background(theme.surface, in: RoundedRectangle(cornerRadius: 3))
            .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.accent))
            FindToggle(title: "Aa", help: "Match Case", isOn: $find.matchCase)
            FindToggle(title: "ab", help: "Whole Word", isOn: $find.wholeWord, underlined: true)
            HStack(spacing: 2) {
                FindStepButton(icon: .caretUp, help: "Previous Match (⇧⌘G)") { find.previous() }
                FindStepButton(icon: .caretDown, help: "Next Match (⌘G)") { find.next() }
            }
            .disabled(find.matches.isEmpty)
            Button("Done") { FindField.close(find) }
                .buttonStyle(.plain).font(.kit(size: 11)).foregroundStyle(theme.text)
                .padding(.horizontal, 4)
        }
        .padding(.horizontal, 12).padding(.vertical, 6)
        .background(theme.raised)
        .task(id: find.request) { await find.refresh() }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Find in file")
    }
}

private struct FindToggle: View {
    @Environment(\.mica) private var theme
    let title: String
    let help: String
    @Binding var isOn: Bool
    var underlined = false
    @State private var hovering = false

    var body: some View {
        Button { isOn.toggle() } label: {
            Text(title).font(.kit(size: 11, weight: .medium)).underline(underlined)
                .foregroundStyle(isOn ? theme.accent : hovering ? theme.text : theme.muted)
                .frame(width: 24, height: 20)
                .background(isOn ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(help)
        .accessibilityLabel(help)
        .accessibilityAddTraits(isOn ? .isSelected : [])
    }
}

private struct FindStepButton: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let icon: Icon
    let help: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            IconView(icon, size: 11).foregroundStyle(enabled ? theme.text : theme.strongBorder)
                .frame(width: 20, height: 20).contentShape(Rectangle())
        }
        .buttonStyle(.plain).help(help).accessibilityLabel(help)
    }
}

/// The find bar's text field. AppKit delivers Return and Esc to a field as
/// editing commands, so its delegate takes them before the field editor.
struct FindField: NSViewRepresentable {
    @Environment(\.mica) private var theme
    let find: FileFind

    /// Closes `find`'s bar, giving the keyboard back to the file first so the
    /// workspace never sees it land nowhere.
    @MainActor static func close(_ find: FileFind) {
        if let window = NSApp.keyWindow, window.firstResponder is NSTextView { window.makeFirstResponder(nil) }
        find.close()
    }

    func makeNSView(context: Context) -> NSTextField {
        let field = FindTextField()
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.usesSingleLineMode = true
        field.cell?.wraps = false
        field.cell?.isScrollable = true
        field.delegate = context.coordinator
        field.setContentHuggingPriority(.defaultLow, for: .horizontal)
        field.setAccessibilityLabel("Find")
        return field
    }

    func updateNSView(_ field: NSTextField, context: Context) {
        let coordinator = context.coordinator
        coordinator.find = find
        if field.stringValue != find.query { field.stringValue = find.query }
        field.font = Typography.shared.font(size: 12, mono: true)
        field.textColor = NSColor(theme.text)
        field.placeholderAttributedString = NSAttributedString(string: "Find", attributes: [
            .font: Typography.shared.font(size: 12), .foregroundColor: NSColor(theme.strongBorder)
        ])
        if coordinator.focused != find.focusRequest {
            coordinator.focused = find.focusRequest
            // The field may not be in its window yet when the bar opens.
            DispatchQueue.main.async { [weak field] in
                guard let field, let window = field.window else { return }
                window.makeFirstResponder(field)
                field.currentEditor()?.selectAll(nil)
            }
        }
    }

    func makeCoordinator() -> Coordinator { Coordinator(find: find) }

    @MainActor final class Coordinator: NSObject, NSTextFieldDelegate {
        var find: FileFind
        var focused = 0
        init(find: FileFind) { self.find = find }

        func controlTextDidChange(_ notification: Notification) {
            if let field = notification.object as? NSTextField { find.query = field.stringValue }
        }

        func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
            switch selector {
            case #selector(NSResponder.insertNewline(_:)), #selector(NSResponder.insertLineBreak(_:)):
                if (textView as? FindFieldEditor)?.shifted == true { find.previous() } else { find.next() }
            case #selector(NSResponder.cancelOperation(_:)):
                FindField.close(find)
            default:
                return false
            }
            return true
        }
    }
}

/// The find field, which edits with its own field editor so Return can tell
/// whether Shift is held.
final class FindTextField: NSTextField {
    override class var cellClass: AnyClass? {
        get { FindFieldCell.self }
        set {}
    }
}

final class FindFieldCell: NSTextFieldCell {
    private lazy var editor: FindFieldEditor = {
        let editor = FindFieldEditor()
        editor.isFieldEditor = true
        return editor
    }()

    override func fieldEditor(for controlView: NSView) -> NSTextView? { editor }
}

/// Notes whether Shift is held for the key it's handling.
final class FindFieldEditor: NSTextView {
    private(set) var shifted = false

    override func keyDown(with event: NSEvent) {
        shifted = event.modifierFlags.contains(.shift)
        defer { shifted = false }
        super.keyDown(with: event)
    }
}
