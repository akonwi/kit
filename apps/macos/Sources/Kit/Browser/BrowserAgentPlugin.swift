import AppKit
import Foundation
import ImageIO
import UniformTypeIdentifiers

/// The macOS app's `browser` client-hosted plugin (ADR 0051): tools that let
/// the agent read and act on the browser tabs beside its conversation in
/// one window. Every call is validated here as well as by the server.
@MainActor final class BrowserAgentPlugin {
    static let pluginID = "browser"

    /// The window's workspace, read when each call runs.
    private let workspace: @MainActor () -> WorkspaceState?
    /// The session's working directory, the only place files chosen for a
    /// page may come from.
    private let workingDirectory: @MainActor () -> String?
    /// The last queued operation on each tab, so a tab's operations run in
    /// the order they arrive.
    private var tails: [UUID: Task<Void, Never>] = [:]
    /// How long the pointer takes to glide to an element before it presses.
    var glide: Duration = .milliseconds(400)
    /// How long an action waits for the page to settle.
    var settleLimit: Duration = .seconds(10)
    /// How long a page script may take before the page is reported as not
    /// responding, as when something else blocks it.
    var scriptLimit: Duration = .seconds(15)
    /// Makes each result's fence; tests fix its nonce.
    var makeFence: () -> UntrustedPageContent = { UntrustedPageContent() }

    /// How long a tab brought to the front may take to enter its window.
    var revealLimit: Duration = .milliseconds(300)

    init(workspace: @escaping @MainActor () -> WorkspaceState?,
         workingDirectory: @escaping @MainActor () -> String? = { nil }) {
        self.workspace = workspace
        self.workingDirectory = workingDirectory
    }

    /// Runs one call and returns its answer.
    func perform(_ call: ClientPluginCallRequest) async -> WireCompleteClientPluginCallInput {
        do {
            guard call.pluginID == Self.pluginID else { throw BrowserAgentError("This instance hosts only the browser plugin.") }
            let content = try await run(call.toolID, input: try ToolInput(call.input))
            return WireCompleteClientPluginCallInput(content: content, details: nil, error: nil)
        } catch is CancellationError {
            return WireCompleteClientPluginCallInput(content: nil, details: nil, error: "The call was cancelled.")
        } catch {
            let message = (error as? BrowserAgentError)?.message ?? error.localizedDescription
            return WireCompleteClientPluginCallInput(content: nil, details: nil, error: String(message.prefix(4000)))
        }
    }

    private func run(_ tool: String, input: ToolInput) async throws -> [WireClientPluginContent] {
        switch tool {
        case "tabs":
            try input.allow([])
            return [.text(listTabs())]
        case "snapshot":
            try input.allow(["tab", "mode"])
            let tab = try self.tab(input.string("tab"))
            let mode = try input.optionalString("mode", in: ["interactive", "full"]) ?? "interactive"
            return try await serialized(tab) { try await self.snapshot(tab, full: mode == "full") }
        case "act":
            try input.allow(["tab", "action", "ref", "text", "key", "direction", "x", "y", "to_ref", "to_x", "to_y", "files"])
            let tab = try self.tab(input.string("tab"))
            let action = try BrowserAction(input)
            return try await serialized(tab) { try await self.act(action, on: tab) }
        case "screenshot":
            try input.allow(["tab"])
            let tab = try self.tab(input.string("tab"))
            return try await serialized(tab) { try await self.screenshot(tab) }
        case "navigate":
            try input.allow(["tab", "url", "action"])
            let action = try input.optionalString("action", in: ["load", "back", "forward", "reload"]) ?? "load"
            let address = try input.optionalString("url")
            let url: URL?
            if action == "load" {
                guard let address else { throw BrowserAgentError("load needs a url.") }
                guard let valid = OpenURLRequest.url(address) else {
                    throw BrowserAgentError("url must be an absolute http or https address without credentials.")
                }
                url = valid
            } else {
                guard address == nil else { throw BrowserAgentError("Only load takes a url.") }
                url = nil
            }
            if let id = try input.optionalString("tab") {
                let tab = try self.tab(id)
                return try await serialized(tab) { try await self.navigate(tab, action: action, url: url) }
            }
            guard action == "load", let url else { throw BrowserAgentError("\(action) needs a tab.") }
            guard let workspace = workspace() else { throw BrowserAgentError("The window has no workspace.") }
            let tab = workspace.openRequestedPage(url, beside: .conversation)
            return try await serialized(tab) { try await self.describeAfterSettling(tab) }
        default:
            throw BrowserAgentError("Unknown browser tool \(tool).")
        }
    }

