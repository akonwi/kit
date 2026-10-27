import AppKit
import ImageIO
import Observation

/// Something a browser tab asks of the workspace holding it.
enum BrowserTabRequest {
    /// The page opened a window: show `tab` beside the one that opened it.
    case openTab(BrowserTab)
    /// The page asked to close.
    case close
    case downloadFinished(file: URL, source: URL?)
    case downloadFailed(filename: String, message: String)
}

/// Takes an authorization's redirect from a browser tab's page in place of
/// loading it. `receive` returns false when the redirect is no longer wanted,
/// and the page then loads it.
struct BrowserRedirectReceiver {
    let redirect: AuthorizationRedirect
    let receive: @MainActor (URL) -> Bool
}

/// Why a browser tab shows Kit's message instead of a page.
struct BrowserFailure: Equatable {
    let title: String
    let detail: String
    /// What Try again loads; nil when there's nothing to retry.
    let retry: URL?
}

/// Who opened a browser tab: the person, or the agent through a tool or a
/// page it asked to open.
enum BrowserTabOpener: String { case user, agent }

/// Where the agent pointer is over a tab's page, in the page view's points
/// from its top-left corner. `presses` counts the presses it has played.
struct AgentPointer: Equatable {
    var location: CGPoint
    var presses = 0
    var visible = true
}

/// A failure the browser agent reports to the model.
struct BrowserAgentError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

/// One browser tab: its address and page state, and the engine drawing it.
/// The engine is created when the page is first loaded or shown, so a tab
/// restored in a background session costs nothing until it's seen.
@MainActor @Observable
final class BrowserTab: Identifiable {
    let id: UUID
    /// The address the tab shows and restores: the page's, or the one that
    /// failed to load. Nil for a new, blank tab.
    private(set) var url: URL?
    private(set) var page = BrowserPageState()
    private(set) var favicon: NSImage?
    private(set) var failure: BrowserFailure?
    /// The site that tried to download a file without a click, shown in the
    /// pane until dismissed or the next page commits. Empty when unknown.
    private(set) var blockedDownload: String?
    /// Increments when the address field should take the keyboard.
    private(set) var addressFocusRequest = 0
    /// The latest request a shown address field has answered.
    @ObservationIgnored private var answeredAddressFocusRequest = 0
    @ObservationIgnored private var engine: (any BrowserEngine)?
    @ObservationIgnored private let makeEngine: @MainActor () -> any BrowserEngine
    /// Loads waiting for the engine, as for a restored tab.
    @ObservationIgnored private var pending: URL?
    @ObservationIgnored var onRequest: (@MainActor (BrowserTabRequest) -> Void)?
    /// Receives an authorization's redirect; the tab closes once it has.
    @ObservationIgnored var redirectReceiver: BrowserRedirectReceiver?
    @ObservationIgnored var opener = BrowserTabOpener.user
    @ObservationIgnored private var closed = false
    /// The page's dialog or file chooser waiting for an answer, if any.
    private(set) var pageDialog: BrowserPageDialog?
    /// The agent pointer, while the agent is acting on the page.
    private(set) var agentPointer: AgentPointer?
    @ObservationIgnored private var pointerFade: Task<Void, Never>?
    /// How long the pointer stays after the agent's last action.
    static let pointerLinger: Duration = .seconds(2)

    init(id: UUID = UUID(), url: URL? = nil, makeEngine: @escaping @MainActor () -> any BrowserEngine) {
        self.id = id
        self.url = url
        self.pending = url
        self.makeEngine = makeEngine
    }

    /// A tab for a window a page opened, already loading in `engine`.
    init(adopting engine: any BrowserEngine, makeEngine: @escaping @MainActor () -> any BrowserEngine) {
        id = UUID()
        self.makeEngine = makeEngine
        attach(engine)
        page = engine.state
        url = engine.state.url
    }

    /// The tab's title: the page's, then its host, then "New tab".
    var title: String {
        if failure == nil, !page.title.isEmpty { return page.title }
        if let url { return BrowserAddress.hostLabel(url) }
        return "New tab"
    }

    var loading: Bool { page.loading && failure == nil }
    func dismissBlockedDownload() { blockedDownload = nil }

    /// The page's view, creating the engine. A waiting load starts after
    /// the view update that asked for it.
    var view: NSView {
        let engine = liveEngine
        if let pending {
            self.pending = nil
            Task { @MainActor [weak self] in
                guard let self, self.engine === engine, self.url == pending else { return }
                engine.load(pending)
            }
        }
        return engine.view
    }

    /// Loads what the address field holds. Text that isn't a web address
    /// shows Kit's message instead.
    func navigate(_ input: String) {
        guard let url = BrowserAddress.url(from: input) else {
            let text = input.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !text.isEmpty else { return }
            failure = BrowserFailure(title: "Can’t open ‘\(text)’", detail: "That isn’t a web address.", retry: nil)
            return
        }
        load(url)
    }

    func load(_ url: URL) {
        pending = nil
        self.url = url
        failure = nil
        favicon = nil
        liveEngine.load(url)
    }

    func goBack() { engine?.goBack() }
    func goForward() { engine?.goForward() }
    func stop() { engine?.stop() }

    func reload() {
        if let retry = failure?.retry { load(retry); return }
        guard let url else { return }
        if pending != nil { load(url) } else { liveEngine.reload() }
    }

    func requestAddressFocus() { addressFocusRequest += 1 }

