import Foundation

public struct FileIndex: Sendable {
    public let paths: [String]
    public var cwd: String? = nil
    public var truncated = false
    public var notice: String? { truncated ? "File index is incomplete; some paths may be missing." : nil }

    public init(paths: [String], cwd: String? = nil, truncated: Bool = false) {
        self.paths = paths
        self.cwd = cwd
        self.truncated = truncated
    }
}

/// Shared by the two file search surfaces in one session workspace.
public actor FileIndexCache {
    public init() {}

    public struct Key: Equatable, Sendable {
        public let server: String
        public let session: String
        public let cwd: String

        public init(server: String, session: String, cwd: String) {
            self.server = server
            self.session = session
            self.cwd = cwd
        }
    }
    private var key: Key?
    private var cached: (FileIndex, Date)?
    private var pending: Task<FileIndex, Error>?
    private var generation = UUID()
    private var consumers = Set<UUID>()

    public func load(key: Key, refresh: Bool = false,
              loader: @escaping @Sendable (Bool) async throws -> FileIndex) async throws -> FileIndex {
        if self.key != key { invalidate(); self.key = key }
        if refresh { invalidate(); self.key = key }
        if let cached, Date().timeIntervalSince(cached.1) < 300 { return cached.0 }
        if pending == nil {
            pending = Task { try await loader(refresh) }
        }
        let task = pending!, token = generation, consumer = UUID()
        consumers.insert(consumer)
        return try await withTaskCancellationHandler {
            do {
                let result = try await task.value
                try Task.checkCancellation()
                guard generation == token, self.key == key else { throw CancellationError() }
                guard result.cwd == nil || result.cwd == key.cwd else { throw ClientError.invalidPayload }
                cached = (result, Date())
                release(consumer, token: token)
                return result
            } catch {
                release(consumer, token: token)
                throw error
            }
        } onCancel: {
            Task { await self.release(consumer, token: token) }
        }
    }
    public func invalidate() {
        pending?.cancel(); pending = nil; cached = nil; consumers = []; generation = UUID()
    }
    private func release(_ consumer: UUID, token: UUID) {
        guard generation == token else { return }
        consumers.remove(consumer)
        if consumers.isEmpty { pending?.cancel(); pending = nil }
    }
}
