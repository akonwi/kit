import AppKit
import WebKit

/// The browser engine, drawn by WebKit. This file is the only one in Kit that
/// uses WebKit. Cookies and site data persist across launches in Kit's own
/// store, separate from Safari's.
@MainActor final class WebKitBrowserEngine: NSObject, BrowserEngine {
    private let webView: KitWebView
    private var observations: [NSKeyValueObservation] = []
    private var faviconTask: Task<Void, Never>?
    private var downloadSources: [ObjectIdentifier: URL] = [:]
    private var downloadFiles: [ObjectIdentifier: URL] = [:]
    /// Whether the page's process has been restarted since the last commit.
    private var restartedSinceCommit = false
    /// The person chose to stop this page's alerts, confirms, and prompts.
    private var dialogsSuppressed = false
    /// Whether the main frame's navigation in progress came from the person,
    /// so a download it turns into may start. Redirects keep it; a committed
    /// page or a failure ends it.
    private var userNavigation = false
    /// Kit's own controls started the next navigation.
    private var explicitNavigation = false
    private(set) var state = BrowserPageState()
    var onEvent: (@MainActor (BrowserEngineEvent) -> Void)?
    var interceptsNavigation: (@MainActor (URL) -> Bool)?
    var view: NSView { webView }

    /// Safari's user agent suffix, with the installed Safari's version: the
    /// page is drawn by the same system WebKit. "Safari/605.1.15" is frozen
    /// in every Safari's user agent.
    static let userAgentSuffix = "Version/\(installedSafariVersion() ?? "18.0") Safari/605.1.15"

    /// The installed Safari's major and minor version, such as "26.0".
    nonisolated static func installedSafariVersion(
        bundle: URL = URL(filePath: "/Applications/Safari.app", directoryHint: .isDirectory)) -> String? {
        guard let version = Bundle(url: bundle)?.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
        else { return nil }
        let parts = version.split(separator: ".").prefix(2)
        guard let major = parts.first, major.allSatisfy(\.isNumber) else { return nil }
        let minor = parts.count > 1 && parts.last!.allSatisfy(\.isNumber) ? parts.last! : "0"
        return "\(major).\(minor)"
    }

    /// Kit's engine with its persistent store.
    static func make() -> any BrowserEngine { WebKitBrowserEngine(configuration: nil) }

    /// An engine for a new page, or for a window a page opened when
    /// `configuration` is WebKit's.
    private init(configuration: WKWebViewConfiguration?) {
        let configuration = configuration ?? {
            let configuration = WKWebViewConfiguration()
            configuration.websiteDataStore = .default()
            configuration.preferences.isElementFullscreenEnabled = true
            // Identify as Safari so sites don't serve a reduced page.
            configuration.applicationNameForUserAgent = Self.userAgentSuffix
            return configuration
        }()
        webView = KitWebView(frame: .zero, configuration: configuration)
        super.init()
        webView.navigationDelegate = self
        webView.uiDelegate = self
        webView.allowsBackForwardNavigationGestures = true
        webView.isInspectable = true
        observe()
    }

    // Navigations from Kit's own controls are the person's.
    func load(_ url: URL) { explicitNavigation = true; webView.load(URLRequest(url: url)) }
    func goBack() { explicitNavigation = true; webView.goBack() }
    func goForward() { explicitNavigation = true; webView.goForward() }
    func reload() {
        explicitNavigation = true
        // After a failed first load there is no page to reload.
        if webView.url == nil, let url = state.url { load(url) } else { webView.reload() }
    }
    func stop() { webView.stopLoading() }

    func close() {
        faviconTask?.cancel()
        observations.removeAll()
        webView.stopLoading()
        webView.navigationDelegate = nil
        webView.uiDelegate = nil
        webView.removeFromSuperview()
        onEvent = nil
        interceptsNavigation = nil
    }

    private func observe() {
        let update: @MainActor (WebKitBrowserEngine) -> Void = { $0.publish() }
        func watch<Value>(_ path: KeyPath<KitWebView, Value>) -> NSKeyValueObservation {
            webView.observe(path, options: []) { [weak self] _, _ in
                MainActor.assumeIsolated { if let self { update(self) } }
            }
        }
        observations = [watch(\.url), watch(\.title), watch(\.isLoading), watch(\.estimatedProgress),
                        watch(\.canGoBack), watch(\.canGoForward)]
    }

