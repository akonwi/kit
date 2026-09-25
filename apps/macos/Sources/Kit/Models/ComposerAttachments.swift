import AppKit
import ImageIO
import Observation

/// A draft owns upload tasks and bytes independently of composer view lifetime.
@MainActor @Observable
final class ComposerAttachments {
    struct Item: Identifiable {
        let id: UUID
        var filename: String
        var data: Data?
        var preview: NSImage?
        var uploaded: WireAttachmentInfo?
        var error: String?
        var busy = true
    }
    var items: [Item] = []
    var error: String?
    @ObservationIgnored private var tasks: [UUID: Task<Void, Never>] = [:]
    static func uniqueIDs(_ values: [String]) -> [String] {
        var seen = Set<String>()
        return values.filter { seen.insert($0).inserted }
    }
    var readyIDs: [String] { items.compactMap { $0.uploaded?.id } }
    var ready: Bool { items.allSatisfy { $0.uploaded != nil } }
    deinit { for task in tasks.values { task.cancel() } }

    func remove(_ id: UUID) {
        tasks.removeValue(forKey: id)?.cancel()
        items.removeAll { $0.id == id }
    }
    func acknowledge(_ ids: [String]) {
        for item in items where item.uploaded.map({ ids.contains($0.id) }) == true { remove(item.id) }
    }
    func add(url: URL, client: any ComposerClient, session: String) {
        add(filename: url.lastPathComponent, client: client, session: session) {
            let access = url.startAccessingSecurityScopedResource()
            defer { if access { url.stopAccessingSecurityScopedResource() } }
            guard try url.resourceValues(forKeys: [.isRegularFileKey]).isRegularFile == true else {
                throw MutationNotSent(reason: "Choose a regular file.")
            }
            let file = try FileHandle(forReadingFrom: url)
            defer { try? file.close() }
            return try file.read(upToCount: 10 * 1024 * 1024 + 1) ?? Data()
        }
    }
    func add(filename: String, data: Data, client: any ComposerClient, session: String) {
        add(filename: filename, client: client, session: session) { data }
    }
    private func add(filename: String, client: any ComposerClient, session: String,
                     read: @escaping @Sendable () throws -> Data) {
        guard items.count < 8 else { error = "A message can contain up to eight attachments."; return }
        error = nil
        let id = UUID()
        items.append(Item(id: id, filename: filename))
        tasks[id] = Task { [weak self] in
            do {
                let prepared = try await Task.detached { try Self.prepare(read(), filename: filename) }.value
                try Task.checkCancellation()
                guard let self, let index = self.items.firstIndex(where: { $0.id == id }) else { return }
                guard self.items.compactMap(\.data).reduce(prepared.data.count, { $0 + $1.count }) <= 20 * 1024 * 1024 else {
                    throw MutationNotSent(reason: "Attachments must total 20 MB or less.")
                }
                self.items[index].filename = prepared.filename
                self.items[index].data = prepared.data
                self.items[index].preview = prepared.preview.map { NSImage(cgImage: $0, size: .zero) }
                await self.upload(id, client: client, session: session)
            } catch is CancellationError {} catch { self?.fail(id, error: error) }
        }
    }
    private struct Prepared: Sendable {
        let filename: String
        let data: Data
        let preview: CGImage?
    }
    nonisolated private static func prepare(_ input: Data, filename: String) throws -> Prepared {
        guard !input.isEmpty, input.count <= 10 * 1024 * 1024 else {
            throw MutationNotSent(reason: "Choose a nonempty image up to 10 MB, or a text file up to 1 MB.")
        }
        guard let source = CGImageSourceCreateWithData(input as CFData, nil),
              let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any] else {
            guard input.count <= 1024 * 1024, !input.contains(0), String(data: input, encoding: .utf8) != nil else {
                throw MutationNotSent(reason: "Attach a PNG, JPEG, GIF, WebP, or UTF-8 text file up to 1 MB.")
            }
            return Prepared(filename: filename, data: input, preview: nil)
        }
        let width = properties[kCGImagePropertyPixelWidth] as? Int ?? 0
        let height = properties[kCGImagePropertyPixelHeight] as? Int ?? 0
        guard width > 0, height > 0, width <= 8192, height <= 8192, width * height <= 12_000_000 else {
            throw MutationNotSent(reason: "Images must be at most 8192 pixels per side and 12 megapixels.")
        }
        var data = input, name = filename
        if let type = CGImageSourceGetType(source) as String?,
           !["public.png", "public.jpeg", "com.compuserve.gif", "org.webmproject.webp"].contains(type) {
            let converted = NSMutableData()
            guard let destination = CGImageDestinationCreateWithData(converted, "public.png" as CFString, 1, nil) else {
                throw MutationNotSent(reason: "Couldn’t prepare the image for upload.")
            }
            CGImageDestinationAddImageFromSource(destination, source, 0, nil)
            guard CGImageDestinationFinalize(destination), converted.length <= 10 * 1024 * 1024 else {
                throw MutationNotSent(reason: "The converted image exceeds the 10 MB attachment limit.")
            }
            data = converted as Data
            name = (filename as NSString).deletingPathExtension + ".png"
        }
        let preview = CGImageSourceCreateThumbnailAtIndex(source, 0, [kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true, kCGImageSourceThumbnailMaxPixelSize: 320] as CFDictionary)
        return Prepared(filename: name, data: data, preview: preview)
    }
    func retry(_ id: UUID, client: any ComposerClient, session: String) {
        guard items.first(where: { $0.id == id })?.data != nil else { return }
        tasks[id]?.cancel()
        tasks[id] = Task { [weak self] in await self?.upload(id, client: client, session: session) }
    }
    private func upload(_ id: UUID, client: any ComposerClient, session: String) async {
        guard let index = items.firstIndex(where: { $0.id == id }), let data = items[index].data else { return }
        items[index].busy = true; items[index].error = nil
        let filename = items[index].filename
        do {
            let info = try await client.upload(session, filename: filename, data: data)
            try Task.checkCancellation()
            guard let index = items.firstIndex(where: { $0.id == id }) else { return }
            items[index].uploaded = info; items[index].busy = false
        } catch is CancellationError {} catch { fail(id, error: error) }
        tasks[id] = nil
    }
    private func fail(_ id: UUID, error: Error) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        items[index].error = error.localizedDescription; items[index].busy = false
        tasks[id] = nil
    }
}