    // MARK: Tabs

    /// The tabs the agent may use: every browser tab in the window except
    /// those waiting for an authorization redirect.
    private var tabs: [BrowserTab] {
        guard let workspace = workspace() else { return [] }
        return workspace.panes.compactMap(workspace.browser).filter { $0.redirectReceiver == nil }
    }

    private func tab(_ id: String) throws -> BrowserTab {
        guard let tab = tabs.first(where: { Self.tabID($0) == id }) else {
            throw BrowserAgentError("Unknown tab \(id). Call browser__tabs for the tabs open now.")
        }
        return tab
    }

    static func tabID(_ tab: BrowserTab) -> String { tab.id.uuidString.lowercased() }

    private func listTabs() -> String {
        let tabs = self.tabs
        guard !tabs.isEmpty else { return "No browser tabs are open. browser__navigate with a url opens one." }
        let fence = makeFence()
        var lines = ["\(tabs.count) browser tab\(tabs.count == 1 ? "" : "s"):"]
        for tab in tabs {
            var facts = ["opened by \(tab.opener.rawValue)"]
            facts.append(isVisible(tab) ? "visible" : "in background")
            if tab.loading { facts.append("loading") }
            if tab.failure != nil { facts.append("failed to load") }
            if let dialog = tab.pageDialog { facts.append("a page \(dialog.kind.noun) is waiting for an answer") }
            lines.append("- tab \(Self.tabID(tab)) · origin \(Self.origin(tab.url)) · " + facts.joined(separator: " · "))
            lines.append(fence.wrap("title: \(tab.title)\naddress: \(tab.url?.absoluteString ?? "(blank)")"))
        }
        return lines.joined(separator: "\n")
    }

    private func isVisible(_ tab: BrowserTab) -> Bool {
        workspace()?.isVisible(.browser(tab.id)) == true && tab.onScreen
    }

    static func origin(_ url: URL?) -> String {
        guard let url, let scheme = url.scheme?.lowercased(), let host = url.host() else { return "none" }
        let port = url.port.map { ":\($0)" } ?? ""
        return "\(scheme)://\(host)\(port)"
    }

    // MARK: Showing

    /// Brings `tab` to the front of its tile, so a person looking at the
    /// session sees the agent act; focus stays where it is. A page that is
    /// in no window, as when its session tab isn't shown, goes to an
    /// off-screen stage for the operation, so it lays out and takes input.
    private func prepare(_ tab: BrowserTab) async throws -> BrowserAgentStage? {
        let engine = try tab.agentEngine()
        if let workspace = workspace(), !workspace.isVisible(.browser(tab.id)) {
            workspace.reveal(.browser(tab.id))
            let deadline = ContinuousClock.now + revealLimit
            while engine.view.window == nil, ContinuousClock.now < deadline {
                try await Task.sleep(for: .milliseconds(20))
            }
        }
        return BrowserAgentStage.stage(engine.view, size: engine.view.frame.size)
    }

    // MARK: Ordering

    /// Runs `work` after every operation already queued on `tab`.
    private func serialized<Result: Sendable>(_ tab: BrowserTab,
                                    _ work: @escaping @MainActor () async throws -> Result) async throws -> Result {
        let previous = tails[tab.id]
        let task = Task { @MainActor [weak self] () async throws -> Result in
            await previous?.value
            try Task.checkCancellation()
            // The tab may have closed while the call waited.
            guard let self, self.tabs.contains(where: { $0 === tab }) else {
                throw BrowserAgentError("The tab was closed. Call browser__tabs for the tabs open now.")
            }
            let stage = try await self.prepare(tab)
            defer { stage?.release() }
            return try await work()
        }
        let tail = Task { @MainActor in _ = try? await task.value }
        tails[tab.id] = tail
        defer { if tails[tab.id] == tail { tails[tab.id] = nil } }
        return try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
    }