    private func publish() {
        var next = state
        // A failed load clears the view's URL; the tab keeps the address.
        if let url = webView.url { next.url = url }
        next.title = webView.title ?? ""
        next.loading = webView.isLoading
        next.progress = webView.estimatedProgress
        next.canGoBack = webView.canGoBack
        next.canGoForward = webView.canGoForward
        guard next != state else { return }
        state = next
        onEvent?(.changed(next))
    }

    private func emit(_ event: BrowserEngineEvent) { onEvent?(event) }

    // MARK: Favicons

    private static let faviconScript = """
        (() => {
          const links = [...document.querySelectorAll('link[rel]')].filter(link => {
            const rel = link.rel.toLowerCase().split(/\\s+/);
            return link.href && (rel.includes('icon') || rel.includes('apple-touch-icon'));
          });
          const icon = links.find(link => link.rel.toLowerCase().split(/\\s+/).includes('icon'));
          return (icon || links[0] || {}).href || '';
        })()
        """

    /// Icons are fetched outside WebKit, so only web addresses, with a size
    /// and time limit, no cookies, and only the page's origin as referrer.
    private nonisolated static let faviconSession: URLSession = {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 10
        configuration.timeoutIntervalForResource = 10
        configuration.httpCookieStorage = nil
        configuration.urlCache = nil
        return URLSession(configuration: configuration)
    }()
    private nonisolated static let faviconLimit = 256 * 1024

    private func loadFavicon() {
        faviconTask?.cancel()
        guard let page = webView.url, BrowserAddress.isWeb(page) else { return }
        faviconTask = Task { [weak self] in
            let declared = try? await self?.webView.evaluateJavaScript(Self.faviconScript) as? String
            let url = declared.flatMap { $0.isEmpty ? nil : URL(string: $0) } ?? URL(string: "/favicon.ico", relativeTo: page)
            guard let url = url?.absoluteURL, BrowserAddress.isWeb(url), !Task.isCancelled,
                  let data = await Self.fetchIcon(url, referrer: page), !Task.isCancelled,
                  let self, self.webView.url?.host() == page.host() else { return }
            self.emit(.favicon(data))
        }
    }

    private nonisolated static func fetchIcon(_ url: URL, referrer page: URL) async -> Data? {
        var request = URLRequest(url: url)
        var origin = URLComponents()
        origin.scheme = page.scheme
        origin.host = page.host()
        origin.port = page.port
        if let referrer = origin.string { request.setValue(referrer + "/", forHTTPHeaderField: "Referer") }
        guard let (bytes, response) = try? await faviconSession.bytes(for: request),
              (response as? HTTPURLResponse).map({ $0.statusCode < 400 }) ?? false,
              response.expectedContentLength <= Int64(faviconLimit) else { return nil }
        var data = Data()
        do {
            for try await byte in bytes {
                data.append(byte)
                if data.count > faviconLimit { return nil }
            }
        } catch { return nil }
        return data
    }
}

// MARK: Navigation

