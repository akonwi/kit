import AppKit
import ObjectiveC

/// Compatibility guard for CodeEditTextView's unchecked NSRange.intersection.
/// AX clients can request NSNotFound or an overflowing range; neither is a
/// document range. Keep valid requests on the editor's original implementation.
@MainActor
enum EditorAccessibility {
    static func install() { _ = installation }

    private static let installation: Void = {
        guard let editorClass = NSClassFromString("CodeEditTextView.TextView"),
              let method = class_getInstanceMethod(editorClass, #selector(NSView.accessibilityFrame(for:))) else { return }
        typealias Frame = @convention(c) (AnyObject, Selector, NSRange) -> NSRect
        let original = unsafeBitCast(method_getImplementation(method), to: Frame.self)
        let selector = #selector(NSView.accessibilityFrame(for:))
        let guarded: @convention(block) (AnyObject, NSRange) -> NSRect = { view, range in
            guard range.location != NSNotFound, range.location >= 0, range.length >= 0,
                  range.length <= Int.max - range.location else { return .zero }
            return original(view, selector, range)
        }
        method_setImplementation(method, imp_implementationWithBlock(guarded))
    }()
}
