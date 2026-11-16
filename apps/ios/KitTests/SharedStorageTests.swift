import Foundation
import Testing
@testable import Kit

/// What the app kept for itself moves into the app group once, so pairings,
/// drafts, and what's been seen carry over to where extensions read them.
struct SharedStorageTests {
    @Test func settingsMoveIntoTheGroup() {
        let own = UserDefaults(suiteName: UUID().uuidString)!, group = UserDefaults(suiteName: UUID().uuidString)!
        own.set(["studio\u{1F}s": "Half a thought"], forKey: "sessions.drafts")
        own.set(["studio"], forKey: "sessions.listedServers")
        group.set(["vps"], forKey: "sessions.listedServers")
        SharedStorage.move(keys: ["sessions.drafts", "sessions.listedServers", "sessions.seen"], from: own, to: group)

        #expect(group.dictionary(forKey: "sessions.drafts") as? [String: String] == ["studio\u{1F}s": "Half a thought"])
        #expect(group.stringArray(forKey: "sessions.listedServers") == ["vps"], "What's in the group is kept")
        #expect(group.object(forKey: "sessions.seen") == nil)
        #expect(own.object(forKey: "sessions.drafts") == nil)
        #expect(own.object(forKey: "sessions.listedServers") == nil)
    }

    @Test func theSavedServersMoveIntoTheGroup() throws {
        let manager = FileManager.default
        let root = manager.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? manager.removeItem(at: root) }
        let own = root.appendingPathComponent("own"), group = root.appendingPathComponent("group/Library/Application Support")
        try manager.createDirectory(at: own, withIntermediateDirectories: true)
        try Data("{\"servers\":[]}".utf8).write(to: own.appendingPathComponent("servers.json"))

        SharedStorage.move(files: ["servers.json"], from: own, to: group)
        #expect(manager.contents(atPath: group.appendingPathComponent("servers.json").path) == Data("{\"servers\":[]}".utf8))
        #expect(!manager.fileExists(atPath: own.appendingPathComponent("servers.json").path))
    }

    @Test func savedServersAlreadyInTheGroupAreKept() throws {
        let manager = FileManager.default
        let root = manager.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? manager.removeItem(at: root) }
        let own = root.appendingPathComponent("own"), group = root.appendingPathComponent("group")
        try manager.createDirectory(at: own, withIntermediateDirectories: true)
        try manager.createDirectory(at: group, withIntermediateDirectories: true)
        try Data("old".utf8).write(to: own.appendingPathComponent("servers.json"))
        try Data("group".utf8).write(to: group.appendingPathComponent("servers.json"))

        SharedStorage.move(files: ["servers.json"], from: own, to: group)
        #expect(manager.contents(atPath: group.appendingPathComponent("servers.json").path) == Data("group".utf8))
    }

    /// The app runs with its group in tests, so it reads and writes there.
    @Test func theAppHasItsGroup() {
        #expect(SharedStorage.group == "group.com.akonwi.kit.dev")
        #expect(SharedStorage.defaults != .standard)
        #expect(SharedStorage.directory.path.contains("Shared/AppGroup") || SharedStorage.directory.path.contains("group.com.akonwi.kit.dev"))
    }
}