extension WebKitBrowserEngine: WKNavigationDelegate {
    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
        let mainFrame = action.targetFrame?.isMainFrame != false
        // Real input, not the navigation's type: a script can click a link.
        let clicked = self.webView.recentlyInteracted || (mainFrame && explicitNavigation)
        if mainFrame {
            explicitNavigation = false
            if clicked { userNavigation = true }
            // WebKit asks again for each redirect, so a redirect is seen
            // before its address is requested.
            if let url = action.request.url, interceptsNavigation?(url) == true { return .cancel }
        }
        if action.shouldPerformDownload {
            guard clicked else { blockDownload(action.request.url); return .cancel }
            return .download
        }
        guard let url = action.request.url, let scheme = url.scheme?.lowercased() else { return .allow }
        guard !["http", "https", "about", "blob", "data", "javascript"].contains(scheme) else { return .allow }
        // Mail, phone, and app links open in the apps that handle them, but
        // only when clicked in a shown page, and after asking.
        guard self.webView.recentlyInteracted, action.targetFrame?.isMainFrame != false,
              webView.window != nil, let app = NSWorkspace.shared.urlForApplication(toOpen: url) else { return .cancel }
        let name = FileManager.default.displayName(atPath: app.path).replacingOccurrences(of: ".app", with: "")
        if await BrowserDialogs.confirmOpen(in: name, from: webView.url.map(BrowserAddress.hostLabel) ?? "", in: webView) {
            NSWorkspace.shared.open(url)
        }
        return .cancel
    }

    func webView(_ webView: WKWebView, decidePolicyFor response: WKNavigationResponse) async -> WKNavigationResponsePolicy {
        let disposition = (response.response as? HTTPURLResponse)?.value(forHTTPHeaderField: "Content-Disposition") ?? ""
        guard !response.canShowMIMEType || disposition.lowercased().hasPrefix("attachment") else { return .allow }
        // Only a download the person asked for starts: from a navigation they
        // began, or just after they used the page. Pages can't start one alone.
        guard (response.isForMainFrame && userNavigation) || self.webView.recentlyInteracted else {
            blockDownload(response.response.url)
            return .cancel
        }
        return .download
    }

    func webView(_ webView: WKWebView, navigationAction: WKNavigationAction, didBecome download: WKDownload) {
        adopt(download, source: navigationAction.request.url)
    }

    func webView(_ webView: WKWebView, navigationResponse: WKNavigationResponse, didBecome download: WKDownload) {
        adopt(download, source: navigationResponse.response.url)
    }

    func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
        userNavigation = false
        restartedSinceCommit = false
        dialogsSuppressed = false
        emit(.committed)
        publish()
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        publish()
        loadFavicon()
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: any Error) {
        fail(error)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: any Error) {
        fail(error)
    }

    /// A page whose process ends is reloaded once; if it ends again before
    /// another page commits, the tab shows a failure with Try again.
    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        guard restartedSinceCommit else {
            restartedSinceCommit = true
            webView.reload()
            return
        }
        emit(.failed(url: webView.url ?? state.url, message: "The page stopped working."))
    }

    func webView(_ webView: WKWebView, respondTo challenge: URLAuthenticationChallenge)
        async -> (URLSession.AuthChallengeDisposition, URLCredential?) {
        let space = challenge.protectionSpace
        guard [NSURLAuthenticationMethodHTTPBasic, NSURLAuthenticationMethodHTTPDigest,
               NSURLAuthenticationMethodNTLM].contains(space.authenticationMethod) else {
            return (.performDefaultHandling, nil)
        }
        let host = space.port == 0 || space.port == 80 || space.port == 443 ? space.host : "\(space.host):\(space.port)"
        guard let login = await BrowserDialogs.credentials(host: host, realm: space.realm,
            secure: space.receivesCredentialSecurely, failed: challenge.previousFailureCount > 0, in: webView) else {
            return (.cancelAuthenticationChallenge, nil)
        }
        return (.useCredential, URLCredential(user: login.user, password: login.password, persistence: .forSession))
    }

    /// Reports a failure, unless the load was cancelled or became a download.
    private func fail(_ error: any Error) {
        let error = error as NSError
        if error.domain == NSURLErrorDomain && error.code == NSURLErrorCancelled { return }
        // WebKit's "frame load interrupted": a navigation turned into a download
        // or was stopped by the tab.
        if error.domain == "WebKitErrorDomain" && error.code == 102 { return }
        userNavigation = false
        // Report the address that failed, before the view falls back to the
        // previous page's; the tab keeps its own address when there is none.
        emit(.failed(url: error.userInfo[NSURLErrorFailingURLErrorKey] as? URL, message: error.localizedDescription))
        publish()
    }
}

// MARK: Windows and page dialogs

