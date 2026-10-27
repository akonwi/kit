import AppKit

/// Standard macOS sheets for what a page asks of the person using it: page
/// alerts, confirms and prompts, file choices, logins, and links to other
/// apps. Each attaches to the window showing the page.
///
/// A page's dialogs and file choosers wait for the page to be shown; logins
/// and links to other apps on a page that isn't on screen are cancelled.
@MainActor enum BrowserDialogs {
    /// Shows a page's waiting dialog or file chooser as a sheet on `window`.
    /// `completion` receives the person's answer: whether they accepted,
    /// a prompt's text, the files chosen, and whether they stopped further
    /// dialogs from the page. A sheet taken down for another answer, or
    /// withdrawn, doesn't complete.
    static func present(_ dialog: BrowserPageDialog, from host: String, in window: NSWindow,
                        completion: @escaping @MainActor (PageDialogAnswer) -> Void) -> PageDialogSheet {
        let sheet = PageDialogSheet()
        if dialog.kind == .files {
            let panel = NSOpenPanel()
            panel.allowsMultipleSelection = dialog.multiple
            panel.canChooseDirectories = dialog.directories
            panel.canChooseFiles = true
            panel.beginSheetModal(for: window) { [weak sheet] response in
                guard let sheet, !sheet.ended else { return }
                sheet.ended = true
                completion(PageDialogAnswer(accept: response == .OK, files: response == .OK ? panel.urls : nil))
            }
            sheet.end = { [weak window, weak panel] in
                guard let window, let panel, window.attachedSheet === panel else { return }
                window.endSheet(panel, returnCode: .cancel)
            }
            return sheet
        }
        let alert = NSAlert()
        alert.messageText = host.isEmpty ? "This page says" : "\(host) says"
        alert.informativeText = dialog.message
        alert.showsSuppressionButton = true
        alert.suppressionButton?.title = "Don’t allow more dialogs from this page"
        alert.addButton(withTitle: "OK")
        var field: NSTextField?
        if dialog.kind != .alert { alert.addButton(withTitle: "Cancel") }
        if dialog.kind == .prompt {
            let input = NSTextField(string: dialog.defaultText ?? "")
            input.frame = NSRect(x: 0, y: 0, width: 260, height: 22)
            alert.accessoryView = input
            alert.window.initialFirstResponder = input
            field = input
        }
        alert.beginSheetModal(for: window) { [weak sheet] response in
            guard let sheet, !sheet.ended else { return }
            sheet.ended = true
            completion(PageDialogAnswer(accept: dialog.kind == .alert || response == .alertFirstButtonReturn,
                                        text: field?.stringValue, suppress: alert.suppressionButton?.state == .on))
        }
        sheet.end = { [weak window, weak alert] in
            guard let window, let alert, window.attachedSheet === alert.window else { return }
            window.endSheet(alert.window, returnCode: .cancel)
        }
        return sheet
    }

    /// Asks before a clicked link opens in another app.
    static func confirmOpen(in app: String, from host: String, in view: NSView) async -> Bool {
        let alert = NSAlert()
        alert.messageText = "Open this link in \(app)?"
        alert.informativeText = host.isEmpty ? "A page wants to open \(app)." : "\(host) wants to open \(app)."
        alert.addButton(withTitle: "Open")
        alert.addButton(withTitle: "Cancel")
        return await present(alert, in: view) == .alertFirstButtonReturn
    }

    /// Asks for a user name and password for `host`. `failed` says the last
    /// attempt was refused.
    static func credentials(host: String, realm: String?, secure: Bool, failed: Bool,
                            in view: NSView) async -> (user: String, password: String)? {
        let alert = NSAlert()
        alert.messageText = "Log in to \(host)"
        var detail = [String]()
        if failed { detail.append("The user name or password was not correct.") }
        if let realm, !realm.isEmpty { detail.append("The site says: “\(realm)”") }
        if !secure { detail.append("Your password will be sent unencrypted.") }
        alert.informativeText = detail.joined(separator: "\n")
        alert.addButton(withTitle: "Log In")
        alert.addButton(withTitle: "Cancel")
        let user = NSTextField(string: "")
        user.placeholderString = "User name"
        let password = NSSecureTextField(string: "")
        password.placeholderString = "Password"
        let stack = NSStackView(views: [user, password])
        stack.orientation = .vertical
        stack.spacing = 8
        stack.frame = NSRect(x: 0, y: 0, width: 260, height: 52)
        for field in [user, password] { field.widthAnchor.constraint(equalToConstant: 260).isActive = true }
        alert.accessoryView = stack
        alert.window.initialFirstResponder = user
        guard await present(alert, in: view) == .alertFirstButtonReturn else { return nil }
        return (user.stringValue, password.stringValue)
    }

    private static func present(_ alert: NSAlert, in view: NSView) async -> NSApplication.ModalResponse {
        // A sheet already up means the page is asking again; decline.
        guard let window = view.window, window.attachedSheet == nil else { return .cancel }
        return await withCheckedContinuation { continuation in
            alert.beginSheetModal(for: window) { continuation.resume(returning: $0) }
        }
    }
}

/// A person's answer to a page dialog or file chooser.
struct PageDialogAnswer {
    var accept: Bool
    var text: String?
    var files: [URL]?
    var suppress = false
}

/// A page dialog's sheet while it shows.
@MainActor final class PageDialogSheet {
    /// Whether the sheet has completed or been taken down.
    fileprivate(set) var ended = false
    fileprivate var end: () -> Void = {}

    /// Takes the sheet down without completing it, as when another answer
    /// came first or the page left the window.
    func withdraw() {
        guard !ended else { return }
        ended = true
        end()
    }
}
