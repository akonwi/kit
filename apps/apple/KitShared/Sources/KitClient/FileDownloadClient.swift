import Foundation

/// A file to download from a session's server (ADR 0074): one in the
/// session's workspace, at its canonical path, or one anywhere on the server's
/// machine, at an absolute, `~`, or `~/…` path.
public enum FileDownloadTarget: Hashable, Sendable {
    case workspace(id: String, path: String)
    case host(path: String)

    /// The request's query. Naming a workspace is what makes it a workspace
    /// download.
    var query: [URLQueryItem] {
        switch self {
        case .workspace(let id, let path): [URLQueryItem(name: "workspaceId", value: id), URLQueryItem(name: "path", value: path)]
        case .host(let path): [URLQueryItem(name: "path", value: path)]
        }
    }

    /// The same file with its path normalized (`FilePaths.normalizedHost`),
    /// as requests and the download store's keys use it.
    public var normalized: FileDownloadTarget {
        guard case .host(let path) = self, let normal = FilePaths.normalizedHost(path) else { return self }
        return .host(path: normal)
    }

    var isValid: Bool {
        switch self {
        case .workspace(let id, let path):
            FileDownloadInfo.validToken(id, prefix: "workspace_") && !path.isEmpty && !path.hasPrefix("/") && !path.contains("\0") && path.utf8.count <= 4096
        case .host(let path):
            (path.hasPrefix("/") || path == "~" || path.hasPrefix("~/")) && !path.contains("\0") && path.utf8.count <= 4096
        }
    }
}

/// What a download's headers say about its file.
public struct FileDownloadInfo: Equatable, Sendable {
    public let session: String
    /// The workspace a workspace file was resolved in; nil for a host file.
    public let workspace: String?
    /// The canonical workspace path, or the absolute host path.
    public let path: String
    /// The file's revision, or nil for a file that reports a size of zero,
    /// which is served to its end without a validator.
    public let revision: String?
    /// The file's size when the server opened it, or nil when unknown.
    public let size: Int64?
    public let modifiedAt: String

    public init(session: String, workspace: String?, path: String, revision: String?, size: Int64?, modifiedAt: String) {
        self.session = session; self.workspace = workspace; self.path = path
        self.revision = revision; self.size = size; self.modifiedAt = modifiedAt
    }

    /// An opaque token: a prefix and 43 base64url characters, as SHA-256 is.
    static func validToken(_ value: String, prefix: String) -> Bool {
        guard value.hasPrefix(prefix) else { return false }
        let rest = value.dropFirst(prefix.count)
        return rest.count == 43 && rest.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") }
    }
}

/// The answer to one download request.
public struct FileDownloadResult: Equatable, Sendable {
    public let info: FileDownloadInfo
    /// The copy named by the request's cached revision is current; nothing
    /// was written.
    public let notModified: Bool
    /// The bytes written, from the start of the file.
    public let bytes: Int64
    /// The download stopped at the request's byte limit before the file's end.
    public let truncated: Bool

    public init(info: FileDownloadInfo, notModified: Bool, bytes: Int64, truncated: Bool) {
        self.info = info; self.notModified = notModified; self.bytes = bytes; self.truncated = truncated
    }
}

/// Why a download failed.
public enum FileDownloadError: LocalizedError, Equatable {
    /// The server predates downloads; read a bounded preview instead.
    case serverNeedsUpdate
    case invalidTarget
    /// The server answered with a declared failure.
    case response(Int)

    public var errorDescription: String? {
        switch self {
        case .serverNeedsUpdate: "This server’s Kit is too old to download whole files."
        case .invalidTarget: "This isn’t a file Kit can download."
        case .response(404): "This file no longer exists."
        case .response(409): "The workspace changed. Refresh to read the latest version."
        case .response(429): "The server is busy. Try again shortly."
        case .response: "Couldn’t download this file."
        }
    }
}

/// Downloads whole files from a session's server (ADR 0074).
public protocol FileDownloadClient: SessionClient {
    /// Downloads `target` into `destination`, replacing it, unless the file
    /// still has `cachedRevision`. At most `maxBytes` are written from the
    /// file's start; `progress` reports the bytes written so far and the
    /// file's size when known.
    func downloadFile(_ session: String, target: FileDownloadTarget, cachedRevision: String?, maxBytes: Int64?,
                      to destination: URL, progress: (@Sendable (Int64, Int64?) -> Void)?) async throws -> FileDownloadResult
}

extension ServerClient: FileDownloadClient {
    public func downloadFile(_ session: String, target: FileDownloadTarget, cachedRevision: String?, maxBytes: Int64?,
                             to destination: URL, progress: (@Sendable (Int64, Int64?) -> Void)?) async throws -> FileDownloadResult {
        try await resolve().downloadFile(session, target: target, cachedRevision: cachedRevision, maxBytes: maxBytes,
                                         to: destination, progress: progress)
    }
}

