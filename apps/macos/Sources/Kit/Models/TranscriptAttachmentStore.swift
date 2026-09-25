import AppKit
import ImageIO

/// A bounded cache owned by one transcript, never shared between session hosts.
@MainActor final class TranscriptAttachmentStore {
    final class Content {
        let info: WireAttachmentInfo
        let data: Data
        let image: NSImage?
        let text: String?
        let truncated: Bool
        init(info: WireAttachmentInfo, data: Data, image: NSImage?, text: String?, truncated: Bool) {
            self.info = info; self.data = data; self.image = image; self.text = text; self.truncated = truncated
        }
    }
    enum Failure: LocalizedError {
        case unavailable, invalidImage
        var errorDescription: String? {
            switch self {
            case .unavailable: return "Attachment unavailable"
            case .invalidImage: return "Image preview unavailable"
            }
        }
    }
    let session: String
    let client: any AttachmentClient
    private let cache = NSCache<NSString, Content>()
    init(client: any AttachmentClient, session: String) {
        self.client = client; self.session = session
        cache.totalCostLimit = 64 * 1024 * 1024
    }
    func load(_ reference: TranscriptAttachment) async throws -> Content {
        guard let id = reference.id, !id.isEmpty else { throw Failure.unavailable }
        if let content = cache.object(forKey: id as NSString) { return content }
        let resolved = try await client.resolveAttachments(session, ids: [id])
        guard let info = resolved.attachments?.first(where: { $0.id == id }) else { throw Failure.unavailable }
        let data = try await client.attachmentContent(session, info: info)
        try Task.checkCancellation()
        let decoded = await Task.detached(priority: .utility) {
            var image: CGImage?
            if info.mediaType.hasPrefix("image/"),
               let source = CGImageSourceCreateWithData(data as CFData, nil) {
                image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                    kCGImageSourceCreateThumbnailFromImageAlways: true,
                    kCGImageSourceThumbnailMaxPixelSize: 1200,
                    kCGImageSourceCreateThumbnailWithTransform: true,
                    kCGImageSourceShouldCacheImmediately: true
                ] as CFDictionary)
            }
            let text = info.mediaType.hasPrefix("text/") ? String(data: data, encoding: .utf8) : nil
            return (image, text.map { String($0.prefix(16_384)) }, (text?.count ?? 0) > 16_384)
        }.value
        try Task.checkCancellation()
        if info.mediaType.hasPrefix("image/"), decoded.0 == nil { throw Failure.invalidImage }
        let image = decoded.0.map { NSImage(cgImage: $0, size: .zero) }
        let result = Content(info: info, data: data, image: image, text: decoded.1, truncated: decoded.2)
        let cost = data.count + (decoded.0.map { $0.bytesPerRow * $0.height } ?? 0)
        cache.setObject(result, forKey: id as NSString, cost: cost)
        return result
    }
}
