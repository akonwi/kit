import AppKit

/// Asks before closing ends commands still running in terminals. At an
/// idle prompt, closing goes ahead without asking.
@MainActor
enum TerminalCloseConfirmation {
    enum Scope {
        /// One terminal tab.
        case tab
        /// A session tab and its terminals.
        case sessionTab
        /// A window and its session tabs.
        case window
        /// The app.
        case quit
    }

    /// Calls `close` now if no terminal in `busy` is running a command, or
    /// once the user confirms in a sheet on `window` (or a dialog without one).
    static func confirm(_ busy: [TerminalTab], scope: Scope, in window: NSWindow?,
                        close: @escaping @MainActor () -> Void) {
        guard !busy.isEmpty else { close(); return }
        let alert = makeAlert(busy, scope: scope)
        if let window, window.attachedSheet == nil {
            alert.beginSheetModal(for: window) { response in
                if response == .alertFirstButtonReturn { close() }
            }
        } else if alert.runModal() == .alertFirstButtonReturn {
            close()
        }
    }

    static func makeAlert(_ busy: [TerminalTab], scope: Scope) -> NSAlert {
        let alert = NSAlert()
        let text = message(busy, scope: scope)
        alert.messageText = text.title
        alert.informativeText = text.detail
        alert.addButton(withTitle: scope == .quit ? "Quit" : "Close")
        alert.addButton(withTitle: "Cancel")
        return alert
    }

    /// The question and what closing ends.
    static func message(_ busy: [TerminalTab], scope: Scope) -> (title: String, detail: String) {
        let title = switch scope {
        case .tab: "Close this terminal?"
        case .sessionTab: "Close this session tab?"
        case .window: "Close this window?"
        case .quit: "Quit Kit?"
        }
        let ending = switch scope {
        case .tab: "Closing the tab ends it."
        case .sessionTab: busy.count == 1 ? "Closing the session tab ends it." : "Closing the session tab ends them."
        case .window: busy.count == 1 ? "Closing the window ends it." : "Closing the window ends them."
        case .quit: busy.count == 1 ? "Quitting ends it." : "Quitting ends them."
        }
        let running: String
        if busy.count == 1 {
            let command = busy[0].shellTitle
            running = (command.isEmpty ? "A command" : command) + " is still running" + (scope == .tab ? "." : " in a terminal.")
        } else {
            running = "\(busy.count) commands are still running in terminals."
        }
        return (title, running + " " + ending)
    }
}