extension WebKitBrowserEngine: WKUIDelegate {
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        let child = WebKitBrowserEngine(configuration: configuration)
        emit(.openedWindow(child))
        // A window no tab took must not be given to the page.
        return child.onEvent == nil ? nil : child.webView
    }

    func webViewDidClose(_ webView: WKWebView) { emit(.closeRequested) }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo) async {
        guard !dialogsSuppressed else { return }
        await BrowserDialogs.alert(message, from: Self.host(of: frame), in: webView) { dialogsSuppressed = true }
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo) async -> Bool {
        guard !dialogsSuppressed else { return false }
        return await BrowserDialogs.confirm(message, from: Self.host(of: frame), in: webView) { dialogsSuppressed = true }
    }

    func webView(_ webView: WKWebView, runJavaScriptTextInputPanelWithPrompt prompt: String,
                 defaultText: String?, initiatedByFrame frame: WKFrameInfo) async -> String? {
        guard !dialogsSuppressed else { return nil }
        return await BrowserDialogs.prompt(prompt, defaultText: defaultText, from: Self.host(of: frame), in: webView) {
            dialogsSuppressed = true
        }
    }

    func webView(_ webView: WKWebView, runOpenPanelWith parameters: WKOpenPanelParameters,
                 initiatedByFrame frame: WKFrameInfo) async -> [URL]? {
        await BrowserDialogs.chooseFiles(multiple: parameters.allowsMultipleSelection,
                                         directories: parameters.allowsDirectories, in: webView)
    }

    private static func host(of frame: WKFrameInfo) -> String {
        let origin = frame.securityOrigin
        guard !origin.host.isEmpty else { return "" }
        let standard = (origin.protocol == "https" && origin.port == 443) || (origin.protocol == "http" && origin.port == 80)
        return origin.port == 0 || standard ? origin.host : "\(origin.host):\(origin.port)"
    }
}

// MARK: Downloads

extension WebKitBrowserEngine: WKDownloadDelegate {
    private func blockDownload(_ source: URL?) {
        emit(.downloadBlocked(source: source ?? state.url))
    }

    private func adopt(_ download: WKDownload, source: URL?) {
        download.delegate = self
        if let source { downloadSources[ObjectIdentifier(download)] = source }
    }

    func download(_ download: WKDownload, decideDestinationUsing response: URLResponse,
                  suggestedFilename: String) async -> URL? {
        let reserved = Set(Self.activeDownloads)
        let file = BrowserDownloads.destination(for: suggestedFilename) {
            reserved.contains($0) || FileManager.default.fileExists(atPath: $0.path)
        }
        downloadFiles[ObjectIdentifier(download)] = file
        Self.activeDownloads.append(file)
        return file
    }

    func downloadDidFinish(_ download: WKDownload) {
        let key = ObjectIdentifier(download)
        let source = downloadSources.removeValue(forKey: key) ?? download.originalRequest?.url
        guard let file = downloadFiles.removeValue(forKey: key) else { return }
        Self.release(file)
        BrowserDownloads.quarantine(file, source: source)
        emit(.downloadFinished(file: file, source: source))
    }

    func download(_ download: WKDownload, didFailWithError error: any Error, resumeData: Data?) {
        let key = ObjectIdentifier(download)
        downloadSources[key] = nil
        let file = downloadFiles.removeValue(forKey: key)
        if let file { Self.release(file) }
        let name = file?.lastPathComponent ?? download.originalRequest?.url?.lastPathComponent ?? "download"
        emit(.downloadFailed(filename: name, message: error.localizedDescription))
    }
}

extension WebKitBrowserEngine {
    /// Destinations of downloads in progress in any tab, so two at once
    /// never pick the same name.
    fileprivate static var activeDownloads: [URL] = []

    fileprivate static func release(_ file: URL) {
        if let index = activeDownloads.firstIndex(of: file) { activeDownloads.remove(at: index) }
    }
}

/// The page's view, noting when the person last clicked or typed in it.
private final class KitWebView: WKWebView {
    private var lastInteraction: Date?

    /// How long after a click or key press the page may start a download,
    /// like the web's transient user activation.
    static let activationWindow: TimeInterval = 5

    var recentlyInteracted: Bool {
        lastInteraction.map { Date().timeIntervalSince($0) < Self.activationWindow } ?? false
    }

    func interacted() { lastInteraction = Date() }

    override func mouseDown(with event: NSEvent) { interacted(); super.mouseDown(with: event) }
    override func rightMouseDown(with event: NSEvent) { interacted(); super.rightMouseDown(with: event) }
    override func otherMouseDown(with event: NSEvent) { interacted(); super.otherMouseDown(with: event) }
    override func keyDown(with event: NSEvent) { interacted(); super.keyDown(with: event) }
}
