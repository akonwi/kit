import AppKit
import UniformTypeIdentifiers

/// Whether a tab can use a feature that works on this Mac's files and
/// programs, such as a terminal, Reveal in Finder, or opening a file in
/// another app. Those need the tab's server to run on this Mac, as its
/// verified machine identifier says (`ServerConnection.isThisMac`).
enum ThisMacGate: Equatable {
    case available
    /// The tab's server runs on another machine.
    case needsThisMac
    /// The tab's server hasn't verified its machine yet.
    case unverified

    init(thisMac: Bool?) {
        switch thisMac {
        case true?: self = .available
        case false?: self = .needsThisMac
        case nil: self = .unverified
        }
    }

    /// Why the feature is unavailable, shown beside it dimmed; nil when it
    /// is available.
    var reason: String? {
        switch self {
        case .available: nil
        case .needsThisMac: "needs this Mac"
        case .unverified: "waiting for the server"
        }
    }
}

/// A workspace file's tab menu: Copy Path, which copies the path on the
/// session's server and works anywhere, then Reveal in Finder and Open in
/// Default App, which work on this Mac's files and so need the server here.
struct FileTabActions: Equatable {
    /// The file's absolute path on the session's server, or nil when the
    /// session's working directory is unknown.
    let path: String?
    let gate: ThisMacGate

    /// `file` is a workspace path relative to the session's working
    /// directory `cwd`.
    init(file: String, cwd: String?, gate: ThisMacGate) {
        path = Self.absolute(file, cwd: cwd)
        self.gate = gate
    }

    static func absolute(_ file: String, cwd: String?) -> String? {
        if file.hasPrefix("/") { return file }
        guard let cwd, cwd.hasPrefix("/") else { return nil }
        return URL(fileURLWithPath: cwd).appendingPathComponent(file).standardizedFileURL.path
    }

    /// Why Reveal in Finder and Open in Default App are unavailable.
    var localReason: String? { path == nil ? "Session unavailable" : gate.reason }

    @MainActor func copyPath(to pasteboard: NSPasteboard = .general) {
        guard let path else { return }
        pasteboard.clearContents()
        pasteboard.setString(path, forType: .string)
    }

    @MainActor func revealInFinder() {
        guard localReason == nil, let path else { return }
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
    }

    /// Opens the file in its default app. A program runs when opened, and
    /// the agent may have written it, so that asks first.
    @MainActor func openInDefaultApp(confirm: @MainActor (URL) -> Bool = Self.confirmRunning) {
        guard localReason == nil, let path else { return }
        let url = URL(fileURLWithPath: path)
        guard !Self.runsWhenOpened(url) || confirm(url) else { return }
        NSWorkspace.shared.open(url)
    }

    /// Whether opening `url` may run something: an app, an executable file, a
    /// script Terminal runs, such as a `.command` file, or an alias or file
    /// location that points elsewhere. Symbolic links are judged by their
    /// target, because opening one opens the target.
    static func runsWhenOpened(_ url: URL) -> Bool {
        let url = url.resolvingSymlinksInPath()
        let values = try? url.resourceValues(forKeys: [.isApplicationKey, .isRegularFileKey, .isExecutableKey, .contentTypeKey, .isAliasFileKey])
        if values?.isApplication == true || values?.isAliasFile == true { return true }
        if values?.isRegularFile == true, values?.isExecutable == true { return true }
        let type = values?.contentType ?? UTType(filenameExtension: url.pathExtension)
        if let type, type.conforms(to: .executable) || type.conforms(to: .application) { return true }
        return ["command", "tool", "terminal", "fileloc"].contains(url.pathExtension.lowercased())
    }

    @MainActor static func confirmRunning(_ url: URL) -> Bool {
        let alert = NSAlert()
        alert.messageText = "Open “\(url.lastPathComponent)”?"
        alert.informativeText = "It’s a program, so opening it runs it on this Mac."
        alert.addButton(withTitle: "Open")
        alert.addButton(withTitle: "Cancel")
        return alert.runModal() == .alertFirstButtonReturn
    }
}