    /// Runs a page script, giving up when the page doesn't answer in time.
    /// WebKit can't cancel a script, so a late answer is dropped.
    ///
    /// With `dialog`, a page dialog that opens first answers for the script:
    /// WebKit replies only after the page's task ends, and a dialog blocks
    /// that task until someone answers it.
    private func script(_ engine: any BrowserEngine, _ operation: String, arguments: [String: Any],
                        dialog: String? = nil) async throws -> String {
        let limit = scriptLimit
        return try await withCheckedThrowingContinuation { continuation in
            var finished = false
            var timer: Task<Void, Never>?
            let finish: @MainActor (Swift.Result<String, any Error>) -> Void = { result in
                guard !finished else { return }
                finished = true
                timer?.cancel()
                continuation.resume(with: result)
            }
            timer = Task { @MainActor in
                let deadline = ContinuousClock.now + limit
                while ContinuousClock.now < deadline {
                    if let dialog, engine.pageDialog != nil { finish(.success(dialog)); return }
                    guard (try? await Task.sleep(for: .milliseconds(50))) != nil else { return }
                }
                finish(.failure(BrowserAgentError("The page didn’t respond. Something may be blocking it; try browser__screenshot.")))
            }
            Task { @MainActor in
                do { finish(.success(try await engine.runAgentScript(operation, arguments: arguments))) }
                catch { finish(.failure(error)) }
            }
        }
    }

    // MARK: Snapshot

    private struct PageSnapshot: Decodable {
        let title: String
        let url: String
        let outline: String
    }

    private func snapshot(_ tab: BrowserTab, full: Bool) async throws -> [WireClientPluginContent] {
        [.text(try await outline(of: tab, full: full, heading: "Snapshot"))]
    }

    /// The tab's outline, or its dialog while one blocks the page.
    private func outline(of tab: BrowserTab, full: Bool, heading: String) async throws -> String {
        let fence = makeFence()
        let engine = try await ready(tab)
        var header = ["\(heading) of tab \(Self.tabID(tab)) · origin \(Self.origin(tab.url)) · \(full ? "full" : "interactive")"]
        if let dialog = engine.pageDialog {
            header.append(Self.describe(dialog, fence: fence))
            return header.joined(separator: "\n")
        }
        if let failure = tab.failure {
            header.append(fence.wrap("The tab shows an error: \(failure.title). \(failure.detail)"))
            return header.joined(separator: "\n")
        }
        guard tab.url != nil else {
            header.append("The tab is blank.")
            return header.joined(separator: "\n")
        }
        let json = try await script(engine, "snapshot", arguments: ["mode": full ? "full" : "interactive"])
        let page = try JSONDecoder().decode(PageSnapshot.self, from: Data(json.utf8))
        let outline = page.outline.isEmpty ? "(no content)" : page.outline
        header.append(fence.wrap("title: \(page.title)\naddress: \(page.url)\n\(outline)"))
        if tab.loading { header.append("The page is still loading.") }
        return header.joined(separator: "\n")
    }

    static func describe(_ dialog: BrowserPageDialog, fence: UntrustedPageContent) -> String {
        if dialog.kind == .files {
            let what = dialog.directories ? "files or folders" : dialog.multiple ? "files" : "a file"
            return "A file chooser for \(what) is open and blocks the page until it is answered; browser__act " +
                "choose_files chooses from the session’s working directory, and dismiss chooses nothing. " +
                "The user can also answer it in the app."
        }
        let answers = dialog.kind == .alert ? "accept dismisses it" : "accept or dismiss answers it"
        var text = "A page \(dialog.kind.rawValue) is open and blocks the page until it is answered; browser__act \(answers)"
        text += dialog.kind == .prompt ? ", with text for the prompt." : "."
        text += " The user can also answer it in the app."
        var body = "message: \(dialog.message)"
        if let value = dialog.defaultText, !value.isEmpty { body += "\ndefault text: \(value)" }
        return text + "\n" + fence.wrap(body)
    }

    // MARK: Actions

    private struct Located: Decodable {
        let x: Double?
        let y: Double?
        let viewportWidth: Double?
        let viewportHeight: Double?
        let error: String?
    }

    private struct Acted: Decodable {
        let ok: Bool?
        let error: String?
    }

