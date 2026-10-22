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
    @ObservationIgnored private var engine: (any BrowserEngine)?
    @ObservationIgnored private let makeEngine: @MainActor () -> any BrowserEngine
    /// Loads waiting for the engine, as for a restored tab.
    @ObservationIgnored private var pending: URL?
    @ObservationIgnored var onRequest: (@MainActor (BrowserTabRequest) -> Void)?
    /// Receives an authorization's redirect; the tab closes once it has.
    @ObservationIgnored var redirectReceiver: BrowserRedirectReceiver?

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

    /// Stops the page before the tab is closed.
    func close() {
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
            onRequest(.openTab(BrowserTab(adopting: child, makeEngine: makeEngine)))
        case .closeRequested:
            onRequest?(.close)
        case .downloadFinished(let file, let source):
            onRequest?(.downloadFinished(file: file, source: source))
        case .downloadFailed(let filename, let message):
            onRequest?(.downloadFailed(filename: filename, message: message))
        case .downloadBlocked(let source):
            blockedDownload = source.map(BrowserAddress.hostLabel) ?? ""
        }
    }
}
