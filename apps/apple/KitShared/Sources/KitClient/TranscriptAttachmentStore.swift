import CoreGraphics
import Foundation
import ImageIO

/// A bounded cache owned by one transcript, never shared between session hosts.
@MainActor public final class TranscriptAttachmentStore {
    /// A downloaded attachment: its bytes, and a preview of an image or text.
    public final class Content {
        public let info: WireAttachmentInfo
        public let data: Data
        /// An image's preview, at most 1200 pixels on its longer side.
        public let image: CGImage?
        /// The start of a text file.
        public let text: String?
        /// Whether `text` stops before the file's end.
        public let truncated: Bool
        public init(info: WireAttachmentInfo, data: Data, image: CGImage?, text: String?, truncated: Bool) {
            self.info = info; self.data = data; self.image = image; self.text = text; self.truncated = truncated
        }
    }
    public enum Failure: LocalizedError {
        case unavailable, invalidImage
        public var errorDescription: String? {
            switch self {
            case .unavailable: return "Attachment unavailable"
            case .invalidImage: return "Image preview unavailable"
            }
        }
    }
    public let session: String
    public let client: any AttachmentClient
    private let cache = NSCache<NSString, Content>()
    public init(client: any AttachmentClient, session: String) {
        self.client = client; self.session = session
        cache.totalCostLimit = 64 * 1024 * 1024
    }
    public func load(_ reference: TranscriptAttachment) async throws -> Content {
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
        let result = Content(info: info, data: data, image: decoded.0, text: decoded.1, truncated: decoded.2)
        let cost = data.count + (decoded.0.map { $0.bytesPerRow * $0.height } ?? 0)
        cache.setObject(result, forKey: id as NSString, cost: cost)
        return result
    }
}
