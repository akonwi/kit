import Foundation
import ImageIO

/// A file Kit shows from outside the session's workspace: one on the session's
/// server, read through the session API (ADR 0050), or one on this Mac, read
/// by the app. Both are bounded the same way and view-only.
struct ViewedFile: Equatable, Sendable {
    enum Content: Equatable, Sendable {
        /// A UTF-8 preview, cut at `ViewedFile.maxTextBytes` or `maxTextLines`.
        case text(String, truncated: Bool)
        /// A complete PNG, JPEG, GIF, or WebP file.
        case image(Data, mediaType: String, width: Int, height: Int)
    }

    /// The absolute path that was read.
    let path: String
    /// The file's size in bytes.
    let size: Int64
    let content: Content

    static let maxTextBytes = 1_000_000
    static let maxTextLines = 5_000
    static let maxImageBytes = 16 * 1024 * 1024
    static let maxImagePixels = 24_000_000
    static let imageTypes: Set<String> = ["image/png", "image/jpeg", "image/gif", "image/webp"]
}

/// Why a file outside the workspace can't be shown.
enum ViewedFileError: LocalizedError, Equatable {
    case notFound, notFile, permissionDenied, unsupported, imageTooLarge, invalidPath, busy, changed, unavailable
    /// The server predates host file reads.
    case serverNeedsUpdate

    var errorDescription: String? {
        switch self {
        case .notFound: "This file doesn’t exist."
        case .notFile: "This path isn’t a file."
        case .permissionDenied: "Kit doesn’t have permission to read this file."
        case .unsupported: "Kit shows text files and PNG, JPEG, GIF, or WebP images."
        case .imageTooLarge: "This image is larger than Kit shows (16 MB or 24 megapixels)."
        case .invalidPath: "This isn’t a path Kit can open."
        case .busy: "The server is busy. Try again shortly."
        case .changed: "The file changed while it was read. Refresh to try again."
        case .unavailable: "Couldn’t read this file."
        case .serverNeedsUpdate: "This server’s Kit is too old to open files outside the working directory."
        }
    }
}

/// Reads files on a session's server, anywhere on its machine.
protocol HostFileClient: SessionClient {
    func readHostFile(_ session: String, path: String) async throws -> ViewedFile
}

extension ServerClient: HostFileClient {
    func readHostFile(_ session: String, path: String) async throws -> ViewedFile {
        try await resolve().readHostFile(session, path: path)
    }
}

extension ViewedFile {
    /// Projects a server read, checking it answers `requested` in `session`
    /// and keeps within the advertised bounds.
    static func project(_ read: Components.Schemas.HostFileRead, session: String, requested: String) throws -> ViewedFile {
        func matches(_ path: String) -> Bool {
            path.hasPrefix("/") && (requested.hasPrefix("~") || path == FilePaths.clean(requested))
        }
        switch read {
        case .text(let text):
            let content = text.content ?? ""
            guard text.sessionId == session, matches(text.path), text.encoding == "utf-8",
                  content.utf8.count == (text.returnedBytes ?? 0), content.utf8.count <= maxTextBytes,
                  (text.returnedLines ?? 0) <= maxTextLines else { throw ClientError.invalidPayload }
            return ViewedFile(path: text.path, size: text.size ?? 0, content: .text(content, truncated: text.truncated == true))
        case .image(let image):
            guard image.sessionId == session, matches(image.path), imageTypes.contains(image.mediaType),
                  let data = Data(base64Encoded: image.data), data.count == image.size, data.count <= maxImageBytes,
                  image.width > 0, image.height > 0, image.width * image.height <= maxImagePixels else { throw ClientError.invalidPayload }
            return ViewedFile(path: image.path, size: image.size,
                              content: .image(data, mediaType: image.mediaType, width: image.width, height: image.height))
        }
    }

