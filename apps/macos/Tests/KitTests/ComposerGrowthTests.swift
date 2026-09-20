import AppKit
import Testing
import SwiftUI
@testable import Kit

@MainActor struct ComposerGrowthTests {
    @Test func swiftUIEditorCapsLargePastes() async throws {
        _ = NSApplication.shared
        let draft = String(repeating: "at application.render(file.swift:123)\n", count: 2000)
        let host = NSHostingView(rootView: VStack {
            Spacer()
            GrowingComposerEditor(text: .constant(draft), focused: .constant(false), focusRequest: 0,
                foreground: .primary, placeholderColor: .secondary, attachmentDrop: { _ in false },
                attachmentDropTargeted: { _ in }, submit: {})
            Text("Composer controls remain visible")
        }.padding(20))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 600, height: 500),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        func editor(in view: NSView) -> ComposerScrollView? {
            if let scroll = view as? ComposerScrollView { return scroll }
            return view.subviews.lazy.compactMap { editor(in: $0) }.first
        }
        for _ in 0..<10 {
            host.layoutSubtreeIfNeeded()
            if let scroll = editor(in: host), scroll.editor.string == draft, scroll.editor.frame.height > 240 { break }
            try await Task.sleep(for: .milliseconds(20))
        }
        let scroll = try #require(editor(in: host))
        #expect(scroll.frame.height == 240)
        #expect(scroll.editor.string == draft)
        #expect(scroll.editor.frame.height > scroll.contentSize.height)
    }

    @Test func largeDraftScrollsAndClearingRestoresSingleLine() throws {
        _ = NSApplication.shared
        let scroll = ComposerScrollView()
        let editor = scroll.editor
        editor.font = .systemFont(ofSize: 14)
        editor.textContainerInset = .zero
        editor.textContainer?.lineFragmentPadding = 0
        editor.textContainer?.widthTracksTextView = false
        editor.isVerticallyResizable = false
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 600, height: 240),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = scroll
        defer { window.close() }
        window.orderFront(nil)
        editor.string = String(repeating: "at application.render(file.swift:123)\n", count: 2000)
        scroll.layoutSubtreeIfNeeded()
        #expect(scroll.intrinsicContentSize.height == 240)
        #expect(editor.frame.height > 240)
        let end = NSRange(location: editor.string.utf16.count, length: 0)
        editor.setSelectedRange(end)
        editor.scrollRangeToVisible(end)
        #expect(scroll.contentView.bounds.minY > 0)
        #expect(editor.selectedRange() == end)
        editor.string = ""
        scroll.needsLayout = true
        scroll.layoutSubtreeIfNeeded()
        let layout = try #require(editor.layoutManager)
        let font = try #require(editor.font)
        #expect(scroll.intrinsicContentSize.height == ceil(layout.defaultLineHeight(for: font)))
    }
}
