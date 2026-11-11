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

/// What reveals and opens this Mac's files in other programs. Tests replace
/// it, so they open nothing.
struct LocalFileOpener {
    var reveal: @MainActor (URL) -> Void = { NSWorkspace.shared.activateFileViewerSelecting([$0]) }
    var open: @MainActor (URL) -> Void = { NSWorkspace.shared.open($0) }
    var openWith: @MainActor (URL, URL) -> Void = { file, app in
        NSWorkspace.shared.open([file], withApplicationAt: app, configuration: NSWorkspace.OpenConfiguration())
    }
    /// The apps that can open a file.
    var apps: @MainActor (URL) -> [URL] = { NSWorkspace.shared.urlsForApplications(toOpen: $0) }
    /// The app that opens a file by default.
    var defaultApp: @MainActor (URL) -> URL? = { NSWorkspace.shared.urlForApplication(toOpen: $0) }
    /// An app the person chooses from all of them, as Finder's Open With ›
    /// Other… asks.
    var chooseApp: @MainActor () -> URL? = LocalFileOpener.chooseApplication
    /// Whether to open a program, which runs it.
    var confirmRunning: @MainActor (URL) -> Bool = FileTabActions.confirmRunning

    @MainActor static func chooseApplication() -> URL? {
        let panel = NSOpenPanel()
        panel.directoryURL = URL(fileURLWithPath: "/Applications")
        panel.allowedContentTypes = [.application]
        panel.prompt = "Open"
        return panel.runModal() == .OK ? panel.url : nil
    }
}

/// A workspace file's tab menu, and a file link's: Copy Path, which copies
/// the path on the session's server and works anywhere, then Reveal in
/// Finder, Open in Default App, and Open With, which work on this Mac's
/// files and so need the server here.
struct FileTabActions: Equatable {
    /// The file's absolute or home-relative path on the session's server,
    /// or nil when the session's working directory is unknown.
    let path: String?
    let gate: ThisMacGate
    var opener = LocalFileOpener()

    /// `file` is a workspace path relative to the session's working
    /// directory `cwd`, or an absolute or home-relative path.
    init(file: String, cwd: String?, gate: ThisMacGate) {
        path = Self.absolute(file, cwd: cwd)
        self.gate = gate
    }

    static func == (a: Self, b: Self) -> Bool { a.path == b.path && a.gate == b.gate }

    static func absolute(_ file: String, cwd: String?) -> String? {
        if file.hasPrefix("/") || file == "~" || file.hasPrefix("~/") { return file }
        guard let cwd, cwd.hasPrefix("/") else { return nil }
        return URL(fileURLWithPath: cwd).appendingPathComponent(file).standardizedFileURL.path
    }

    /// Why Reveal in Finder, Open in Default App, and Open With are
    /// unavailable.
    var localReason: String? { path == nil ? "Session unavailable" : gate.reason }

    /// The file on this Mac, when the server is here.
    private var local: URL? {
        guard localReason == nil, let path else { return nil }
        return URL(fileURLWithPath: (path as NSString).expandingTildeInPath)
    }

    @MainActor func copyPath(to pasteboard: NSPasteboard = .general) {
        guard let path else { return }
        pasteboard.clearContents()
        pasteboard.setString(path, forType: .string)
    }

    @MainActor func revealInFinder() {
        guard let local else { return }
        opener.reveal(local)
    }

    /// Opens the file in its default app. A program runs when opened, and
    /// the agent may have written it, so that asks first.
    @MainActor func openInDefaultApp() {
        guard let local, !Self.runsWhenOpened(local) || opener.confirmRunning(local) else { return }
        opener.open(local)
    }

    /// An app Open With offers.
    struct App: Equatable {
        let url: URL
        let name: String
        let isDefault: Bool

        /// Its menu title: Finder's, with "(default)" after the default app.
        var title: String { isDefault ? name + " (default)" : name }
    }

    /// The apps Open With offers, as Finder lists them: the default app,
    /// then the others by name. None when the file isn't on this Mac.
    @MainActor func openWithApps() -> [App] {
        guard let local else { return [] }
        let preferred = opener.defaultApp(local)?.standardizedFileURL
        var seen = Set<String>()
        let others = opener.apps(local).map(\.standardizedFileURL).filter { url in
            url != preferred && seen.insert(url.path).inserted
        }.map { App(url: $0, name: Self.name(of: $0), isDefault: false) }
            .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
        return (preferred.map { [App(url: $0, name: Self.name(of: $0), isDefault: true)] } ?? []) + others
    }

    /// Opens the file with `app`. A program the file names runs when opened,
    /// so that asks first, as Open in Default App does.
    @MainActor func open(with app: URL) {
        guard let local, !Self.runsWhenOpened(local) || opener.confirmRunning(local) else { return }
        opener.openWith(local, app)
    }

    /// Asks for an app, as Finder's Open With › Other… does, and opens the
    /// file with it.
    @MainActor func openWithChosenApp() {
        guard local != nil, let app = opener.chooseApp() else { return }
        open(with: app)
    }

    private static func name(of app: URL) -> String {
        let name = (try? app.resourceValues(forKeys: [.localizedNameKey]).localizedName) ?? app.lastPathComponent
        return name.hasSuffix(".app") ? String(name.dropLast(4)) : name
    }

    /// The Open With submenu's items, for an AppKit menu: the apps, then
    /// Other…. Each item keeps what carries it out.
    @MainActor func openWithMenu() -> NSMenu {
        let menu = OwningMenu(title: "Open With")
        let target = FileMenuTarget(actions: self)
        // A menu doesn't keep its items' targets.
        menu.owner = target
        let apps = openWithApps()
        for (index, app) in apps.enumerated() {
            let item = NSMenuItem(title: app.title, action: #selector(FileMenuTarget.openWith(_:)), keyEquivalent: "")
            item.target = target
            item.representedObject = app.url
            let icon = NSWorkspace.shared.icon(forFile: app.url.path)
            icon.size = NSSize(width: 16, height: 16)
            item.image = icon
            menu.addItem(item)
            if index == 0, app.isDefault, apps.count > 1 { menu.addItem(.separator()) }
        }
        if !apps.isEmpty { menu.addItem(.separator()) }
        let other = NSMenuItem(title: "Other…", action: #selector(FileMenuTarget.openWithOther), keyEquivalent: "")
        other.target = target
        menu.addItem(other)
        return menu
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

/// Carries out an AppKit Open With submenu's items.
@MainActor final class FileMenuTarget: NSObject {
    let actions: FileTabActions

    init(actions: FileTabActions) { self.actions = actions }

    @objc func openWith(_ item: NSMenuItem) {
        guard let app = item.representedObject as? URL else { return }
        actions.open(with: app)
    }
    @objc func openWithOther() { actions.openWithChosenApp() }
}

/// A menu that keeps the object its items act through.
final class OwningMenu: NSMenu {
    var owner: AnyObject?
}
