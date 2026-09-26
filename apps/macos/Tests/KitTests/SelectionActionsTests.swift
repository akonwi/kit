import AppKit
import Testing
@testable import Kit

@MainActor struct SelectionActionsTests {
    @Test func quoteAppearsOnlyForAChangedNonemptySelection() throws {
        _ = NSApplication.shared
        let textView = NSTextView(frame: NSRect(x: 0, y: 0, width: 300, height: 100))
        textView.string = "alpha alpha"
        let window = NSWindow(contentRect: textView.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = textView
        #expect(window.makeFirstResponder(textView))
        let coordinator = SelectionActions.Coordinator(theme: MicaTheme(dark: true), quote: { _ in })

        textView.setSelectedRange(NSRange(location: 0, length: 5))
        let first = try #require(coordinator.selection(in: window))
        #expect(first.text == "alpha")
        #expect(!first.isNew(comparedTo: first))
        #expect(first.isNew(comparedTo: nil))
        #expect(first.isCurrent(in: window))

        // Identical text in another range is still a genuinely new selection.
        textView.setSelectedRange(NSRange(location: 6, length: 5))
        let second = try #require(coordinator.selection(in: window))
        #expect(second.isNew(comparedTo: first))
        #expect(!first.isCurrent(in: window))
        textView.setSelectedRange(NSRange(location: 6, length: 0))
        #expect(coordinator.selection(in: window) == nil)
        #expect(!second.isCurrent(in: window))

        let other = NSTextView(frame: textView.bounds)
        other.string = "alpha alpha"
        textView.addSubview(other)
        #expect(window.makeFirstResponder(other))
        other.setSelectedRange(NSRange(location: 0, length: 5))
        let third = try #require(coordinator.selection(in: window))
        #expect(third.text == first.text)
        #expect(third.range == first.range)
        #expect(third.isNew(comparedTo: first))
        #expect(!first.isCurrent(in: window))
    }

    @Test func trackedSelectionCompletesWithoutAMouseUpMonitorEvent() throws {
        _ = NSApplication.shared
        let content = NSView(frame: NSRect(x: 0, y: 0, width: 300, height: 100))
        let textView = NSTextView(frame: content.bounds)
        textView.string = "alpha beta"
        content.addSubview(textView)
        let window = NSWindow(contentRect: content.bounds, styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = content
        let coordinator = SelectionActions.Coordinator(theme: MicaTheme(dark: true), quote: { _ in })

        // SwiftUI starts tracking with the window as responder, then installs
        // its text view and handles mouse-up inside its own event loop.
        let before = coordinator.selection(in: window)
        #expect(before == nil)
        #expect(window.makeFirstResponder(textView))
        textView.setSelectedRange(NSRange(location: 0, length: 5))
        #expect(coordinator.selectionAfterMouseGesture(in: window, after: before, mouseIsDown: true) == nil)
        let completed = try #require(coordinator.selectionAfterMouseGesture(in: window, after: before, mouseIsDown: false))
        #expect(completed.text == "alpha")
        #expect(completed.isCurrent(in: window))
        #expect(coordinator.selectionAfterMouseGesture(in: window, after: completed, mouseIsDown: false) == nil)
        textView.setSelectedRange(NSRange(location: 0, length: 0))
        #expect(coordinator.selectionAfterMouseGesture(in: window, after: before, mouseIsDown: false) == nil)
    }

    @Test func composerFocusStillAllowsTranscriptAccessibilitySelection() throws {
        _ = NSApplication.shared
        let content = NSView(frame: NSRect(x: 0, y: 0, width: 300, height: 150))
        let transcript = NSTextView(frame: NSRect(x: 0, y: 70, width: 300, height: 80))
        transcript.string = "alpha beta"
        transcript.setSelectedRange(NSRange(location: 0, length: 5))
        let composer = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 300, height: 50))
        content.addSubview(transcript)
        content.addSubview(composer)
        let window = NSWindow(contentRect: NSRect(x: -10000, y: -10000, width: 300, height: 150),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = content
        #expect(window.makeFirstResponder(composer))
        let coordinator = SelectionActions.Coordinator(theme: MicaTheme(dark: true), quote: { _ in })
        let selection = try #require(coordinator.selection(in: window, accessibilityElement: transcript))
        #expect(selection.text == "alpha")
        #expect(selection.viaAccessibility)
        #expect(selection.responder === composer)
        #expect(selection.isCurrent(in: window))
        transcript.setSelectedRange(NSRange(location: 0, length: 0))
        #expect(!selection.isCurrent(in: window))
    }

    @Test func accessibilitySelectionsDistinguishSourcesWithIdenticalText() {
        let firstSource = NSObject(), secondSource = NSObject()
        let first = SelectionActions.Coordinator.Selection(text: "alpha", rect: .zero,
            source: firstSource, range: nil, responder: nil, viaAccessibility: true)
        let second = SelectionActions.Coordinator.Selection(text: "alpha", rect: .zero,
            source: secondSource, range: nil, responder: nil, viaAccessibility: true)
        #expect(!first.isNew(comparedTo: first))
        #expect(second.isNew(comparedTo: first))
    }
}
