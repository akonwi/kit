import Foundation

/// Composer operations against the session host; presentation never reads its filesystem.
protocol ModelCatalogRefreshClient: SessionClient {
    func refreshModels() async throws -> [WireModelCapability]
}

protocol ComposerClient: SessionClient {
    func fileIndex(_ session: String, refresh: Bool) async throws -> FileIndex
    func models() async throws -> [WireModelCapability]
    func configure(_ session: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult
    func files(_ session: String) async throws -> [String]
    func files(_ session: String, refresh: Bool) async throws -> [String]
    func upload(_ session: String, filename: String, data: Data) async throws -> WireAttachmentInfo
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution
    func respond(_ session: String, input: WireInteractionResponse) async throws
}

extension ComposerClient {
    func fileIndex(_ session: String, refresh: Bool) async throws -> FileIndex {
        FileIndex(paths: try await files(session, refresh: refresh))
    }
    func files(_ session: String, refresh: Bool) async throws -> [String] { try await files(session) }
}

extension LocalClient: ModelCatalogRefreshClient {
    func refreshModels() async throws -> [WireModelCapability] { try await HTTPClient.local().refreshModels() }
}

extension LocalClient: ComposerClient {
    func fileIndex(_ session: String, refresh: Bool) async throws -> FileIndex {
        try await HTTPClient.local().fileIndex(session, refresh: refresh)
    }
    func configure(_ session: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult {
        try await HTTPClient.local().configure(session, input: input)
    }
    func files(_ session: String) async throws -> [String] { try await HTTPClient.local().files(session) }
    func files(_ session: String, refresh: Bool) async throws -> [String] { try await HTTPClient.local().files(session, refresh: refresh) }
    func upload(_ session: String, filename: String, data: Data) async throws -> WireAttachmentInfo {
        try await HTTPClient.local().upload(session, filename: filename, data: data)
    }
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution {
        try await HTTPClient.local().resolveAttachments(session, ids: ids)
    }
    func respond(_ session: String, input: WireInteractionResponse) async throws {
        try await HTTPClient.local().respond(session, input: input)
    }
}
