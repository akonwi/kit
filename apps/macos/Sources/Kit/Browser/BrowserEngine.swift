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
    var state: BrowserPageState { get }
    func load(_ url: URL)
    func goBack()
    func goForward()
    func reload()
    func stop()
    /// Stops all work before the engine is released.
    func close()
}