    private func act(_ action: BrowserAction, on tab: BrowserTab) async throws -> [WireClientPluginContent] {
        let engine = try tab.agentEngine()
        switch action.kind {
        case "accept", "dismiss", "choose_files":
            guard let dialog = engine.pageDialog else { throw BrowserAgentError("The tab has no open page dialog or file chooser.") }
            let answered: Bool
            if action.kind == "choose_files" {
                guard dialog.kind == .files else { throw BrowserAgentError("The open \(dialog.kind.noun) takes accept or dismiss.") }
                let files = try chosenFiles(action.files ?? [], for: dialog)
                answered = engine.choosePageFiles(files)
            } else {
                if dialog.kind == .files, action.kind == "accept" {
                    throw BrowserAgentError("A file chooser takes choose_files, or dismiss to choose nothing.")
                }
                answered = engine.answerPageDialog(accept: action.kind == "accept", text: action.text)
            }
            guard answered else { throw BrowserAgentError("The page dialog was already answered.") }
            try await settle(tab)
            tab.releaseAgentPointer()
            let result = try await outline(of: tab, full: true, heading: "Answered the \(dialog.kind.noun). Snapshot")
            return [.text(result)]
        default:
            break
        }
        if let dialog = engine.pageDialog {
            throw BrowserAgentError(Self.describe(dialog, fence: makeFence()) + "\nAnswer it before acting on the page.")
        }
        guard tab.url != nil, tab.failure == nil else { throw BrowserAgentError("The tab has no page to act on.") }
        defer { tab.releaseAgentPointer() }
        switch action.kind {
        case "hover", "drag", "type", "press":
            try await actNatively(action, on: tab, engine: engine)
        case "click" where action.ref == nil:
            try await actNatively(action, on: tab, engine: engine)
        default:
            var arguments: [String: Any] = ["action": action.kind]
            if let ref = action.ref { arguments["ref"] = ref }
            if let text = action.text { arguments["text"] = text }
            if let direction = action.direction { arguments["direction"] = direction }
            if let ref = action.ref {
                let at = try await locate(ref, on: tab, engine: engine)
                try await point(to: at, on: tab, engine: engine, press: ["click", "check", "uncheck"].contains(action.kind))
            }
            try await perform(arguments, on: engine)
        }
        try await settle(tab)
        let target = action.ref ?? action.x.map { _ in "the point" }
        let done = target.map { "Did \(action.kind) on \($0)." } ?? "Did \(action.kind)."
        return [.text(try await outline(of: tab, full: true, heading: done + " Snapshot"))]
    }

