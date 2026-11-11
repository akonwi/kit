import AppKit
import KitTranscript
import SwiftUI

extension OpenURLAction {
    /// Whether a link opening now was ⌘-clicked, so it opens with the
    /// system's default handler.
    @MainActor static func clickedExternally(_ url: URL) -> Bool {
        TextLinkHover.takeCommandClick(for: url)
            || (NSApp.currentEvent?.modifierFlags ?? NSEvent.modifierFlags).contains(.command)
    }

    /// Opens web links in a browser tab beside `anchor` and file links in
    /// their tabs; links `externally` says to, ⌘-clicked ones by default,
    /// open in the default browser, or a file on this Mac in its default
    /// app, and other schemes use the system's default handler. `handle` may take a link first, unless it
    /// opens externally.
    @MainActor static func workspace(_ workspace: WorkspaceState, beside anchor: WorkspacePane,
                                     externally opensExternally: @escaping @MainActor (URL) -> Bool = clickedExternally,
                                     handle: @escaping @MainActor (URL) -> Bool = { _ in false }) -> OpenURLAction {
        OpenURLAction { [weak workspace] url in
            let externally = opensExternally(url)
            if !externally, handle(url) { return .handled }
            return workspace?.openLink(url, beside: anchor, externally: externally) == true ? .handled : .systemAction
        }
    }

    /// Transcript links: a session mention opens its session's tab, however
    /// it's clicked; other links open as `workspace` opens them.
    @MainActor static func transcript(_ workspace: WorkspaceState, beside anchor: WorkspacePane,
                                      sessionLink: TranscriptSessionLink?,
                                      externally: @escaping @MainActor (URL) -> Bool = clickedExternally) -> OpenURLAction {
        let other = OpenURLAction.workspace(workspace, beside: anchor, externally: externally)
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

extension TextLinkHover.LinkActions {
    /// A transcript's link menus: Open Link, Open File, and Open Session
    /// open as a plain click on a row's link does, whatever keys are held,
    /// and a file link names the file it opens.
    @MainActor static func transcript(_ workspace: WorkspaceState, beside anchor: WorkspacePane,
                                      sessionLink: TranscriptSessionLink?) -> Self {
        let open = OpenURLAction.transcript(workspace, beside: anchor, sessionLink: sessionLink, externally: { _ in false })
        return .init(open: { open($0) }, file: { [weak workspace] in workspace?.fileActions(forLink: $0) })
    }
}
