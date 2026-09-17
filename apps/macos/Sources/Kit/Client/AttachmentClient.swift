import Foundation

/// Attachment reads are scoped to the owning session and its server.
protocol AttachmentClient: SessionClient {
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution
    func attachmentContent(_ session: String, info: WireAttachmentInfo) async throws -> Data
}

extension LocalClient: AttachmentClient {
    func attachmentContent(_ session: String, info: WireAttachmentInfo) async throws -> Data {
        try await HTTPClient.local().attachmentContent(session, info: info)
    }
}