    /// Runs the page script's `act` with `arguments`, which may end in a
    /// dialog the page opens.
    private func perform(_ arguments: [String: Any], on engine: any BrowserEngine) async throws {
        let json = try await script(engine, "act", arguments: arguments, dialog: #"{"ok":true}"#)
        let acted = try JSONDecoder().decode(Acted.self, from: Data(json.utf8))
        if let error = acted.error { throw BrowserAgentError(error) }
    }

    /// Acts with native input, as a person's mouse and keyboard would.
    private func actNatively(_ action: BrowserAction, on tab: BrowserTab, engine: any BrowserEngine) async throws {
        switch action.kind {
        case "type", "press":
            if let ref = action.ref {
                try await point(to: try await locate(ref, on: tab, engine: engine), on: tab, engine: engine, press: false)
                try await perform(["action": "focus", "ref": ref], on: engine)
            }
            let keys: [BrowserKey]
            if action.kind == "press" {
                guard let key = action.key.flatMap(BrowserKey.init) else {
                    throw BrowserAgentError("key must be a key name like Enter, Shift+Tab, or a single character.")
                }
                keys = [key]
            } else {
                keys = (action.text ?? "").map { BrowserKey($0) }
            }
            try engine.sendInput(.keys(keys))
        case "hover":
            let at = try await target(ref: action.ref, x: action.x, y: action.y, on: tab, engine: engine)
            try await point(to: at, on: tab, engine: engine, press: false)
            try engine.sendInput(.move(at))
        case "drag":
            let from = try await target(ref: action.ref, x: action.x, y: action.y, on: tab, engine: engine)
            try await point(to: from, on: tab, engine: engine, press: false)
            try engine.sendInput(.move(from))
            try engine.sendInput(.down(from, clicks: 1))
            tab.pressAgentPointer()
            // Locating the drop scrolls it into view, which would move the
            // page under the held button; a ref's drop is located first.
            let to = try await target(ref: action.toRef, x: action.toX, y: action.toY, on: tab, engine: engine, scroll: false)
            let steps = 10
            for step in 1...steps {
                let t = Double(step) / Double(steps)
                let at = CGPoint(x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t)
                if isVisible(tab) { tab.moveAgentPointer(to: at, in: engine.view.bounds.size) }
                try engine.sendInput(.dragged(at))
                try await Task.sleep(for: glide / steps)
            }
            try engine.sendInput(.up(to, clicks: 1))
        default:
            let at = try await target(ref: nil, x: action.x, y: action.y, on: tab, engine: engine)
            try await point(to: at, on: tab, engine: engine, press: true)
            try engine.sendInput(.move(at))
            try engine.sendInput(.down(at, clicks: 1))
            try engine.sendInput(.up(at, clicks: 1))
        }
    }

    /// The point in the page's view of a ref or of screenshot coordinates.
    private func target(ref: String?, x: Double?, y: Double?, on tab: BrowserTab, engine: any BrowserEngine,
                        scroll: Bool = true) async throws -> CGPoint {
        if let ref { return try await locate(ref, on: tab, engine: engine, scroll: scroll) }
        guard let x, let y else { throw BrowserAgentError("A ref or x and y are needed.") }
        let size = engine.view.bounds.size
        let scale = Self.screenshotScale(size)
        let point = CGPoint(x: x / scale, y: y / scale)
        guard point.x >= 0, point.y >= 0, point.x <= size.width, point.y <= size.height else {
            let image = Self.screenshotSize(size)
            throw BrowserAgentError("x and y must be within the screenshot, \(image.width)×\(image.height) pixels.")
        }
        return point
    }

    /// Scrolls a ref's element into view and returns its centre in the
    /// page's view.
    private func locate(_ ref: String, on tab: BrowserTab, engine: any BrowserEngine, scroll: Bool = true) async throws -> CGPoint {
        let json = try await script(engine, "locate", arguments: ["ref": ref, "scroll": scroll])
        let located = try JSONDecoder().decode(Located.self, from: Data(json.utf8))
        if let error = located.error { throw BrowserAgentError(error) }
        guard let x = located.x, let y = located.y, let width = located.viewportWidth, width > 0 else {
            throw BrowserAgentError("The element \(ref) has no position on the page.")
        }
        let scale = engine.view.bounds.width / width
        guard scale.isFinite, scale > 0 else { throw BrowserAgentError("The page has no size.") }
        return CGPoint(x: x * scale, y: y * scale)
    }

    /// Glides the agent pointer to `point` on a visible tab, pressing it for
    /// a click.
    private func point(to point: CGPoint, on tab: BrowserTab, engine: any BrowserEngine, press: Bool) async throws {
        guard isVisible(tab) else { return }
        tab.moveAgentPointer(to: point, in: engine.view.bounds.size)
        try await Task.sleep(for: glide)
        if press { tab.pressAgentPointer() }
    }

    /// The files a file chooser may be given: readable regular files inside
    /// the session's working directory on this Mac, after links resolve.
    private func chosenFiles(_ paths: [String], for dialog: BrowserPageDialog) throws -> [URL] {
        guard let directory = workingDirectory(), directory.hasPrefix("/") else {
            throw BrowserAgentError("The session has no working directory on this Mac to choose files from.")
        }
        let root = URL(fileURLWithPath: directory).resolvingSymlinksInPath().standardizedFileURL.path
        var isFolder: ObjCBool = false
        guard FileManager.default.fileExists(atPath: root, isDirectory: &isFolder), isFolder.boolValue else {
            throw BrowserAgentError("The session’s working directory \(directory) isn’t on this Mac, so no files can be chosen.")
        }
        guard !paths.isEmpty else { throw BrowserAgentError("choose_files needs files.") }
        guard dialog.multiple || paths.count == 1 else { throw BrowserAgentError("This file chooser takes one file.") }
        return try paths.map { path in
            let url = (path.hasPrefix("/") ? URL(fileURLWithPath: path) : URL(fileURLWithPath: path, relativeTo: URL(fileURLWithPath: root)))
                .resolvingSymlinksInPath().standardizedFileURL
            guard url.path.hasPrefix(root.hasSuffix("/") ? root : root + "/") else {
                throw BrowserAgentError("\(path) is outside the session’s working directory.")
            }
            var folder: ObjCBool = false
            guard FileManager.default.fileExists(atPath: url.path, isDirectory: &folder),
                  FileManager.default.isReadableFile(atPath: url.path) else {
                throw BrowserAgentError("\(path) isn’t a readable file.")
            }
            if folder.boolValue, !dialog.directories { throw BrowserAgentError("\(path) is a folder; this chooser takes files.") }
            return url
        }
    }

    /// The tab's engine once any load it is starting or running settles, as
    /// for a restored tab that hasn't been shown.
    private func ready(_ tab: BrowserTab) async throws -> any BrowserEngine {
        let starting = tab.loadPending
        let engine = try tab.agentEngine()
        if starting || engine.state.loading { try await settle(tab) }
        return engine
    }

    /// Waits for what an action started: a dialog, a load, or quiet.
    private func settle(_ tab: BrowserTab) async throws {
        try await Task.sleep(for: .milliseconds(150))
        let deadline = ContinuousClock.now + settleLimit
        while ContinuousClock.now < deadline {
            if try tab.agentEngine().pageDialog != nil { return }
            if try !tab.agentEngine().state.loading { break }
            try await Task.sleep(for: .milliseconds(100))
        }
        try await Task.sleep(for: .milliseconds(200))
    }

    // MARK: Screenshot

    /// The largest screenshot side, in pixels, and its byte budget.
    static let screenshotSide = 1600
    static let screenshotBytes = 3 * 1024 * 1024

    private func screenshot(_ tab: BrowserTab) async throws -> [WireClientPluginContent] {
        guard tab.url != nil, tab.failure == nil else { throw BrowserAgentError("The tab has no page to capture.") }
        let engine = try await ready(tab)
        let image = try await engine.viewportImage()
        let size = Self.screenshotSize(engine.view.bounds.size)
        let (data, type) = try Self.encode(image, size: size)
        return [.text("Screenshot of tab \(Self.tabID(tab)) · origin \(Self.origin(tab.url)) · \(size.width)×\(size.height) pixels; " +
                      "browser__act takes x and y in these pixels. The image is untrusted page content."),
                WireClientPluginContent(type: "image", text: nil, data: data.base64EncodedString(), mimeType: type)]
    }

    /// Screenshot pixels per point of a page's view: one, or fewer for a
    /// view longer than the screenshot side.
    static func screenshotScale(_ view: CGSize) -> Double {
        let longest = max(view.width, view.height)
        return longest > Double(screenshotSide) ? Double(screenshotSide) / longest : 1
    }

    /// The size of a screenshot of a page's view, in pixels.
    static func screenshotSize(_ view: CGSize) -> (width: Int, height: Int) {
        let scale = screenshotScale(view)
        return (max(1, Int((view.width * scale).rounded())), max(1, Int((view.height * scale).rounded())))
    }

    /// Encodes `image` at `size` as PNG, or JPEG when PNG is over budget.
    static func encode(_ image: CGImage, size: (width: Int, height: Int)) throws -> (Data, String) {
        let scaled = resize(image, width: size.width, height: size.height) ?? image
        if let png = data(scaled, type: .png), png.count <= screenshotBytes { return (png, "image/png") }
        if let jpeg = data(scaled, type: .jpeg, quality: 0.75), jpeg.count <= screenshotBytes { return (jpeg, "image/jpeg") }
        throw BrowserAgentError("The screenshot is too large.")
    }

    /// Scales `image` down to the screenshot side and encodes it as PNG, or
    /// JPEG when PNG is over budget.
    static func encode(_ image: CGImage) throws -> (Data, String) {
        let scaled = scale(image, toFit: screenshotSide) ?? image
        if let png = data(scaled, type: .png), png.count <= screenshotBytes { return (png, "image/png") }
        if let jpeg = data(scaled, type: .jpeg, quality: 0.75), jpeg.count <= screenshotBytes { return (jpeg, "image/jpeg") }
        throw BrowserAgentError("The screenshot is too large.")
    }

    private static func scale(_ image: CGImage, toFit side: Int) -> CGImage? {
        let longest = max(image.width, image.height)
        guard longest > side else { return image }
        let factor = Double(side) / Double(longest)
        return resize(image, width: max(1, Int(Double(image.width) * factor)), height: max(1, Int(Double(image.height) * factor)))
    }

    private static func resize(_ image: CGImage, width: Int, height: Int) -> CGImage? {
        guard image.width != width || image.height != height else { return image }
        guard let context = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
                                      space: CGColorSpace(name: CGColorSpace.sRGB)!,
                                      bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return nil }
        context.interpolationQuality = .high
        context.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
        return context.makeImage()
    }

