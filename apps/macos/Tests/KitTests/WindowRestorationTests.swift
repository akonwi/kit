import AppKit
import Foundation
import Testing
@testable import Kit

@MainActor struct WindowRestorationTests {
    private func temporaryURL() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
    }
    private func id(_ session: String, server: String = "local-v2") -> SessionIdentity {
        SessionIdentity(server: server, session: session)
    }

    @Test func persistsWindowsTabsAndSelection() {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url) }
        let first = UUID(), second = UUID()
        let store = WindowRestorationStore(url: url)
        store.update(.init(id: first, sessions: [id("a"), id("a", server: "remote"), id("b")], selected: id("b")))
        store.update(.init(id: second, sessions: [id("c")], selected: nil))
        #expect(WindowRestorationStore(url: url).windows == [
            .init(id: first, sessions: [id("a"), id("a", server: "remote"), id("b")], selected: id("b")),
            .init(id: second, sessions: [id("c")], selected: id("c")),
        ])
        store.remove(first)
        #expect(WindowRestorationStore(url: url).sessions == [id("c")])
        #expect(store.error == nil)
    }

    @Test func sessionMovesToTheWindowThatLastShowedIt() {
        let store = WindowRestorationStore(url: temporaryURL())
        let first = UUID(), second = UUID()
        store.update(.init(id: first, sessions: [id("a"), id("b")], selected: id("b")))
        store.update(.init(id: second, sessions: [id("b")], selected: id("b")))
        #expect(store.windows == [
            .init(id: first, sessions: [id("a")], selected: id("a")),
            .init(id: second, sessions: [id("b")], selected: id("b")),
        ])
    }

    @Test func windowWithoutSessionsIsNotRestorationIntent() {
        let store = WindowRestorationStore(url: temporaryURL())
        let window = UUID()
        store.update(.init(id: window, sessions: [id("a")], selected: id("a")))
        store.update(.init(id: window, sessions: [], selected: nil))
        #expect(store.windows == [])
    }

    @Test func corruptManifestIsRecoverable() throws {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url) }
        try Data("invalid".utf8).write(to: url)
        let store = WindowRestorationStore(url: url)
        #expect(store.windows == [])
        #expect(store.error?.hasPrefix("Could not restore windows:") == true)
        let window = UUID()
        store.update(.init(id: window, sessions: [id("a")], selected: id("a")))
        #expect(WindowRestorationStore(url: url).windows == [.init(id: window, sessions: [id("a")], selected: id("a"))])
    }

    @Test func nativeTabGroupsMigrateToWindowsOfTabs() throws {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url) }
        let legacy = """
        {"version":1,
         "sessions":[{"server":"local-v2","session":"a"},{"server":"local-v2","session":"b"},{"server":"local-v2","session":"c"}],
         "groups":[{"sessions":[{"server":"local-v2","session":"a"},{"server":"local-v2","session":"b"}],
                    "selected":{"server":"local-v2","session":"b"}}]}
        """
        try Data(legacy.utf8).write(to: url)
        let windows = WindowRestorationStore(url: url).windows
        #expect(windows.map(\.sessions) == [[id("a"), id("b")], [id("c")]])
        #expect(windows.map(\.selected) == [id("b"), id("c")])
    }

    @Test func emptyWindowShowsTheLauncher() {
        let model = SessionWindowModel(id: UUID())
        #expect(model.tabs.count == 1)
        #expect(model.selectedTab.isLauncher)
        #expect(model.sessions == [])
        #expect(model.selectedSession == nil)
    }

    @Test func openingSessionsAddsTabsAfterSelection() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a"), id("b")], selected: id("a"))
        var changes = 0
        model.onChange = { _ in changes += 1 }
        model.open(id("c"))
        #expect(model.sessions == [id("a"), id("c"), id("b")])
        #expect(model.selectedSession == id("c"))
        model.open(id("b"))
        #expect(model.sessions == [id("a"), id("c"), id("b")])
        #expect(model.selectedSession == id("b"))
        #expect(changes == 2)
    }

    @Test func launcherTabBecomesTheChosenSession() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"))
        model.openLauncher()
        let launcher = model.selectedTab
        #expect(launcher.isLauncher)
        model.openLauncher()
        #expect(model.tabs.count == 2)
        model.show(id("b"), in: launcher)
        #expect(model.sessions == [id("a"), id("b")])
        #expect(model.selectedTab === launcher)
        #expect(model.selectedSession == id("b"))
    }

    @Test func choosingAnOpenSessionSelectsItsTab() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"))
        let existing = model.selectedTab
        model.openLauncher()
        model.show(id("a"), in: model.selectedTab)
        #expect(model.tabs.map(\.id) == [existing.id])
        #expect(model.selectedTab === existing)
    }

    @Test func closingTabsSelectsTheNeighborAndKeepsALauncher() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a"), id("b"), id("c")], selected: id("b"))
        model.close(model.selectedTab)
        #expect(model.sessions == [id("a"), id("c")])
        #expect(model.selectedSession == id("c"))
        model.close(model.selectedTab)
        #expect(model.selectedSession == id("a"))
        model.close(model.selectedTab)
        #expect(model.tabs.count == 1)
        #expect(model.selectedTab.isLauncher)
    }

    @Test func numberedAndAdjacentSelectionFollowDisplayOrder() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a"), id("b"), id("c")], selected: id("a"))
        model.select(index: 2)
        #expect(model.selectedSession == id("c"))
        model.select(index: 8)
        #expect(model.selectedSession == id("c"))
        model.selectAdjacent(1)
        #expect(model.selectedSession == id("a"))
        model.selectAdjacent(-1)
        #expect(model.selectedSession == id("c"))
    }

    @Test func unopenedTabsDescribeThemselvesFromTheCatalog() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a"), id("b")], selected: id("a"))
        let tab = model.tabs[1]
        #expect(tab.title == "b")
        #expect(tab.directory == "")
        var session = SessionExcerpt(id: "b", title: "Release notes", sourceTitle: "", model: "m", thinking: "high",
                                     workspace: "/work/kit", date: "", messages: [])
        session.cwd = "/work/kit/apps"
        model.app.recordCreated(session)
        #expect(tab.title == "Release notes")
        #expect(tab.directory == "/work/kit/apps")
    }

    @Test func tabsGroupByProjectInFirstAppearanceOrder() {
        let groups = SessionTabGroup.grouped(["kit/a", "", "ard/b", "kit/c"]) { tab in
            tab.isEmpty ? "" : "/Users/me/" + tab.split(separator: "/")[0]
        }
        #expect(groups.map(\.directory) == ["/Users/me/kit", "/Users/me/ard", ""])
        #expect(groups.map(\.tabs) == [["kit/a", "kit/c"], ["ard/b"], [""]])
        #expect(groups.map(\.name) == ["kit", "ard", nil])
    }

    @Test func registryCapturesTabsAndForgetsClosedWindows() {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url) }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let request = SessionWindowRequest(windowID: UUID())
        let model = registry.model(for: request)
        #expect(registry.model(for: request) === model)
        model.open(id("a"))
        model.open(id("b"))
        #expect(registry.restoration.windows == [.init(id: request.windowID, sessions: [id("a"), id("b")], selected: id("b"))])

        let window = NSWindow(contentRect: .zero, styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        registry.attach(model, to: window)
        window.close()
        #expect(registry.restoration.windows == [])
    }

    @Test func quittingKeepsOpenWindowsForNextLaunch() {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url) }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let request = SessionWindowRequest(windowID: UUID())
        let model = registry.model(for: request)
        model.open(id("a"))
        let window = NSWindow(contentRect: .zero, styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        registry.attach(model, to: window)
        registry.terminating = true
        window.close()
        #expect(WindowRestorationStore(url: url).sessions == [id("a")])

        let next = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        #expect(next.defaultRequest() == request)
        #expect(next.model(for: next.defaultRequest()).sessions == [id("a")])
    }

    @Test func openingASessionShownElsewhereFocusesThatWindow() {
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: temporaryURL()))
        let first = registry.model(for: SessionWindowRequest(windowID: UUID()))
        let second = registry.model(for: SessionWindowRequest(windowID: UUID()))
        first.open(id("a"))
        second.open(id("b"))
        registry.open(id("a"), from: second)
        #expect(second.sessions == [id("b")])
        #expect(first.selectedSession == id("a"))
        registry.open(id("c"), from: second)
        #expect(second.sessions == [id("b"), id("c")])
    }
}
