import Foundation

/// Something shared to a session from another app: words and attachments
/// waiting for the app to add them to the session's draft.
struct SharedItem: Codable, Equatable, Sendable {
    var id = UUID()
    var server: String
    var session: String
    var text: String
    /// Attachments already uploaded to the session, by their IDs on the server.
    var attachmentIDs: [String] = []
    var date = Date()

    /// The words a share adds: the note, then what was shared, each in its
    /// own paragraph.
    static func text(note: String, shared: [String]) -> String {
        ([note] + shared).map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .joined(separator: "\n\n")
    }

    /// A draft with shared words after its own, in a new paragraph.
    static func join(_ draft: String, _ shared: String) -> String {
        let draft = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        if draft.isEmpty { return shared }
        if shared.isEmpty { return draft }
        return draft + "\n\n" + shared
    }
}

/// Shares waiting for the app, one file each in the app group. The share
/// extension only adds files and the app only removes them, so neither
/// writes over what the other kept, and a share that arrives while the app
/// takes the others waits for the next time.
struct ShareInbox: Sendable {
    let folder: URL

    static var live: ShareInbox {
        ShareInbox(folder: SharedStorage.directory.appendingPathComponent("Shares", isDirectory: true))
    }

    /// Leaves a share for the app. It's written whole or not at all.
    func put(_ item: SharedItem) throws {
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        try encoder.encode(item).write(to: file(item.id), options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }

    /// The shares waiting, oldest first. A file that can't be read is
    /// skipped and left in place.
    func waiting() -> [SharedItem] {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let files = (try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil)) ?? []
        return files.filter { $0.pathExtension == "json" && !$0.lastPathComponent.hasPrefix(".") }
            .compactMap { (try? Data(contentsOf: $0)).flatMap { try? decoder.decode(SharedItem.self, from: $0) } }
            .sorted { $0.date < $1.date }
    }

    /// Lets a share go once it's in its draft.
    func remove(_ id: UUID) {
        try? FileManager.default.removeItem(at: file(id))
    }

    private func file(_ id: UUID) -> URL {
        folder.appendingPathComponent(id.uuidString + ".json")
    }
}
