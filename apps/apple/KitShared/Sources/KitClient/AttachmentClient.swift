import Foundation

/// Attachment reads are scoped to the owning session and its server.
public protocol AttachmentClient: SessionClient {
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution
    func attachmentContent(_ session: String, info: WireAttachmentInfo) async throws -> Data
}

extension ServerClient: AttachmentClient {
    public func attachmentContent(_ session: String, info: WireAttachmentInfo) async throws -> Data {
        try await resolve().attachmentContent(session, info: info)
    }
}
