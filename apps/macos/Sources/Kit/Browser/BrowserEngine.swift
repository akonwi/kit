import AppKit

/// A page's navigation state, as a browser engine reports it.
struct BrowserPageState: Equatable {
    var url: URL?
    var title = ""
    var loading = false
    /// Load progress from 0 to 1, meaningful while loading.
    var progress = 0.0
    var canGoBack = false
    var canGoForward = false
}

/// Something a browser engine reports to the tab that owns it.
enum BrowserEngineEvent {
    /// Navigation state changed.
    case changed(BrowserPageState)
    /// A navigation committed: the page now shows a new document.
    case committed
    /// A navigation failed. Cancelled loads are not failures.
    case failed(url: URL?, message: String)
    /// The page's icon, as image data.
    case favicon(Data)
    /// The page opened a window. The new engine is already loading it and
    /// keeps its opener; its owner shows it as a new tab.
    case openedWindow(any BrowserEngine)
    /// The page asked to close its window.
    case closeRequested
    /// A page dialog or file chooser opened or ended; nil once none is open.
    case dialogChanged(BrowserPageDialog?)
    /// A download finished, saved at `file`.
    case downloadFinished(file: URL, source: URL?)
    case downloadFailed(filename: String, message: String)
    /// The page tried to start a download the person didn't ask for.
    case downloadBlocked(source: URL?)
}

/// A web page engine behind a browser tab. Kit's views and models use only
/// this interface; the engine's implementation is the one place that knows
/// which web engine draws the page. It presents its own login prompts and
/// page dialogs as sheets on the window showing it.
@MainActor protocol BrowserEngine: AnyObject {
    /// The page's view. One host shows it at a time.
    var view: NSView { get }
    var onEvent: (@MainActor (BrowserEngineEvent) -> Void)? { get set }
    /// Asked before the page goes to a new address, including by a
    /// redirect. Returning true stops the navigation there.
    var interceptsNavigation: (@MainActor (URL) -> Bool)? { get set }
    var state: BrowserPageState { get }
    func load(_ url: URL)
    func goBack()
    func goForward()
    func reload()
    func stop()
    /// Stops all work before the engine is released.
    func close()

    // MARK: Agent

    /// Runs `operation` of Kit's browser agent page script with `arguments`
    /// in a content world the page can't see, returning its JSON result.
    func runAgentScript(_ operation: String, arguments: [String: Any]) async throws -> String
    /// An image of the page's visible viewport.
    func viewportImage() async throws -> CGImage
    /// The page dialog or file chooser waiting for an answer, if any.
    var pageDialog: BrowserPageDialog? { get }
    /// Answers the open page dialog as its buttons would, with `text` for a
    /// prompt; dismissing a file chooser chooses nothing. Returns false when
    /// none is open.
    func answerPageDialog(accept: Bool, text: String?) -> Bool
    /// Chooses `files` in the open file chooser. Returns false when none is open.
    func choosePageFiles(_ files: [URL]) -> Bool
    /// Delivers native input to the page as a person's, which needs the
    /// page in a window.
    func sendInput(_ input: BrowserInput) throws
}

/// A page's alert, confirm, prompt, or file chooser, waiting for an answer.
/// It waits while the page isn't shown, and its sheet appears once it is.
struct BrowserPageDialog: Equatable {
    enum Kind: String {
        case alert, confirm, prompt, files

        /// What the dialog is called.
        var noun: String { self == .files ? "file chooser" : rawValue }
    }
    let kind: Kind
    var message = ""
    /// A prompt's default text.
    var defaultText: String?
    /// Whether a file chooser takes several files, or folders.
    var multiple = false
    var directories = false
}

/// Native input for a page, at points in its view from the top-left corner.
enum BrowserInput: Equatable {
    case move(CGPoint)
    case down(CGPoint, clicks: Int)
    case dragged(CGPoint)
    case up(CGPoint, clicks: Int)
    /// Keys pressed and released in order, typed into the focused element.
    case keys([BrowserKey])
}

