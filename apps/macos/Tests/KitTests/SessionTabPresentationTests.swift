import AppKit
import Testing
@testable import Kit

@MainActor struct SessionTabPresentationTests {
    @Test func nativeTabShowsActivityAndRetainsItsName() throws {
        let window = NSWindow(contentRect: .zero, styleMask: [.titled], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        defer { window.close() }
        SessionTabPresentation.update(window, title: "Review", status: .running)
        #expect(window.tab.title == "Review")
        #expect(window.tab.toolTip == "Review — Working")
        let spinner = try #require(window.tab.accessoryView as? TabActivitySpinner)
        #expect(spinner.constraints.filter { $0.constant == 14 }.count == 2)
        #expect(spinner.accessibilityLabel() == "Working")
        SessionTabPresentation.update(window, title: "Renamed", status: .running)
        #expect(window.tab.accessoryView === spinner)
        #expect(window.tab.title == "Renamed")
        SessionTabPresentation.update(window, title: "Renamed", status: .awaitingResponse)
        let indicator = try #require(window.tab.accessoryView as? NSImageView)
        #expect(indicator.image != nil)
        #expect(indicator.accessibilityLabel() == "Awaiting response")
        #expect(window.tab.toolTip == "Renamed — Awaiting response")
        SessionTabPresentation.update(window, title: "Renamed", status: .idle)
        #expect(window.tab.accessoryView == nil)
        #expect(window.tab.toolTip == "Renamed")
        #expect(window.tab.title == "Renamed")
    }
}
