import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// Where the share sheet starts: it reads what was shared, shows the sheet,
/// and ends the share when it's added, sent, or cancelled.
final class ShareViewController: UIViewController {
    private lazy var model = ShareModel { [weak self] done in self?.finish(done) }

    override func viewDidLoad() {
        super.viewDidLoad()
        let host = UIHostingController(rootView: ShareView(model: model).tint(Theme.accent))
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
        model.load()
        let items = extensionContext?.inputItems as? [NSExtensionItem] ?? []
        Task { await readShared(items) }
    }

    /// A web link when one was shared, with the page's title when the app
    /// gave one; otherwise the images and files, up to eight.
    private func readShared(_ items: [NSExtensionItem]) async {
        let url = UTType.url.identifier
        for item in items {
            for provider in item.attachments ?? [] where provider.hasItemConformingToTypeIdentifier(url) {
                let link = await withCheckedContinuation { continuation in
                    provider.loadItem(forTypeIdentifier: url) { value, _ in continuation.resume(returning: value as? URL) }
                }
                guard let link, !link.isFileURL else { continue }
                model.received(link: link, title: (item.attributedTitle ?? item.attributedContentText)?.string)
                return
            }
        }
        var files: [ShareModel.File] = []
        for provider in items.flatMap({ $0.attachments ?? [] }) where files.count < 8 {
            if provider.hasItemConformingToTypeIdentifier(UTType.image.identifier) {
                if let file = await copy(provider, type: UTType.image.identifier, isImage: true) { files.append(file) }
            } else if let type = Self.fileType(of: provider), let file = await copy(provider, type: type, isImage: false) {
                files.append(file)
            }
        }
        model.received(files: files)
    }

    /// A file's type, when the provider has file data rather than a web link.
    private static func fileType(of provider: NSItemProvider) -> String? {
        provider.registeredTypeIdentifiers.first { identifier in
            guard let type = UTType(identifier) else { return false }
            return type.conforms(to: .data) && !type.conforms(to: .url)
        }
    }

    /// Copies a shared file somewhere the sheet can read it after the
    /// provider lets its own copy go.
    private func copy(_ provider: NSItemProvider, type: String, isImage: Bool) async -> ShareModel.File? {
        let suggested = provider.suggestedName
        return await withCheckedContinuation { continuation in
            _ = provider.loadFileRepresentation(forTypeIdentifier: type) { url, _ in
                continuation.resume(returning: url.flatMap { Self.keep($0, suggested: suggested, isImage: isImage) })
            }
        }
    }

    private nonisolated static func keep(_ url: URL, suggested: String?, isImage: Bool) -> ShareModel.File? {
        let manager = FileManager.default
        let folder = manager.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let name = url.lastPathComponent.isEmpty ? (suggested ?? "File") : url.lastPathComponent
        let copy = folder.appendingPathComponent(name)
        do {
            try manager.createDirectory(at: folder, withIntermediateDirectories: true)
            try manager.copyItem(at: url, to: copy)
        } catch {
            return nil
        }
        return ShareModel.File(url: copy, name: name, isImage: isImage)
    }

    private func finish(_ done: Bool) {
        if done {
            extensionContext?.completeRequest(returningItems: nil)
        } else {
            extensionContext?.cancelRequest(withError: CocoaError(.userCancelled))
        }
    }
}