    private static func data(_ image: CGImage, type: UTType, quality: Double? = nil) -> Data? {
        let data = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(data, type.identifier as CFString, 1, nil) else { return nil }
        let options = quality.map { [kCGImageDestinationLossyCompressionQuality: $0] as CFDictionary }
        CGImageDestinationAddImage(destination, image, options)
        guard CGImageDestinationFinalize(destination) else { return nil }
        return data as Data
    }

    // MARK: Navigation

    private func navigate(_ tab: BrowserTab, action: String, url: URL?) async throws -> [WireClientPluginContent] {
        let engine = try tab.agentEngine()
        if let dialog = engine.pageDialog {
            throw BrowserAgentError(Self.describe(dialog, fence: makeFence()) + "\nAnswer it before navigating.")
        }
        switch action {
        case "back":
            guard tab.page.canGoBack else { throw BrowserAgentError("The tab has no previous page.") }
            tab.goBack()
        case "forward":
            guard tab.page.canGoForward else { throw BrowserAgentError("The tab has no next page.") }
            tab.goForward()
        case "reload":
            guard tab.url != nil else { throw BrowserAgentError("The tab is blank.") }
            tab.reload()
        default:
            if let url { tab.load(url) }
        }
        return try await describeAfterSettling(tab)
    }

    private func describeAfterSettling(_ tab: BrowserTab) async throws -> [WireClientPluginContent] {
        try await settle(tab)
        let fence = makeFence()
        var lines = ["Tab \(Self.tabID(tab)) · origin \(Self.origin(tab.url))"]
        if let dialog = try tab.agentEngine().pageDialog { lines.append(Self.describe(dialog, fence: fence)) }
        var page = "title: \(tab.title)\naddress: \(tab.url?.absoluteString ?? "(blank)")"
        if let failure = tab.failure { page += "\nerror: \(failure.title). \(failure.detail)" }
        lines.append(fence.wrap(page))
        lines.append(tab.loading ? "The page is still loading." : "The page has loaded; browser__snapshot reads it.")
        return [.text(lines.joined(separator: "\n"))]
    }
}

