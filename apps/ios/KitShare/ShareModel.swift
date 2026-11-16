import Foundation
import ImageIO
import KitClient
import Observation
import UIKit

/// What the share sheet knows: what was shared, the sessions it can go to,
/// the servers that didn't list theirs, and how adding or sending it went.
@MainActor @Observable
final class ShareModel {
    /// A shared web link.
    struct Link: Equatable {
        let url: URL
        let title: String?
    }

    /// A shared image or file, copied where the sheet can read it.
    struct File: Identifiable, Equatable, Sendable {
        let id = UUID()
        let url: URL
        let name: String
        let isImage: Bool
    }

    enum Shared: Equatable {
        case reading
        case link(Link)
        case files([File])
        /// Some of what was shared couldn't be read.
        case unreadable(String)
        /// Nothing the sheet can add.
        case nothing
    }

    /// Where adding or sending what was shared stands.
    enum Progress: Equatable {
        case idle
        case uploading
        case sending
        /// The connection dropped before the server answered, so the
        /// message may or may not have gone.
        case uncertain
    }

    private(set) var shared = Shared.reading
    /// Small previews of shared images, by file.
    private(set) var previews: [UUID: UIImage] = [:]
    private(set) var destinations: [ShareDestination] = []
    /// Servers that didn't list their sessions, with why.
    private(set) var problems: [(server: String, reason: String)] = []
    /// Whether some server hasn't answered yet.
    private(set) var loading = true
    /// Whether any server is paired.
    private(set) var paired = true
    private(set) var progress = Progress.idle
    /// Why the share couldn't be added or sent, if it couldn't.
    var failure: String?

    private let finish: (Bool) -> Void
    private let inbox: ShareInbox
    /// The files uploaded so far, and the session they went to. A retry to
    /// the same session uploads only the ones that failed.
    private var uploads: (destination: String, attachments: ComposerAttachments, files: [UUID: UUID])?
    private let operations = SessionOperations()
    /// The message being sent, as sent, for keeping it if it may not have gone.
    private var sending: (destination: ShareDestination, text: String, attachments: [String])?
    /// Whether the sheet has closed; nothing finishes it again.
    private var ended = false

    init(inbox: ShareInbox = .live, finish: @escaping (Bool) -> Void) {
        self.inbox = inbox
        self.finish = finish
    }

    func received(link url: URL?, title: String?) {
        guard let url else { shared = .nothing; return }
        let title = title?.trimmingCharacters(in: .whitespacesAndNewlines)
        shared = .link(Link(url: url, title: title?.isEmpty == false && title != url.absoluteString ? title : nil))
    }

    /// The files read, out of how many were shared.
    func received(files: [File], of count: Int) {
        guard files.count == count else {
            shared = .unreadable("Kit couldn’t read \(count - files.count) of the \(count) items. Try sharing them again.")
            return
        }
        guard !files.isEmpty else { shared = .nothing; return }
        shared = .files(Array(files.prefix(8)))
        for file in files where file.isImage { previews[file.id] = Self.preview(file.url) }
    }

    /// Lists every paired server's sessions, each as it answers.
    func load() {
        let servers = (try? PairedServers.saved()) ?? []
        paired = !servers.isEmpty
        guard paired else { loading = false; return }
        Task {
            await withTaskGroup(of: (String, Result<AttentionListing, any Error>).self) { group in
                for server in servers {
                    group.addTask {
                        let client = PairedServers.client(for: server)
                        do { return (server.name, .success(try await client.sessionsByAttention())) }
                        catch { return (server.name, .failure(error)) }
                    }
                }
                for await (server, result) in group {
                    switch result {
                    case .success(let listing):
                        destinations = ShareDestination.ordered(destinations + ShareDestination.listed(listing, on: server))
                    case .failure(let error):
                        problems.append((server, Self.reason(error)))
                    }
                }
            }
            loading = false
        }
    }

    /// The sessions a search finds.
    func destinations(matching search: String) -> [ShareDestination] {
        destinations.filter { $0.matches(search) }
    }

    var busy: Bool { progress == .uploading || progress == .sending }

    /// Adds what was shared, after the note, to the session's draft in Kit,
    /// and closes the sheet. Files are uploaded to the session first.
    func addToDraft(_ destination: ShareDestination, note: String) {
        guard !busy else { return }
        Task {
            guard let (text, attachments) = await prepared(for: destination, note: note) else { return }
            keep(text: text, attachments: attachments, in: destination)
        }
    }

