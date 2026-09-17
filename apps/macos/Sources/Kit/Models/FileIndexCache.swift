import Foundation

struct FileIndex: Sendable {
    let paths: [String]
    var cwd: String? = nil
    var truncated = false
    var notice: String? { truncated ? "File index is incomplete; some paths may be missing." : nil }
}

/// Shared by the two file search surfaces in one session workspace.
actor FileIndexCache {
    struct Key: Equatable, Sendable { let server: String; let session: String; let cwd: String }
    private var key: Key?
    private var cached: (FileIndex, Date)?
    private var pending: Task<FileIndex, Error>?
    private var generation = UUID()
    private var consumers = Set<UUID>()

    func load(key: Key, refresh: Bool = false,
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
    func invalidate() {
        pending?.cancel(); pending = nil; cached = nil; consumers = []; generation = UUID()
    }
    private func release(_ consumer: UUID, token: UUID) {
        guard generation == token else { return }
        consumers.remove(consumer)
        if consumers.isEmpty { pending?.cancel(); pending = nil }
    }
}