    /// Reads a file on this Mac with the bounds a server read has. Blocks;
    /// call it off the main actor.
    static func readLocal(path: String) throws -> ViewedFile {
        guard path.hasPrefix("/"), !path.contains("\0") else { throw ViewedFileError.invalidPath }
        // Only a regular file is opened: opening a device or socket can have
        // side effects, or block, even without reading it.
        var initial = stat()
        guard stat(path, &initial) == 0 else { throw localError(errno) }
        guard initial.st_mode & S_IFMT == S_IFREG else { throw ViewedFileError.notFile }
        let descriptor = open(path, O_RDONLY | O_NONBLOCK | O_NOCTTY | O_CLOEXEC)
        guard descriptor >= 0 else { throw localError(errno) }
        let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        var status = stat()
        guard fstat(descriptor, &status) == 0 else { throw localError(errno) }
        guard status.st_mode & S_IFMT == S_IFREG, status.st_dev == initial.st_dev, status.st_ino == initial.st_ino else {
            throw ViewedFileError.changed
        }
        let size = Int64(status.st_size)
        let header = try handle.read(upToCount: 16) ?? Data()
        if let mediaType = imageType(header) {
            guard size <= maxImageBytes else { throw ViewedFileError.imageTooLarge }
            let data = header + (try handle.read(upToCount: maxImageBytes + 1 - header.count) ?? Data())
            guard data.count <= maxImageBytes, Int64(data.count) == size else {
                throw data.count > maxImageBytes ? ViewedFileError.imageTooLarge : ViewedFileError.changed
            }
            guard let source = CGImageSourceCreateWithData(data as CFData, nil),
                  let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
                  let width = properties[kCGImagePropertyPixelWidth] as? Int,
                  let height = properties[kCGImagePropertyPixelHeight] as? Int, width > 0, height > 0 else {
                throw ViewedFileError.unsupported
            }
            guard width * height <= maxImagePixels else { throw ViewedFileError.imageTooLarge }
            return ViewedFile(path: path, size: size, content: .image(data, mediaType: mediaType, width: width, height: height))
        }
        let data = header + (try handle.read(upToCount: maxTextBytes + 4 - header.count) ?? Data())
        let (text, truncated) = try preview(data, complete: Int64(data.count) >= size)
        return ViewedFile(path: path, size: size, content: .text(text, truncated: truncated))
    }

    /// The supported image type `header` begins, or nil.
    static func imageType(_ header: Data) -> String? {
        let bytes = [UInt8](header.prefix(12))
        if bytes.starts(with: [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]) { return "image/png" }
        if bytes.starts(with: [0xFF, 0xD8, 0xFF]) { return "image/jpeg" }
        if bytes.starts(with: Array("GIF87a".utf8)) || bytes.starts(with: Array("GIF89a".utf8)) { return "image/gif" }
        if bytes.count == 12, bytes.starts(with: Array("RIFF".utf8)), Array(bytes[8..<12]) == Array("WEBP".utf8) { return "image/webp" }
        return nil
    }

    /// A bounded UTF-8 preview of a file's first bytes, as the server cuts
    /// one: at a character boundary within `maxTextBytes`, then at
    /// `maxTextLines` complete lines.
    static func preview(_ data: Data, complete: Bool) throws -> (String, truncated: Bool) {
        guard !data.contains(0) else { throw ViewedFileError.unsupported }
        var bytes = data
        var truncated = !complete
        if bytes.count > maxTextBytes { bytes = bytes.prefix(maxTextBytes); truncated = true }
        var text = String(data: bytes, encoding: .utf8)
        if text == nil, truncated {
            for cut in 1..<4 where bytes.count > cut {
                if let trimmed = String(data: bytes.dropLast(cut), encoding: .utf8) { text = trimmed; break }
            }
        }
        guard var text else { throw ViewedFileError.unsupported }
        var lines = 0
        var end = text.utf8.startIndex
        for index in text.utf8.indices where text.utf8[index] == UInt8(ascii: "\n") {
            lines += 1
            end = text.utf8.index(after: index)
            if lines == maxTextLines { break }
        }
        if lines == maxTextLines, end != text.utf8.endIndex {
            text = String(text[..<end])
            truncated = true
        }
        return (text, truncated)
    }

    private static func localError(_ code: Int32) -> ViewedFileError {
        switch code {
        case ENOENT: .notFound
        case EACCES, EPERM: .permissionDenied
        case EISDIR, ENOTDIR, ELOOP, ENXIO, EOPNOTSUPP, ENODEV: .notFile
        case ENAMETOOLONG: .invalidPath
        default: .unavailable
        }
    }
}
