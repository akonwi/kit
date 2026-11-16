import Foundation
import ImageIO
import UniformTypeIdentifiers

/// Phone photos are larger than an attachment may be, so they go as JPEG at
/// most 3072 pixels on the longer side.
enum PhotoPreparation {
    static let maxPixels = 3072

    /// The photo as a JPEG that fits, or nil when it isn't an image.
    nonisolated static func jpeg(_ data: Data) -> Data? {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil),
              let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                  kCGImageSourceCreateThumbnailFromImageAlways: true,
                  kCGImageSourceCreateThumbnailWithTransform: true,
                  kCGImageSourceThumbnailMaxPixelSize: maxPixels
              ] as CFDictionary) else { return nil }
        let output = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(output, UTType.jpeg.identifier as CFString, 1, nil) else {
            return nil
        }
        CGImageDestinationAddImage(destination, image, [kCGImageDestinationLossyCompressionQuality: 0.85] as CFDictionary)
        guard CGImageDestinationFinalize(destination) else { return nil }
        return output as Data
    }
}

extension PhotoPreparation {
    /// A shared file as it uploads: an image the server takes as it is
    /// (PNG, JPEG, GIF, or WebP, at most 10 MB and 12 megapixels) keeps its
    /// bytes and name; any other image goes as a JPEG named for it; other
    /// files go as they are.
    nonisolated static func forUpload(_ data: Data, filename: String) -> (data: Data, filename: String) {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil), let type = CGImageSourceGetType(source),
              let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any] else {
            return (data, filename)
        }
        let width = properties[kCGImagePropertyPixelWidth] as? Int ?? 0
        let height = properties[kCGImagePropertyPixelHeight] as? Int ?? 0
        let accepted = [UTType.png, .jpeg, .gif, .webP].map(\.identifier).contains(type as String)
        if accepted, data.count <= 10 * 1024 * 1024, width * height <= 12_000_000 { return (data, filename) }
        guard let jpeg = jpeg(data) else { return (data, filename) }
        return (jpeg, (filename as NSString).deletingPathExtension + ".jpg")
    }
}