/// Downloaded files kept on disk for one run of the app, keyed by server,
/// session, file, and byte limit. A file is downloaded again only when the
/// server says its revision changed. The directory is private to the user,
/// emptied when the store is created and when `removeAll` runs, and bounded:
/// the least recently used files go first, and a file larger than the bound
/// isn't kept.
///
/// Callers get a file's bytes mapped from disk rather than its location, so
/// replacing or evicting it doesn't disturb a caller still reading them.
/// Concurrent requests for one key each download it.
public actor FileDownloadStore {
    public struct Key: Hashable, Sendable {
        public let server: String
        public let session: String
        public let target: FileDownloadTarget
        public let maxBytes: Int64?
        public init(server: String, session: String, target: FileDownloadTarget, maxBytes: Int64?) {
            self.server = server; self.session = session; self.target = target.normalized; self.maxBytes = maxBytes
        }
    }

    /// One downloaded file.
    public struct File: Sendable {
        public let info: FileDownloadInfo
        /// The bytes from the file's start, mapped from the store's copy.
        public let data: Data
        /// The file continues past these bytes.
        public let truncated: Bool
    }

    private struct Entry {
        let info: FileDownloadInfo
        let url: URL
        let bytes: Int64
        let truncated: Bool
        var used: UInt64
    }

    /// The shared store, under the app's caches directory.
    public static let shared = FileDownloadStore(directory: defaultDirectory(), maxTotalBytes: 1 << 30)

    /// Where the shared store keeps its files.
    public static func defaultDirectory() -> URL {
        let caches = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first ?? FileManager.default.temporaryDirectory
        return caches.appendingPathComponent(Bundle.main.bundleIdentifier ?? "Kit", isDirectory: true)
            .appendingPathComponent("FileDownloads", isDirectory: true)
    }

    private let directory: URL
    private let maxTotalBytes: Int64
    private var entries: [Key: Entry] = [:]
    private var clock: UInt64 = 0
    /// The latest request under way for each key.
    private var latest: [Key: UInt64] = [:]
    private var requests: UInt64 = 0

    public init(directory: URL, maxTotalBytes: Int64) {
        self.directory = directory
        self.maxTotalBytes = maxTotalBytes
        Self.reset(directory)
    }

    /// The current copy of a file, downloading it when the store has none or
    /// the server has a newer revision.
    public func file(_ key: Key, client: any FileDownloadClient, progress: (@Sendable (Int64, Int64?) -> Void)? = nil) async throws -> File {
        guard key.target.isValid else { throw FileDownloadError.invalidTarget }
        // A copy that changed hands meanwhile is fetched again, a few times.
        for _ in 0..<3 {
            if let file = try await attempt(key, client: client, progress: progress) { return file }
        }
        throw FileDownloadError.response(409)
    }

    /// One request for `key`, or nil when it was answered with a copy the
    /// store no longer has.
    private func attempt(_ key: Key, client: any FileDownloadClient, progress: (@Sendable (Int64, Int64?) -> Void)?) async throws -> File? {
        let cached = entries[key]
        if !FileManager.default.fileExists(atPath: directory.path) { Self.reset(directory) }
        // Only the latest request for a key changes its entry, so a slower,
        // older answer can't replace a newer one.
        requests += 1
        let request = requests
        latest[key] = request
        let destination = directory.appendingPathComponent(UUID().uuidString)
        let result: FileDownloadResult
        do {
            result = try await client.downloadFile(key.session, target: key.target, cachedRevision: cached?.info.revision,
                                                   maxBytes: key.maxBytes, to: destination, progress: progress)
        } catch {
            try? FileManager.default.removeItem(at: destination)
            if latest[key] == request { latest[key] = nil }
            throw error
        }
        clock += 1
        let newest = latest[key] == request
        if newest { latest[key] = nil }
        if result.notModified {
            guard let current = entries[key], current.info.revision == result.info.revision else { return nil }
            guard let data = try? Data(contentsOf: current.url, options: .alwaysMapped) else {
                remove(key)
                return nil
            }
            entries[key]?.used = clock
            return File(info: current.info, data: data, truncated: current.truncated)
        }
        let data: Data
        do { data = try Data(contentsOf: destination, options: .alwaysMapped) } catch {
            try? FileManager.default.removeItem(at: destination)
            throw error
        }
        let file = File(info: result.info, data: data, truncated: result.truncated)
        guard newest, result.bytes <= maxTotalBytes else {
            // Not kept: superseded, or too large. The mapping outlives the file.
            try? FileManager.default.removeItem(at: destination)
            return file
        }
        remove(key)
        entries[key] = Entry(info: result.info, url: destination, bytes: result.bytes, truncated: result.truncated, used: clock)
        evict()
        return file
    }

    /// Deletes every downloaded file, as the app does when it quits.
    public func removeAll() {
        entries = [:]; latest = [:]
        Self.reset(directory)
    }

    private func remove(_ key: Key) {
        if let entry = entries.removeValue(forKey: key) { try? FileManager.default.removeItem(at: entry.url) }
    }

    /// Removes the least recently used files until the rest fit the bound.
    private func evict() {
        var total = entries.values.reduce(0) { $0 + $1.bytes }
        while total > maxTotalBytes, let victim = entries.min(by: { $0.value.used < $1.value.used }) {
            total -= victim.value.bytes
            remove(victim.key)
        }
    }

    /// Deletes the shared store's files synchronously, as the app does when it
    /// quits. The store keeps working; a later download recreates the
    /// directory.
    public nonisolated static func deleteSharedFiles() {
        try? FileManager.default.removeItem(at: defaultDirectory())
    }

    /// Empties `directory` and makes it private to the user.
    nonisolated static func reset(_ directory: URL) {
        let manager = FileManager.default
        try? manager.removeItem(at: directory)
        try? manager.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
    }
}