    /// Sends what was shared, after the note, as a message to the session,
    /// and closes the sheet once the server has it. A busy session queues it.
    func send(_ destination: ShareDestination, note: String) {
        guard !busy else { return }
        Task {
            guard let (text, attachments) = await prepared(for: destination, note: note) else { return }
            progress = .sending
            sending = (destination, text, attachments)
            let server = (try? PairedServers.saved())?.first { $0.name == destination.server }
            guard let server else {
                progress = .idle
                failure = "\(destination.server) isn’t paired anymore."
                return
            }
            operations.submit(client: PairedServers.client(for: server), session: destination.session,
                              input: WirePromptInput(text: text, attachmentIds: attachments, annotationIds: []),
                              draft: .init(text: text, notes: [:], attachmentIDs: attachments),
                              uncertain: {}, acknowledged: {})
            while operations.submission == .pending { try? await Task.sleep(for: .milliseconds(50)) }
            switch operations.submission {
            case .acknowledged: end(true)
            case .uncertain: progress = .uncertain
            case .failed(let reason), .rejected(let reason):
                progress = .idle
                failure = reason
            case .idle, .pending: progress = .idle
            }
        }
    }

    /// After a send that may not have gone: keeps the message in the
    /// session's draft instead, to check and send again from Kit.
    func keepUnsent() {
        guard progress == .uncertain, let sending else { return }
        keep(text: sending.text, attachments: sending.attachments, in: sending.destination)
    }

    func cancel() {
        guard !busy else { return }
        end(false)
    }

    func done() {
        end(true)
    }

    private func end(_ done: Bool) {
        guard !ended else { return }
        ended = true
        finish(done)
    }

    // MARK: Preparing

    private var sharedText: [String] {
        if case .link(let link) = shared { return [link.url.absoluteString] }
        return []
    }

    /// The message's words and its attachments' IDs, once every file is
    /// uploaded to the session; nil, with the reason shown, if one isn't.
    private func prepared(for destination: ShareDestination, note: String) async -> (String, [String])? {
        let text = SharedItem.text(note: note, shared: sharedText)
        guard case .files(let files) = shared else { return (text, []) }
        if let model = destination.imageRejectingModel, files.contains(where: \.isImage) {
            failure = "\(model) doesn’t take images."
            return nil
        }
        guard let server = (try? PairedServers.saved())?.first(where: { $0.name == destination.server }) else {
            failure = "\(destination.server) isn’t paired anymore."
            return nil
        }
        progress = .uploading
        defer { if progress == .uploading { progress = .idle } }
        let client = PairedServers.client(for: server)
        if uploads?.destination != destination.id {
            uploads = (destination.id, ComposerAttachments(), [:])
        }
        guard var current = uploads else { return nil }
        // One at a time, so only one file's bytes are being prepared at once.
        for file in files {
            if let item = current.files[file.id] {
                if current.attachments.items.first(where: { $0.id == item })?.error != nil {
                    current.attachments.retry(item, client: client, session: destination.session)
                }
            } else {
                let url = file.url, name = file.name, isImage = file.isImage
                let upload = await Task.detached { () -> Result<(data: Data, filename: String), any Error> in
                    Result { PhotoPreparation.forUpload(try SharedFiles.read(url, name: name, isImage: isImage), filename: name) }
                }.value
                guard let reserved = current.attachments.reserve(filename: name) else {
                    failure = current.attachments.error
                    uploads = current
                    return nil
                }
                current.files[file.id] = reserved
                uploads = current
                switch upload {
                case .success(let upload):
                    current.attachments.add(filename: upload.filename, data: upload.data, client: client,
                                            session: destination.session, reserved: reserved)
                case .failure(let error):
                    current.attachments.failReserved(reserved, error: error)
                }
            }
            while current.attachments.items.contains(where: \.busy) { try? await Task.sleep(for: .milliseconds(100)) }
        }
        uploads = current
        if let failed = current.attachments.items.first(where: { $0.error != nil }) {
            failure = "\(failed.filename): \(failed.error ?? "")"
            return nil
        }
        return (text, current.attachments.readyIDs)
    }

    private func keep(text: String, attachments: [String], in destination: ShareDestination) {
        do {
            try inbox.put(SharedItem(server: destination.server, session: destination.session,
                                     text: text, attachmentIDs: attachments))
            end(true)
        } catch {
            failure = error.localizedDescription
        }
    }

    private static func preview(_ url: URL) -> UIImage? {
        guard let source = CGImageSourceCreateWithURL(url as CFURL, nil),
              let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                  kCGImageSourceCreateThumbnailFromImageAlways: true,
                  kCGImageSourceCreateThumbnailWithTransform: true,
                  kCGImageSourceThumbnailMaxPixelSize: 160
              ] as CFDictionary) else { return nil }
        return UIImage(cgImage: image)
    }

    private static func reason(_ error: any Error) -> String {
        if let error = error as? ClientError {
            switch error {
            case .needsPairing, .http(401): return "Pair it again in Kit."
            case .incompatibleDaemon: return "Update Kit or the server."
            default: return error.localizedDescription
            }
        }
        if error is URLError { return "It didn’t answer." }
        return error.localizedDescription
    }
}
