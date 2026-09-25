import AppKit
import Foundation
import Testing
@testable import Kit

@MainActor struct WindowGroupingTests {
    private func request(_ id: String) -> SessionWindowRequest {
        SessionWindowRequest(serverID: "local-v2", sessionID: id)
    }

    private func window() -> NSWindow {
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 500, height: 400),
                              styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        return window
    }

    @Test func launcherJoinsOriginatingGroupAndReusesItsTab() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let source = window(), launcher = window(), other = window()
        defer { source.close(); launcher.close(); other.close() }
        registry.prepareTabbing(source, request: request("a"))
        registry.register(source, request: request("a"))
        var opens = 0
        registry.showLauncher(from: source) { request in
            opens += 1
            registry.prepareTabbing(launcher, request: request)
            registry.register(launcher, request: request)
        }
        #expect(source.tabGroup?.windows == [source, launcher])
        // An already-open launcher follows a new tab request from another group.
        registry.register(other, request: request("b"))
        registry.showLauncher(from: other) { _ in opens += 1 }
        #expect(other.tabGroup?.windows == [other, launcher])
        #expect(other.tabGroup?.selectedWindow === launcher)
        #expect(opens == 1)
    }

    @Test func layoutSurvivesPartialRestorationAndIdentityChanges() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let store = WindowRestorationStore(url: url)
        let a = request("a").identity, b = request("b").identity, c = request("c").identity
        for member in [a, b, c] { store.remember(member) }
        store.rememberGroups([.init(sessions: [b, a], selected: a), .init(sessions: [c], selected: c)])
        store.rememberGroups([.init(sessions: [b], selected: b)])
        #expect(store.groups == [.init(sessions: [b, a], selected: a), .init(sessions: [c], selected: c)])
        let replacement = request("renamed-selection").identity
        store.remember(replacement, replacing: a)
        #expect(store.groups[0] == .init(sessions: [b, replacement], selected: replacement))
        store.forget(b)
        #expect(WindowRestorationStore(url: url).groups == [
            .init(sessions: [replacement], selected: replacement), .init(sessions: [c], selected: c)
        ])
    }

    @Test func restoresSeparateGroupsInSavedOrderAcrossRepeatedLaunches() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let store = WindowRestorationStore(url: url)
        let ids = ["a", "b", "c", "d", "e"]
        for id in ids { store.remember(request(id).identity) }
        let expected: [WindowRestorationStore.Group] = [
            .init(sessions: [request("b").identity, request("a").identity], selected: request("a").identity),
            .init(sessions: [request("c").identity], selected: request("c").identity),
            .init(sessions: [request("d").identity, request("e").identity], selected: request("d").identity)
        ]
        store.rememberGroups(expected)
        for order in [["e", "c", "a", "d", "b"], ["b", "d", "a", "e", "c"]] {
            let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
            let windows = Dictionary(uniqueKeysWithValues: ids.map { ($0, window()) })
            // Native restoration may provisionally combine unrelated windows.
            windows["a"]!.addTabbedWindow(windows["c"]!, ordered: .above)
            for id in order {
                let window = windows[id]!
                registry.prepareTabbing(window, request: request(id))
                registry.register(window, request: request(id))
            }
            #expect(windows["a"]!.tabGroup?.windows == [windows["b"]!, windows["a"]!])
            #expect(windows["a"]!.tabGroup?.selectedWindow === windows["a"]!)
            #expect(windows["c"]!.tabGroup?.windows == [windows["c"]!])
            #expect(windows["d"]!.tabGroup?.windows == [windows["d"]!, windows["e"]!])
            #expect(windows["d"]!.tabGroup?.selectedWindow === windows["d"]!)
            registry.terminating = true
            for window in windows.values { window.close() }
            #expect(WindowRestorationStore(url: url).groups == expected)
        }
    }

    @Test func tabsRetainWindowContentsAndClosedSessionsStayClosed() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let a = window(), b = window(), detached = window()
        defer { a.close(); b.close(); detached.close() }
        let content = NSView()
        a.contentView = content
        for (window, id) in [(a, "a"), (b, "b"), (detached, "c")] {
            registry.prepareTabbing(window, request: request(id))
            registry.register(window, request: request(id))
        }
        detached.moveTabToNewWindow(nil)
        a.tabGroup?.selectedWindow = a
        registry.captureLayout()
        b.close()
        registry.prepareTabbing(a, request: request("a"))
        #expect((a.tabGroup?.windows ?? [a]) == [a])
        #expect((detached.tabGroup?.windows ?? [detached]) == [detached])
        #expect(a.contentView === content)
        #expect(Set(registry.registeredIdentities) == Set([request("a").identity, request("c").identity]))
        // A deliberate reopen uses its source group, rather than resurrecting an old group.
        a.makeKeyAndOrderFront(nil)
        var opened: SessionWindowRequest?
        registry.openSession("b", serverID: "local-v2") { opened = $0 }
        #expect(opened == request("b"))
        let reopened = window()
        defer { reopened.close() }
        registry.prepareTabbing(reopened, request: request("b"))
        registry.register(reopened, request: request("b"))
        #expect(a.tabGroup?.windows == [a, reopened])
        #expect((detached.tabGroup?.windows ?? [detached]) == [detached])
    }

    @Test func nativeReorderAndDetachPersistWithoutViewUpdates() async throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let a = window(), b = window(), c = window()
        defer { a.close(); b.close(); c.close() }
        for (window, id) in [(a, "a"), (b, "b"), (c, "c")] {
            registry.prepareTabbing(window, request: request(id))
            registry.register(window, request: request(id))
        }
        a.tabGroup?.insertWindow(b, at: 0)
        c.moveTabToNewWindow(nil)
        a.tabGroup?.selectedWindow = b
        let expected: [WindowRestorationStore.Group] = [
            .init(sessions: [request("b").identity, request("a").identity], selected: request("b").identity),
            .init(sessions: [request("c").identity], selected: request("c").identity)
        ]
        for _ in 0..<60 {
            if registry.restoration.groups == expected { break }
            try await Task.sleep(for: .milliseconds(50))
        }
        #expect(WindowRestorationStore(url: url).groups == expected)
    }
    @Test func appDelegateQuitPreservesEveryWindow() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let delegate = AppDelegate(sessionWindows: registry)
        let a = window(), b = window(), c = window()
        for (window, id) in [(a, "a"), (b, "b"), (c, "c")] {
            registry.prepareTabbing(window, request: request(id))
            registry.register(window, request: request(id))
        }
        c.moveTabToNewWindow(nil)
        a.tabGroup?.selectedWindow = a
        #expect(delegate.applicationShouldTerminate(NSApplication.shared) == .terminateNow)
        #expect(registry.terminating)
        for window in [a, b, c] { window.close() }
        let restored = WindowRestorationStore(url: url)
        #expect(restored.sessions == [request("a").identity, request("b").identity, request("c").identity])
        #expect(restored.groups == [
            .init(sessions: [request("a").identity, request("b").identity], selected: request("a").identity),
            .init(sessions: [request("c").identity], selected: request("c").identity)
        ])
    }

}
