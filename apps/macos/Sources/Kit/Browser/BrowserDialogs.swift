import AppKit

/// Standard macOS sheets for what a page asks of the person using it: page
/// alerts, confirms and prompts, logins, and file choices. Each attaches to
/// the window showing the page, or runs as a dialog when the page isn't in one.
///
/// A page that isn't on screen, as in a session tab that isn't shown, gets
/// no dialog: an alert is dismissed, a confirm or login is cancelled, and a
/// prompt returns nothing.
@MainActor enum BrowserDialogs {
    /// Asks before a clicked link opens in another app.
    static func confirmOpen(in app: String, from host: String, in view: NSView) async -> Bool {
        let alert = NSAlert()
        alert.messageText = "Open this link in \(app)?"
        alert.informativeText = host.isEmpty ? "A page wants to open \(app)." : "\(host) wants to open \(app)."
        alert.addButton(withTitle: "Open")
        alert.addButton(withTitle: "Cancel")
        return await present(alert, in: view) == .alertFirstButtonReturn
    }

    /// Each page dialog offers to stop further dialogs from the page;
    /// `suppress` runs when that is chosen.
    static func alert(_ message: String, from host: String, in view: NSView, suppress: () -> Void) async {
        let alert = pageAlert(message, from: host)
        alert.addButton(withTitle: "OK")
        _ = await present(alert, in: view)
        if alert.suppressionButton?.state == .on { suppress() }
    }

    static func confirm(_ message: String, from host: String, in view: NSView, suppress: () -> Void) async -> Bool {
        let alert = pageAlert(message, from: host)
        alert.addButton(withTitle: "OK")
        alert.addButton(withTitle: "Cancel")
        let response = await present(alert, in: view)
        if alert.suppressionButton?.state == .on { suppress() }
        return response == .alertFirstButtonReturn
    }

    static func prompt(_ message: String, defaultText: String?, from host: String, in view: NSView,
                       suppress: () -> Void) async -> String? {
        let alert = pageAlert(message, from: host)
        alert.addButton(withTitle: "OK")
        alert.addButton(withTitle: "Cancel")
        let field = NSTextField(string: defaultText ?? "")
        field.frame = NSRect(x: 0, y: 0, width: 260, height: 22)
        alert.accessoryView = field
        alert.window.initialFirstResponder = field
        let response = await present(alert, in: view)
        if alert.suppressionButton?.state == .on { suppress() }
        return response == .alertFirstButtonReturn ? field.stringValue : nil
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

    static func chooseFiles(multiple: Bool, directories: Bool, in view: NSView) async -> [URL]? {
        guard let window = view.window else { return nil }
        let panel = NSOpenPanel()
        panel.allowsMultipleSelection = multiple
        panel.canChooseDirectories = directories
        panel.canChooseFiles = true
        let response = await withCheckedContinuation { continuation in
            panel.beginSheetModal(for: window) { continuation.resume(returning: $0) }
        }
        return response == .OK ? panel.urls : nil
    }

    private static func pageAlert(_ message: String, from host: String) -> NSAlert {
        let alert = NSAlert()
        alert.messageText = host.isEmpty ? "This page says" : "\(host) says"
        alert.informativeText = message
        alert.showsSuppressionButton = true
        alert.suppressionButton?.title = "Don’t allow more dialogs from this page"
        return alert
    }

    private static func present(_ alert: NSAlert, in view: NSView) async -> NSApplication.ModalResponse {
        // A sheet already up means the page is asking again; decline.
        guard let window = view.window, window.attachedSheet == nil else { return .cancel }
        return await withCheckedContinuation { continuation in
            alert.beginSheetModal(for: window) { continuation.resume(returning: $0) }
        }
    }
}
