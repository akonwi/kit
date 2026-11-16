import Foundation

/// Where the app keeps what its extensions read too: the saved servers,
/// drafts, and what's been seen, in the app group named by `KitAppGroup` in
/// Info.plist. Without the group, as in a build that isn't signed for it,
/// it's the app's own storage. Device tokens are in the app's Keychain
/// group, which extensions share by entitlement.
enum SharedStorage {
    /// The app group's identifier, from Info.plist.
    static let group: String? = {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "KitAppGroup") as? String,
              !group.isEmpty, !group.contains("$(") else { return nil }
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
    /// drafts, and what's been seen carry over. Something already in the
    /// group is kept. The app calls this before reading any of it.
    static func moveIntoGroup() {
        guard group != nil, defaults != .standard else { return }
        move(keys: movedKeys, from: .standard, to: defaults)
        let own = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        move(files: movedFiles, from: own, to: directory)
    }

    static func move(keys: [String], from old: UserDefaults, to new: UserDefaults) {
        for key in keys {
            guard let value = old.object(forKey: key) else { continue }
            if new.object(forKey: key) == nil { new.set(value, forKey: key) }
            old.removeObject(forKey: key)
        }
    }

    static func move(files: [String], from old: URL, to new: URL) {
        let manager = FileManager.default
        for name in files {
            let source = old.appendingPathComponent(name), destination = new.appendingPathComponent(name)
            guard manager.fileExists(atPath: source.path) else { continue }
            guard !manager.fileExists(atPath: destination.path) else { continue }
            do {
                try manager.createDirectory(at: new, withIntermediateDirectories: true)
                // Copied, then removed, so a failure leaves the original in place.
                try manager.copyItem(at: source, to: destination)
                try? manager.removeItem(at: source)
            } catch {
                continue
            }
        }
    }
}
