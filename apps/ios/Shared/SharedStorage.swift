import Foundation

/// Where the app keeps what its extensions read too: the saved servers,
/// drafts, and what's been seen, in the app group named by `KitAppGroup` in
/// Info.plist. Without the group, as in a build that isn't signed for it,
/// it's the app's own storage. Device tokens are in the app's Keychain
/// group, which extensions share by entitlement.
enum SharedStorage {
    /// The app group's identifier, from Info.plist, when the build is signed
    /// for it: its folder is there. Settings and files both go to the group
    /// or both stay with the app.
    static let group: String? = {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "KitAppGroup") as? String,
              !group.isEmpty, !group.contains("$("),
              FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) != nil else { return nil }
        return group
    }()

    /// The group's settings, where the app's are when there's no group.
    /// UserDefaults is safe to use from any thread.
    nonisolated(unsafe) static let defaults: UserDefaults = group.flatMap(UserDefaults.init(suiteName:)) ?? .standard

    /// The group's folder, or the app's Application Support folder.
    static let directory: URL = {
        if let group, let container = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) {
            return container.appendingPathComponent("Library/Application Support", isDirectory: true)
        }
        return FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
    }()

    /// The settings that move to the group: drafts and what's been seen.
    static let movedKeys = ["sessions.drafts", "sessions.seen", "sessions.listedServers", "sessions.working",
                            "sessions.finishedAt", "sessions.finished"]
    /// The files that move to the group: the saved servers.
    static let movedFiles = ["servers.json"]

    /// Moves what the app kept for itself into the group, once, so pairings,
    /// drafts, and what's been seen carry over. The app calls this before
    /// reading any of it.
    static func moveIntoGroup() {
        guard group != nil, defaults != .standard else { return }
        move(keys: movedKeys, from: .standard, to: defaults)
        let own = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        move(files: movedFiles, from: own, to: directory)
    }

    /// Moves each setting into the group. One the group has too, as when an
    /// extension saved there before the app moved its own, is merged: a
    /// dictionary keeps the group's entry where both have one, and a list
    /// keeps what either has.
    static func move(keys: [String], from old: UserDefaults, to new: UserDefaults) {
        for key in keys {
            guard let value = old.object(forKey: key) else { continue }
            new.set(new.object(forKey: key).map { merge(group: $0, own: value) } ?? value, forKey: key)
            old.removeObject(forKey: key)
        }
    }

    static func merge(group: Any, own: Any) -> Any {
        if let group = group as? [String: Any], let own = own as? [String: Any] {
            return own.merging(group) { _, group in group }
        }
        if let group = group as? [String], let own = own as? [String] {
            return group + own.filter { !group.contains($0) }
        }
        return group
    }

    /// Moves each file into the group unless the group has one. It's copied
    /// beside where it goes, then renamed into place, so a move cut short
    /// leaves no partial file; the original goes once the copy is in place.
    static func move(files: [String], from old: URL, to new: URL) {
        let manager = FileManager.default
        for name in files {
            let source = old.appendingPathComponent(name), destination = new.appendingPathComponent(name)
            guard manager.fileExists(atPath: source.path), !manager.fileExists(atPath: destination.path) else { continue }
            let copy = new.appendingPathComponent(".\(name).\(UUID().uuidString)")
            do {
                try manager.createDirectory(at: new, withIntermediateDirectories: true)
                try manager.copyItem(at: source, to: copy)
                try manager.moveItem(at: copy, to: destination)
                try? manager.removeItem(at: source)
            } catch {
                try? manager.removeItem(at: copy)
            }
        }
    }
}