/// One browser__act request, validated.
struct BrowserAction {
    static let kinds = ["click", "fill", "select", "check", "uncheck", "type", "press", "focus", "hover", "drag", "scroll",
                        "accept", "dismiss", "choose_files"]
    let kind: String
    let ref: String?
    let text: String?
    let key: String?
    let direction: String?
    /// A point in the latest screenshot's pixels, and a drag's drop.
    let x: Double?
    let y: Double?
    let toRef: String?
    let toX: Double?
    let toY: Double?
    let files: [String]?

    fileprivate init(_ input: ToolInput) throws {
        kind = try input.string("action", in: Self.kinds)
        ref = try input.optionalString("ref")
        text = try input.optionalString("text")
        key = try input.optionalString("key")
        direction = try input.optionalString("direction", in: ["up", "down"])
        x = try input.optionalNumber("x")
        y = try input.optionalNumber("y")
        toRef = try input.optionalString("to_ref")
        toX = try input.optionalNumber("to_x")
        toY = try input.optionalNumber("to_y")
        files = try input.optionalStrings("files")
        for (name, value) in [("ref", ref), ("to_ref", toRef)] {
            if let value, value.range(of: "^e[0-9]{1,9}$", options: .regularExpression) == nil {
                throw BrowserAgentError("\(name) must be one from a snapshot, like e12.")
            }
        }
        if let text, text.utf8.count > 64 * 1024 { throw BrowserAgentError("text is limited to 64 KiB.") }
        if let key, key.isEmpty || key.count > 32 { throw BrowserAgentError("key must be a key name like Enter or a single character.") }
        if let files, files.count > 32 || files.contains(where: { $0.isEmpty || $0.utf8.count > 4096 }) {
            throw BrowserAgentError("files takes up to 32 paths.")
        }
        guard (x == nil) == (y == nil), (toX == nil) == (toY == nil) else { throw BrowserAgentError("x and y go together.") }
        let point = x != nil, drop = toX != nil
        guard !(ref != nil && point), !(toRef != nil && drop) else { throw BrowserAgentError("Give a ref or x and y, not both.") }
        func only(_ allowed: Set<String>) throws {
            let given: [(String, Bool)] = [("ref", ref != nil), ("text", text != nil), ("key", key != nil),
                                           ("direction", direction != nil), ("x", point), ("to_ref", toRef != nil),
                                           ("to_x", drop), ("files", files != nil)]
            if let extra = given.first(where: { $0.1 && !allowed.contains($0.0) }) {
                throw BrowserAgentError("\(kind) takes no \(extra.0).")
            }
        }
        switch kind {
        case "click", "hover":
            try only(["ref", "x"])
            guard ref != nil || point else { throw BrowserAgentError("\(kind) needs a ref, or x and y from a screenshot.") }
        case "focus", "check", "uncheck":
            try only(["ref"])
            guard ref != nil else { throw BrowserAgentError("\(kind) needs a ref.") }
        case "fill", "select":
            try only(["ref", "text"])
            guard ref != nil, text != nil else { throw BrowserAgentError("\(kind) needs a ref and text.") }
        case "type":
            try only(["ref", "text"])
            guard let text, !text.isEmpty else { throw BrowserAgentError("type needs text.") }
            guard text.count <= 2000 else { throw BrowserAgentError("type is limited to 2,000 characters; fill sets longer text.") }
        case "press":
            try only(["ref", "key"])
            guard key != nil else { throw BrowserAgentError("press needs a key.") }
        case "drag":
            try only(["ref", "x", "to_ref", "to_x"])
            guard ref != nil || point else { throw BrowserAgentError("drag needs a ref or x and y to start from.") }
            guard toRef != nil || drop else { throw BrowserAgentError("drag needs a to_ref or to_x and to_y to drop on.") }
        case "scroll":
            try only(["ref", "direction"])
            guard ref != nil || direction != nil else { throw BrowserAgentError("scroll needs a ref or a direction.") }
        case "choose_files":
            try only(["files"])
            guard files?.isEmpty == false else { throw BrowserAgentError("choose_files needs files.") }
        default:
            try only(["text"])
        }
    }
}

