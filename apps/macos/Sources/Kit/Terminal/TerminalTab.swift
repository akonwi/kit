import AppKit
import Observation

/// Something a terminal tab asks of the workspace holding it.
enum TerminalTabRequest {
    /// The shell exited.
    case close
    case bell
    case notification(title: String, body: String)
    /// A link the user ⌘-clicked.
    case openURL(URL)
}

/// One terminal tab: its shell's title and working directory, and the view
/// running it. The shell starts when the tab is first shown, so a terminal
/// restored in a background session costs nothing until it's seen.
@MainActor @Observable
final class TerminalTab: Identifiable {
    let id: UUID
    /// The shell's working directory, as shell integration last reported
    /// it. Relaunch starts the new shell here.
    private(set) var directory: String
    /// The title the shell set: the directory at a prompt, or the command
    /// that's running.
    private(set) var shellTitle = ""
    /// Whether a bell rang, or the shell posted a notification, while the
    /// tab wasn't on screen.
    var bell = false
    /// Why the terminal couldn't start.
    private(set) var failure: String?
    @ObservationIgnored private var surface: TerminalSurfaceView?
    @ObservationIgnored var onRequest: (@MainActor (TerminalTabRequest) -> Void)?
    /// Typed into the shell when it starts, such as a command to run. It is
    /// not restored with the tab.
    @ObservationIgnored private let initialInput: String?

    init(id: UUID = UUID(), directory: String, initialInput: String? = nil) {
        self.id = id
        self.directory = directory
        self.initialInput = initialInput
    }

    /// The tab's name: the shell's title, or "Terminal" until it sets one.
    var title: String { shellTitle.isEmpty ? "Terminal" : shellTitle }

    /// The terminal's view, starting libghostty and the shell on first use.
    /// Nil once it has failed to start.
    func view(appearance: TerminalAppearance) -> NSView? {
        if let surface { return surface }
        guard failure == nil else { return nil }
        let runtime = TerminalRuntime.shared
        guard runtime.start(appearance) else {
            failure = runtime.failure ?? "The terminal couldn’t start."
            return nil
        }
        guard let view = TerminalSurfaceView(directory: Self.startingDirectory(directory), initialInput: initialInput) else {
            failure = "The shell couldn’t start."
            return nil
        }
        view.owner = self
        surface = view
        return view
    }

    /// Whether closing the tab would end a command that is still running.
    var needsConfirmClose: Bool { surface?.needsConfirmClose ?? false }
    /// The window showing the terminal, if it's on screen.
    var window: NSWindow? { surface?.window }

    /// Ends the shell.
    func close() {
        surface?.close()
        surface = nil
        onRequest = nil
    }

    /// `path` if it is a directory on this Mac, else the home directory.
    static func startingDirectory(_ path: String) -> String {
        var isDirectory: ObjCBool = false
        if !path.isEmpty, FileManager.default.fileExists(atPath: path, isDirectory: &isDirectory), isDirectory.boolValue {
            return path
        }
        return NSHomeDirectory()
    }
}

extension TerminalTab: TerminalSurfaceOwner {
    func terminal(_ view: TerminalSurfaceView, received event: TerminalEvent) {
        switch event {
        case .title(let title):
            let trimmed = title.trimmingCharacters(in: .whitespacesAndNewlines)
            if trimmed != shellTitle { shellTitle = trimmed }
        case .workingDirectory(let path):
            // OSC 7 may report a file URL; keep only paths on this Mac.
            let local = URL(string: path).flatMap { $0.isFileURL ? $0.path : nil } ?? path
            if local.hasPrefix("/"), local != directory { directory = local }
        case .bell:
            onRequest?(.bell)
        case .notification(let title, let body):
            onRequest?(.notification(title: title, body: body))
        case .openURL(let text):
            guard let url = URL(string: text.trimmingCharacters(in: .whitespacesAndNewlines)), url.scheme != nil
            else { return }
            onRequest?(.openURL(url))
        case .closed:
            onRequest?(.close)
        case .mouseShape, .mouseVisible:
            break
        }
    }

    func terminal(_ view: TerminalSurfaceView, receivedDrop text: String) {
        view.sendText(text)
    }
}
