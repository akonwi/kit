import AppKit
import SwiftUI

extension OpenURLAction {
    /// Opens web links in a browser tab beside `anchor`; ⌘-click and other
    /// schemes use the system's default handler. `handle` may take a link
    /// first, unless it's ⌘-clicked.
    @MainActor static func workspace(_ workspace: WorkspaceState, beside anchor: WorkspacePane,
                                     handle: @escaping @MainActor (URL) -> Bool = { _ in false }) -> OpenURLAction {
        OpenURLAction { [weak workspace] url in
            let externally = (NSApp.currentEvent?.modifierFlags ?? NSEvent.modifierFlags).contains(.command)
            if !externally, handle(url) { return .handled }
            return workspace?.openLink(url, beside: anchor, externally: externally) == true ? .handled : .systemAction
        }
    }

    /// Transcript links: a session mention opens its session's tab, however
    /// it's clicked; other links open as `workspace` opens them.
    @MainActor static func transcript(_ workspace: WorkspaceState, beside anchor: WorkspacePane,
                                      sessionLink: TranscriptSessionLink?) -> OpenURLAction {
        let other = OpenURLAction.workspace(workspace, beside: anchor)
        return OpenURLAction { url in
            if let id = SessionMentions.session(in: url) {
                sessionLink?.open(id)
                return .handled
            }
            other(url)
            return .handled
        }
    }
}