    /// Whether a request for the address field is still unanswered; it's
    /// answered by asking, so a field shown again doesn't repeat it.
    func takeAddressFocusRequest() -> Bool {
        guard addressFocusRequest > answeredAddressFocusRequest else { return false }
        answeredAddressFocusRequest = addressFocusRequest
        return true
    }

    // MARK: Agent

    /// The engine, created and loading any waiting address, for the agent.
    /// A restored tab that hasn't been shown starts its load now, since the
    /// agent may read it before anything shows it. A closed tab has none.
    func agentEngine() throws -> any BrowserEngine {
        guard !closed else { throw BrowserAgentError("The tab was closed. Call browser__tabs for the tabs open now.") }
        let engine = liveEngine
        // A page never shown has no size; lay it out as a typical pane until
        // a host sizes it.
        if engine.view.frame.isEmpty { engine.view.frame = NSRect(origin: .zero, size: Self.unshownPageSize) }
        if let pending {
            self.pending = nil
            engine.load(pending)
        }
        return engine
    }

    /// Whether a restored address is waiting for the page to be created.
    var loadPending: Bool { pending != nil }
    /// The size the agent lays out a page that hasn't been shown at.
    static let unshownPageSize = CGSize(width: 1280, height: 800)

    /// Whether the page's view is in a window on screen.
    var onScreen: Bool {
        guard let view = engine?.view, let window = view.window else { return false }
        return window.isVisible && !view.isHiddenOrHasHiddenAncestor
    }

    /// Shows the agent pointer moving to `location`. It appears at the
    /// page's centre the first time.
    func moveAgentPointer(to location: CGPoint, in size: CGSize) {
        guard location.x.isFinite, location.y.isFinite else { return }
        pointerFade?.cancel()
        let clamped = CGPoint(x: min(max(location.x, 0), max(size.width, 0)), y: min(max(location.y, 0), max(size.height, 0)))
        if var pointer = agentPointer, pointer.visible {
            pointer.location = clamped
            agentPointer = pointer
        } else {
            agentPointer = AgentPointer(location: clamped)
        }
    }

    /// Plays the agent pointer's press where it is.
    func pressAgentPointer() {
        guard agentPointer != nil else { return }
        pointerFade?.cancel()
        agentPointer?.presses += 1
    }

    /// Hides the agent pointer after it has lingered.
    func releaseAgentPointer() {
        pointerFade?.cancel()
        guard agentPointer != nil else { return }
        pointerFade = Task { @MainActor [weak self] in
            try? await Task.sleep(for: Self.pointerLinger)
            guard !Task.isCancelled else { return }
            self?.agentPointer?.visible = false
        }
    }

    /// Stops the page before the tab is closed.
    func close() {
        closed = true
        pointerFade?.cancel()
        engine?.close()
        engine = nil
        onRequest = nil
        redirectReceiver = nil
    }

    private var liveEngine: any BrowserEngine {
        if let engine { return engine }
        let engine = makeEngine()
        attach(engine)
        return engine
    }

    private func attach(_ engine: any BrowserEngine) {
        self.engine = engine
        engine.onEvent = { [weak self] event in self?.handle(event) }
        engine.interceptsNavigation = { [weak self] url in self?.intercept(url) ?? false }
    }

    /// Hands a page's redirect to the tab's receiver instead of loading it,
    /// then closes the tab, after the engine has stopped the navigation.
    private func intercept(_ url: URL) -> Bool {
        guard let receiver = redirectReceiver, receiver.redirect.matches(url) else { return false }
        redirectReceiver = nil
        guard receiver.receive(url) else { return false }
        Task { @MainActor [weak self] in self?.onRequest?(.close) }
        return true
    }

    /// Decodes a site's icon at tab size only, so a large or hostile image
    /// is never fully decoded.
    static func icon(from data: Data) -> NSImage? {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil),
              let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                  kCGImageSourceCreateThumbnailFromImageAlways: true,
                  kCGImageSourceCreateThumbnailWithTransform: true,
                  kCGImageSourceThumbnailMaxPixelSize: 64,
              ] as CFDictionary) else { return nil }
        return NSImage(cgImage: image, size: NSSize(width: 16, height: 16))
    }

    private func handle(_ event: BrowserEngineEvent) {
        switch event {
        case .changed(let state):
            if page != state { page = state }
            if let next = state.url, next != url, failure == nil || state.loading { url = next }
        case .committed:
            failure = nil
            blockedDownload = nil
            if let next = engine?.state.url, next.host() != url?.host() { favicon = nil }
        case .failed(let failed, let message):
            let target = failed ?? url
            url = target
            failure = BrowserFailure(title: "Can’t reach " + (target.map(BrowserAddress.hostLabel) ?? "the page"),
                                     detail: message, retry: target)
        case .favicon(let data):
            if let image = Self.icon(from: data) { favicon = image }
        case .openedWindow(let child):
            // Without a workspace to show it, the window is refused.
            guard let onRequest else { child.close(); return }
            let tab = BrowserTab(adopting: child, makeEngine: makeEngine)
            tab.opener = opener
            onRequest(.openTab(tab))
        case .closeRequested:
            onRequest?(.close)
        case .downloadFinished(let file, let source):
            onRequest?(.downloadFinished(file: file, source: source))
        case .downloadFailed(let filename, let message):
            onRequest?(.downloadFailed(filename: filename, message: message))
        case .dialogChanged(let dialog):
            pageDialog = dialog
        case .downloadBlocked(let source):
            blockedDownload = source.map(BrowserAddress.hostLabel) ?? ""
        }
    }
}
