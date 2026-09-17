import AppKit
import Foundation
import Testing
@testable import Kit

@MainActor struct WindowRestorationTests {
    @Test func persistsIntentAndSelectionChangesWithoutCatalogExpansion() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("windows.json")
        let a = SessionIdentity(server: "one", session: "a")
        let b = SessionIdentity(server: "one", session: "b")
        let remote = SessionIdentity(server: "two", session: "a")
        let store = WindowRestorationStore(url: url)
        store.remember(a)
        store.remember(remote)
        store.remember(a)
        #expect(WindowRestorationStore(url: url).sessions == [a, remote])
        store.remember(b, replacing: a)
        #expect(WindowRestorationStore(url: url).sessions == [remote, b])
        store.forget(remote)
        #expect(WindowRestorationStore(url: url).sessions == [b])
        #expect(store.error == nil)
    }

    @Test func emptyWindowIsNotRestorationIntent() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        let store = WindowRestorationStore(url: url)
        store.remember(SessionIdentity(server: "local-v2", session: ""))
        #expect(store.sessions == [])
    }

    @Test func corruptManifestIsRecoverable() throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        try Data("invalid".utf8).write(to: url)
        let store = WindowRestorationStore(url: url)
        #expect(store.sessions == [])
        #expect(store.error?.hasPrefix("Could not restore windows:") == true)
        store.remember(SessionIdentity(server: "local-v2", session: "a"))
        #expect(WindowRestorationStore(url: url).sessions == [SessionIdentity(server: "local-v2", session: "a")])
    }

    @Test func windowRequestsIncludeServerIdentity() throws {
        let a = SessionWindowRequest(serverID: "one", sessionID: "same")
        let b = SessionWindowRequest(serverID: "two", sessionID: "same")
        #expect(Set([a, b]).count == 2)
        #expect(try JSONDecoder().decode(SessionWindowRequest.self, from: JSONEncoder().encode(a)) == a)
    }
    @Test func registryDeduplicatesAndTracksCloseVersusQuit() throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let store = WindowRestorationStore(url: url)
        let registry = SessionWindowRegistry(restoration: store)
        let first = NSWindow(contentRect: .zero, styleMask: [], backing: .buffered, defer: false)
        let second = NSWindow(contentRect: .zero, styleMask: [], backing: .buffered, defer: false)
        first.isReleasedWhenClosed = false
        second.isReleasedWhenClosed = false
        defer { first.close(); second.close() }
        let a = SessionWindowRequest(serverID: "local-v2", sessionID: "a")
        let b = SessionWindowRequest(serverID: "local-v2", sessionID: "b")
        #expect(registry.register(first, request: a))
        var opens: [SessionWindowRequest] = []
        registry.openSession("a", serverID: "local-v2") { opens.append($0) }
        #expect(opens == [])
        registry.register(first, request: b)
        #expect(store.sessions == [b.identity])
        registry.register(second, request: a)
        #expect(store.sessions == [b.identity, a.identity])
        second.close()
        #expect(store.sessions == [b.identity])
        registry.terminating = true
        first.close()
        #expect(WindowRestorationStore(url: url).sessions == [b.identity])
    }

    @Test func tabJoinsDuringRootAttachmentBeforeSessionRegistration() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let style: NSWindow.StyleMask = [.titled, .closable, .resizable]
        let first = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 500, height: 400), styleMask: style, backing: .buffered, defer: false)
        let second = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 500, height: 400), styleMask: style, backing: .buffered, defer: false)
        first.isReleasedWhenClosed = false
        second.isReleasedWhenClosed = false
        defer { first.close(); second.close() }
        registry.prepareTabbing(first, request: SessionWindowRequest(serverID: "local-v2", sessionID: "a"))
        let probe = SessionWindowBridge.WindowProbe()
        probe.prepare = { window in
            registry.prepareTabbing(window, request: SessionWindowRequest(serverID: "local-v2", sessionID: "b"))
        }
        second.contentView = probe
        #expect(first.tabbedWindows?.contains(second) == true)
        #expect(second.tabbingIdentifier == AppIdentity.tabbingIdentifier)
        #expect(registry.restoration.sessions == [])
        // Later configuration must not move the already attached tab again.
        registry.prepareTabbing(second, request: SessionWindowRequest(serverID: "local-v2", sessionID: "b"))
        #expect(first.tabbedWindows?.count == 2)
    }

    private func window() -> NSWindow {
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 500, height: 400),
                              styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        return window
    }

    @Test
    func repeatedRestorationConvergesOnIntent() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let a = SessionWindowRequest(serverID: "local-v2", sessionID: "a")
        let b = SessionWindowRequest(serverID: "remote", sessionID: "a")
        WindowRestorationStore(url: url).remember(a.identity)
        WindowRestorationStore(url: url).remember(b.identity)
        for _ in 0..<3 {
            let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
            #expect(registry.defaultRequest() == a)
            #expect(registry.defaultRequest() == a)
            let windows = (0..<5).map { _ in window() }
            let requests = [SessionWindowRequest(sessionID: ""), a, b, a, SessionWindowRequest(sessionID: "")]
            for (window, request) in zip(windows, requests) {
                registry.prepareTabbing(window, request: request)
                registry.register(window, request: request)
            }
            #expect(Set(registry.registeredIdentities) == Set([a.identity, b.identity]))
            #expect(registry.registeredIdentities.count == 2)
            #expect(registry.restoration.sessions == [a.identity, b.identity])
            var requestsToOpen: [SessionWindowRequest] = []
            registry.openSession(a.sessionID, serverID: a.serverID) { requestsToOpen.append($0) }
            #expect(requestsToOpen == [])
            registry.terminating = true
            for window in windows { window.close() }
            #expect(WindowRestorationStore(url: url).sessions == [a.identity, b.identity])
        }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let first = window()
        registry.register(first, request: a)
        first.close()
        // A deferred SwiftUI callback after close must not restore the closed session.
        #expect(registry.register(first, request: a) == false)
        #expect(WindowRestorationStore(url: url).sessions == [b.identity])
        #expect(registry.defaultRequest() == b)
    }

    @Test
    func emptyCatalogRetainsOneChooser() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let blank = SessionWindowRequest(sessionID: "")
        let connected = SessionWindowRequest(serverID: "local-v2", sessionID: "")
        let windows = (0..<4).map { _ in window() }
        defer { for window in windows { window.close() } }
        for (window, request) in zip(windows, [blank, connected, connected, blank]) {
            registry.prepareTabbing(window, request: request)
            registry.configure(window, serverID: request.serverID, sessionID: request.sessionID,
                               sessions: [], open: { _ in })
        }
        #expect(registry.registeredIdentities == [connected.identity])
        #expect(registry.restoration.sessions == [])
        #expect(registry.defaultRequest() == blank)
        // Rejected duplicate windows cannot reappear through late configuration.
        #expect(registry.register(windows[2], request: connected) == false)
        #expect(registry.registeredIdentities == [connected.identity])
    }

    @Test func closedUnregisteredRootCannotReturnToTabGroup() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let a = SessionWindowRequest(serverID: "local-v2", sessionID: "a")
        let b = SessionWindowRequest(serverID: "local-v2", sessionID: "b")
        let closed = window(), first = window(), second = window()
        defer { closed.close(); first.close(); second.close() }
        registry.prepareTabbing(closed, request: a)
        closed.close()
        registry.prepareTabbing(closed, request: a)
        #expect(registry.register(closed, request: a) == false)
        registry.prepareTabbing(first, request: a)
        registry.register(first, request: a)
        registry.prepareTabbing(second, request: b)
        registry.register(second, request: b)
        #expect(first.tabbedWindows?.count == 2)
        #expect(first.tabbedWindows?.contains(second) == true)
        #expect(Set(registry.registeredIdentities) == Set([a.identity, b.identity]))
    }

    @Test
    func nativeRestorationAndManifestReplayShareOneIdentity() async throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let a = SessionWindowRequest(serverID: "local-v2", sessionID: "a")
        let b = SessionWindowRequest(serverID: "local-v2", sessionID: "b")
        let store = WindowRestorationStore(url: url)
        store.remember(a.identity)
        store.remember(b.identity)
        let registry = SessionWindowRegistry(restoration: store)
        let first = window(), second = window(), duplicate = window()
        defer { first.close(); second.close(); duplicate.close() }
        var opens: [SessionWindowRequest] = []
        registry.restoreRemaining { opens.append($0) }
        registry.restoreRemaining { opens.append($0) }
        // The native scene arrives before the deferred manifest replay.
        registry.prepareTabbing(first, request: a)
        registry.register(first, request: a)
        // AppKit tests share the main actor; wait for replay rather than assuming
        // its task began when it was scheduled.
        for _ in 0..<60 {
            if !opens.isEmpty { break }
            try await Task.sleep(for: .milliseconds(50))
        }
        #expect(opens == [b])
        registry.openSession(b.sessionID, serverID: b.serverID) { opens.append($0) }
        #expect(opens == [b])
        // A second native scene races the requested scene for the same identity.
        for window in [second, duplicate] {
            registry.prepareTabbing(window, request: b)
            registry.register(window, request: b)
        }
        #expect(registry.registeredIdentities.count == 2)
        #expect(Set(registry.registeredIdentities) == Set([a.identity, b.identity]))
        #expect(store.sessions == [a.identity, b.identity])
    }

    @Test func nativeNewTabOpensLauncherBeforeSessionConnection() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let delegate = AppDelegate(sessionWindows: registry)
        var opened: [SessionWindowRequest] = []
        registry.restoreRemaining { opened.append($0) }
        delegate.newWindowForTab(nil)
        #expect(opened == [SessionWindowRequest(serverID: "local-v2", sessionID: "")])
        #expect(registry.restoration.sessions == [])
    }

}
