import AppKit
import SwiftUI

/// A terminal tab's pane: the shell, drawn edge to edge in the Kit theme.
/// The terminal is in a window only while its tab is on screen, so a
/// hidden terminal stops drawing while its shell keeps running.
struct TerminalPane: View {
    @Environment(\.mica) private var theme
    let workspace: WorkspaceState
    let tab: TerminalTab
    @State private var hosted: NSView?

    var body: some View {
        let pane = WorkspacePane.terminal(tab.id)
        let appearance = TerminalAppearance(theme: theme)
        let visible = workspace.isVisible(pane)
        ZStack {
            theme.surface
            if let failure = tab.failure {
                BrowserFailureView(failure: BrowserFailure(title: "Can’t open a terminal", detail: failure, retry: nil)) {}
            } else if visible, let hosted {
                LiveViewHost(view: hosted).workspaceFocusTarget(pane)
            }
        }
        .task(id: visible) {
            if visible, hosted == nil { hosted = tab.view(appearance: appearance) }
            if visible { tab.bell = false }
        }
        .onChange(of: appearance) { TerminalRuntime.shared.apply(appearance) }
    }
}
