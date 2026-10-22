import AppKit

/// A window whose SwiftUI text fields keep their focus while other tests run.
///
/// SwiftUI edits every text field in the process with one shared field
/// editor. When a text field in another window takes focus, as one in a test
/// running at the same time may, it takes that editor along, and this window
/// is left with no first responder. This window edits with its own instance
/// of the same editor instead.
class FocusIsolatedWindow: NSWindow {
    private var editor: NSText?

    override func fieldEditor(_ createFlag: Bool, for object: Any?) -> NSText? {
        if let editor { return editor }
        guard let shared = super.fieldEditor(true, for: object) as? NSTextView else {
            return super.fieldEditor(createFlag, for: object)
        }
        let own = type(of: shared).init(frame: .zero)
        own.isFieldEditor = true
        editor = own
        return own
    }
}
