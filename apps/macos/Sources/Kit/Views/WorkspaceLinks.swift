import AppKit
import SwiftUI

extension OpenURLAction {
    /// Opens web links in a browser tab beside `anchor`; ⌘-click and other
    /// schemes use the system's default handler.
    @MainActor static func workspace(_ workspace: WorkspaceState, beside anchor: WorkspacePane) -> OpenURLAction {
        OpenURLAction { [weak workspace] url in
            let externally = (NSApp.currentEvent?.modifierFlags ?? NSEvent.modifierFlags).contains(.command)
            return workspace?.openLink(url, beside: anchor, externally: externally) == true ? .handled : .systemAction
        }
    }
}
