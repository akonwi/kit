import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// Where the share sheet starts: it reads what was shared, shows the sheet,
/// and ends the share when it's added or cancelled.
final class ShareViewController: UIViewController {
    private lazy var model = ShareModel { [weak self] added in self?.finish(added) }

    override func viewDidLoad() {
        super.viewDidLoad()
        let host = UIHostingController(rootView: ShareView(model: model).tint(Theme.accent))
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
        readShared()
        model.load()
    }

    /// The first web link shared, with the page's title when the app gave one.
    private func readShared() {
        let items = extensionContext?.inputItems as? [NSExtensionItem] ?? []
        let type = UTType.url.identifier
        for item in items {
            let title = (item.attributedTitle ?? item.attributedContentText)?.string
            guard let provider = item.attachments?.first(where: { $0.hasItemConformingToTypeIdentifier(type) }) else { continue }
            provider.loadItem(forTypeIdentifier: type) { [model] value, _ in
                let url = value as? URL
                Task { @MainActor in model.received(link: url, title: title) }
            }
            return
        }
        model.received(link: nil, title: nil)
    }

    private func finish(_ added: Bool) {
        if added {
            extensionContext?.completeRequest(returningItems: nil)
        } else {
            extensionContext?.cancelRequest(withError: CocoaError(.userCancelled))
        }
    }
}