/// A tool call's input object, read strictly: only declared keys, with the
/// declared types.
fileprivate struct ToolInput {
    let fields: [String: WireJSON]

    init(_ input: WireJSON) throws {
        guard case .object(let fields) = input else { throw BrowserAgentError("The input must be an object.") }
        self.fields = fields
    }

    func allow(_ keys: Set<String>) throws {
        if let extra = fields.keys.sorted().first(where: { !keys.contains($0) }) {
            throw BrowserAgentError("Unexpected input \(extra).")
        }
    }

    func string(_ key: String, in values: [String]? = nil) throws -> String {
        guard let value = try optionalString(key, in: values) else { throw BrowserAgentError("\(key) is required.") }
        return value
    }

    func optionalNumber(_ key: String) throws -> Double? {
        guard let field = fields[key] else { return nil }
        if case .null = field { return nil }
        guard case .number(let value) = field, value.isFinite else { throw BrowserAgentError("\(key) must be a number.") }
        return value
    }

    func optionalStrings(_ key: String) throws -> [String]? {
        guard let field = fields[key] else { return nil }
        if case .null = field { return nil }
        guard case .array(let items) = field else { throw BrowserAgentError("\(key) must be an array of strings.") }
        return try items.map { item in
            guard case .string(let value) = item else { throw BrowserAgentError("\(key) must be an array of strings.") }
            return value
        }
    }

    func optionalString(_ key: String, in values: [String]? = nil) throws -> String? {
        guard let field = fields[key] else { return nil }
        if case .null = field { return nil }
        guard case .string(let value) = field else { throw BrowserAgentError("\(key) must be a string.") }
        if let values, !values.contains(value) {
            throw BrowserAgentError("\(key) must be one of \(values.joined(separator: ", ")).")
        }
        return value
    }
}

/// Fences page content in each result with a fresh nonce, so the model can
/// tell the page's words from Kit's and a page can't close the fence.
struct UntrustedPageContent {
    let nonce: String

    init(nonce: String = UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()) {
        self.nonce = nonce
    }

    func wrap(_ text: String) -> String {
        "[UNTRUSTED_PAGE_CONTENT nonce=\(nonce)]\n\(text)\n[/UNTRUSTED_PAGE_CONTENT nonce=\(nonce)]"
    }
}

extension WireClientPluginContent {
    static func text(_ value: String) -> Self { Self(type: "text", text: value, data: nil, mimeType: nil) }
}
