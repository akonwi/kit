import AppKit
import SwiftUI
import XCTest
@testable import Kit

@MainActor
final class InteractionTextInputTests: XCTestCase {
    func testSingleLineInputFillsClickableBox() {
        let input = InteractionTextInput(text: .constant(""), prompt: "Branch name",
                                         submit: {}, previous: {}, cancel: {})
        let host = NSHostingView(rootView: input.frame(width: 300, height: 40))
        host.frame = NSRect(x: 0, y: 0, width: 300, height: 40)
        host.layoutSubtreeIfNeeded()

        guard let box = findInputBox(in: host) else {
            return XCTFail("Input box is missing")
        }
        XCTAssertEqual(box.bounds.size, NSSize(width: 300, height: 40))
        XCTAssertTrue(box.field.usesSingleLineMode)
        XCTAssertEqual(box.field.frame.midY, box.bounds.midY, accuracy: 0.5)
        for content in ["", "test", String(repeating: "long answer ", count: 100)] {
            box.field.stringValue = content
            host.layoutSubtreeIfNeeded()
            XCTAssertEqual(box.bounds.height, 40)
            for y in [4.0, 20.0, 36.0] {
                let target = box.hitTest(NSPoint(x: 20, y: y))
                XCTAssertTrue(target === box || target === box.field,
                              "Click at y=\(y) should reach the input after \(content.count) characters")
            }
        }
    }

    func testFieldEditorCommandsKeepInteractionNavigation() {
        var actions: [String] = []
        let input = InteractionTextInput(text: .constant(""), prompt: "Answer",
                                         submit: { actions.append("submit") },
                                         previous: { actions.append("previous") },
                                         cancel: { actions.append("cancel") })
        let coordinator = input.makeCoordinator()
        let field = NSTextField()
        let editor = NSTextView()
        for selector in [#selector(NSResponder.insertNewline(_:)), #selector(NSResponder.insertTab(_:)),
                         #selector(NSResponder.insertBacktab(_:)), #selector(NSResponder.cancelOperation(_:))] {
            XCTAssertTrue(coordinator.control(field, textView: editor, doCommandBy: selector))
        }
        XCTAssertEqual(actions, ["submit", "submit", "previous", "cancel"])
    }

    private func findInputBox(in view: NSView) -> InteractionInputBox? {
        if let box = view as? InteractionInputBox { return box }
        return view.subviews.lazy.compactMap { self.findInputBox(in: $0) }.first
    }
}
