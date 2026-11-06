import SwiftUI
import UIKit

/// The scratchpad's Markdown source in a monospaced text view, with
/// headings in bold. Typing reports each change; a new `version` replaces
/// the text, as when the draft gives way to the shared copy.
struct ScratchpadEditor: UIViewRepresentable {
    let text: String
    let version: Int
    let edit: (String) -> Void

    static let inset = UIEdgeInsets(top: 12, left: 20, bottom: 32, right: 20)
    static var placeholderFont: Font { Font(font(bold: false)) }

    static func font(bold: Bool) -> UIFont {
        UIFontMetrics(forTextStyle: .body)
            .scaledFont(for: .monospacedSystemFont(ofSize: 14, weight: bold ? .bold : .regular))
    }

    func makeUIView(context: Context) -> UITextView {
        let view = UITextView()
        view.backgroundColor = .clear
        view.textContainerInset = Self.inset
        view.textContainer.lineFragmentPadding = 0
        view.autocorrectionType = .no
        view.autocapitalizationType = .none
        view.smartQuotesType = .no
        view.smartDashesType = .no
        view.keyboardDismissMode = .interactive
        view.alwaysBounceVertical = true
        view.delegate = context.coordinator
        view.typingAttributes = Self.attributes(bold: false)
        view.accessibilityIdentifier = "scratchpad-editor"
        view.accessibilityLabel = "Scratchpad"
        return view
    }

    func updateUIView(_ view: UITextView, context: Context) {
        let coordinator = context.coordinator
        coordinator.edit = edit
        // Typing already shows its text; replace it only when it was
        // replaced underneath, or on first show.
        guard coordinator.version != version || view.text != text && !view.isFirstResponder else { return }
        coordinator.version = version
        let selection = view.selectedRange
        view.attributedText = Self.styled(text)
        let length = (text as NSString).length
        view.selectedRange = NSRange(location: min(selection.location, length), length: 0)
    }

    func makeCoordinator() -> Coordinator { Coordinator(edit: edit) }

    final class Coordinator: NSObject, UITextViewDelegate {
        var edit: (String) -> Void
        var version: Int?
        /// Where the pending edit's new text goes, and how long it is.
        private var changed: NSRange?

        init(edit: @escaping (String) -> Void) { self.edit = edit }

        func textView(_ view: UITextView, shouldChangeTextIn range: NSRange, replacementText text: String) -> Bool {
            changed = NSRange(location: range.location, length: (text as NSString).length)
            return true
        }

        func textViewDidChange(_ view: UITextView) {
            // Restyle the lines the edit touched, a paste's included;
            // headings come and go as `#` does.
            let storage = view.textStorage
            let string = storage.string as NSString
            var touched = changed ?? view.selectedRange
            changed = nil
            touched.location = min(touched.location, string.length)
            touched.length = min(touched.length, string.length - touched.location)
            let lines = string.paragraphRange(for: touched)
            storage.beginEditing()
            ScratchpadEditor.style(storage, in: lines)
            storage.endEditing()
            view.typingAttributes = ScratchpadEditor.attributes(bold: false)
            edit(view.text)
        }
    }

    static func attributes(bold: Bool) -> [NSAttributedString.Key: Any] {
        let font = font(bold: bold)
        let paragraph = NSMutableParagraphStyle()
        // 24 pt lines at the default size.
        paragraph.lineSpacing = max(0, font.pointSize * 24 / 14 - font.lineHeight)
        return [.font: font, .foregroundColor: UIColor(Theme.text), .paragraphStyle: paragraph]
    }

    static func styled(_ text: String) -> NSAttributedString {
        let result = NSMutableAttributedString(string: text)
        style(result, in: NSRange(location: 0, length: result.length))
        return result
    }

    /// Styles each line in `range`: bold when it's a Markdown heading.
    static func style(_ text: NSMutableAttributedString, in range: NSRange) {
        let string = text.string as NSString
        string.enumerateSubstrings(in: range, options: [.byParagraphs, .substringNotRequired]) { _, line, enclosing, _ in
            let heading = isHeading(string.substring(with: line))
            text.setAttributes(attributes(bold: heading), range: enclosing)
        }
    }

    /// Whether a line is an ATX heading: one to six `#` and then a space or nothing.
    static func isHeading(_ line: String) -> Bool {
        let hashes = line.prefix { $0 == "#" }.count
        guard (1...6).contains(hashes) else { return false }
        let rest = line.dropFirst(hashes)
        return rest.isEmpty || rest.first == " "
    }
}
