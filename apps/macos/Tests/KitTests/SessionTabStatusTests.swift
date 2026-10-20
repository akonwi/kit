import Foundation
import Testing
@testable import Kit

@MainActor struct SessionTabStatusTests {
    private func model() -> SessionWindowModel {
        let ids = ["a", "b"].map { SessionIdentity(server: "local", session: $0) }
        return SessionWindowModel(id: UUID(), sessions: ids, selected: ids[0])
    }

    @Test func turnEndingInABackgroundTabMarksItFinishedUntilShown() {
        let model = model()
        let background = model.tabs[1]
        model.record(.running, for: background)
        #expect(background.statusLabel == nil)
        model.record(.idle, for: background)
        #expect(background.finished)
        #expect(background.needsAttention)
        #expect(background.statusLabel == "Finished")
        model.select(background)
        #expect(!background.finished)
        #expect(!background.needsAttention)
    }

    @Test func turnEndingInFrontOfTheUserIsNotMarked() {
        let model = model()
        let selected = model.selectedTab
        model.record(.awaitingResponse, for: selected)
        model.record(.idle, for: selected)
        #expect(!selected.finished)
    }

    @Test func turnEndingWhileTheWindowIsInactiveIsMarkedUntilFocused() {
        let model = model()
        let selected = model.selectedTab
        model.isFocused = false
        model.record(.running, for: selected)
        model.record(.idle, for: selected)
        #expect(selected.finished)
        model.isFocused = true
        #expect(!selected.finished)
    }

    @Test func newActivityReplacesTheFinishedMarker() {
        let model = model()
        let background = model.tabs[1]
        model.record(.running, for: background)
        model.record(.idle, for: background)
        model.record(.running, for: background)
        #expect(!background.finished)
        model.record(.idle, for: background)
        model.record(.idle, for: background)
        #expect(background.finished)
    }

    @Test func idleTabsRecedeUnlessSelected() {
        let model = model()
        #expect(SessionTabStatusMarker.titleProminent(model.tabs[0], selected: true))
        #expect(!SessionTabStatusMarker.titleProminent(model.tabs[1], selected: false))
        model.openLauncher()
        #expect(!SessionTabStatusMarker.titleProminent(model.selectedTab, selected: true))
    }
}