/// A key press: the characters it types and its key code, with modifiers.
struct BrowserKey: Equatable {
    let characters: String
    let keyCode: UInt16
    var modifiers: NSEvent.ModifierFlags = []

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.characters == rhs.characters && lhs.keyCode == rhs.keyCode && lhs.modifiers.rawValue == rhs.modifiers.rawValue
    }

    private static let named: [String: (String, UInt16)] = [
        "enter": ("\r", 36), "return": ("\r", 36), "tab": ("\t", 48), "escape": ("\u{1b}", 53), "space": (" ", 49),
        "backspace": ("\u{7f}", 51),
        "delete": (String(UnicodeScalar(NSDeleteFunctionKey)!), 117),
        "arrowup": (String(UnicodeScalar(NSUpArrowFunctionKey)!), 126),
        "arrowdown": (String(UnicodeScalar(NSDownArrowFunctionKey)!), 125),
        "arrowleft": (String(UnicodeScalar(NSLeftArrowFunctionKey)!), 123),
        "arrowright": (String(UnicodeScalar(NSRightArrowFunctionKey)!), 124),
        "home": (String(UnicodeScalar(NSHomeFunctionKey)!), 115), "end": (String(UnicodeScalar(NSEndFunctionKey)!), 119),
        "pageup": (String(UnicodeScalar(NSPageUpFunctionKey)!), 116),
        "pagedown": (String(UnicodeScalar(NSPageDownFunctionKey)!), 121),
    ]
    /// Mac key codes of the keys that type letters, digits, and common marks.
    private static let codes: [Character: UInt16] = {
        var codes: [Character: UInt16] = [:]
        let rows: [(String, [UInt16])] = [
            ("asdfhgzxcv", [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]), ("bqweryt", [11, 12, 13, 14, 15, 16, 17]),
            ("123465", [18, 19, 20, 21, 22, 23]), ("=97-80]ou[ip", [24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35]),
            ("lj'k;\\,/nm.`", [37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 50]),
        ]
        for (characters, keys) in rows { for (character, key) in zip(characters, keys) { codes[character] = key } }
        codes[" "] = 49
        return codes
    }()

    /// A key by name, such as Enter, ArrowDown, or a, with modifiers joined
    /// by plus signs, such as Shift+Tab or Control+a.
    init?(_ name: String) {
        var parts = name.split(separator: "+", omittingEmptySubsequences: false).map(String.init)
        guard let last = parts.popLast(), !last.isEmpty else { return nil }
        var modifiers: NSEvent.ModifierFlags = []
        for part in parts {
            switch part.lowercased() {
            case "shift": modifiers.insert(.shift)
            case "control", "ctrl": modifiers.insert(.control)
            case "alt", "option": modifiers.insert(.option)
            case "meta", "command", "cmd": modifiers.insert(.command)
            default: return nil
            }
        }
        if let (characters, code) = Self.named[last.lowercased()] {
            self.init(characters: characters, keyCode: code, modifiers: modifiers)
        } else if last.count == 1, let character = last.first {
            self.init(character, modifiers: modifiers)
        } else {
            return nil
        }
    }

    init(characters: String, keyCode: UInt16, modifiers: NSEvent.ModifierFlags = []) {
        self.characters = characters
        self.keyCode = keyCode
        self.modifiers = modifiers
    }

    /// The key that types `character`.
    init(_ character: Character, modifiers: NSEvent.ModifierFlags = []) {
        if character == "\n" || character == "\r" { self.init(characters: "\r", keyCode: 36, modifiers: modifiers); return }
        if character == "\t" { self.init(characters: "\t", keyCode: 48, modifiers: modifiers); return }
        let lower = Character(character.lowercased())
        var modifiers = modifiers
        if character.isUppercase { modifiers.insert(.shift) }
        // A character without its own key, like é, types through a key with no code.
        self.init(characters: String(character), keyCode: Self.codes[lower] ?? 0xFFFF, modifiers: modifiers)
    }
}
