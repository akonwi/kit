import AppKit
import KitClient
import Observation

/// An annotation made in a browser tab, ready for the composer: the
/// person's comment, the capture, and a PNG of the element when asked for.
struct BrowserElementAnnotation {
    let comment: String
    let capture: BrowserElementCapture
    var screenshot: Data?
}

/// A browser tab's Annotate mode. Kit takes the pointer over the page and
/// draws the outlines; the page's script, in its own content world, finds
/// the elements. The outline follows the pointer to what's directly under
/// it, a click selects that, ⌘ reaches the innermost element, and parent
/// and child step out and back in.
@MainActor @Observable
final class BrowserAnnotationSession {
    private(set) var page = BrowserAnnotationPageState.empty
    /// The comment being written for the selection.
    var comment = ""
    /// Whether the element's image goes with the comment. The person's
    /// last choice is kept.
    var attachScreenshot: Bool {
        didSet { defaults.set(attachScreenshot, forKey: Self.screenshotKey) }
    }
    private(set) var adding = false
    private(set) var error: String?
    /// The page view's size, for mapping between view points and the
    /// page's CSS pixels and clipping the screenshot.
    var viewSize = CGSize.zero
    /// Counts clicks on the page, even ones that keep the selection, so the
    /// comment takes the keyboard back after each.
    private(set) var clicks = 0

    static let screenshotKey = "BrowserAnnotation.attachScreenshot"
    /// Space around the element in its screenshot, in points.
    static let screenshotMargin: CGFloat = 8
    /// How often the outlines follow a page that moves or animates.
    static let followInterval: Duration = .milliseconds(100)

    @ObservationIgnored private let engine: any BrowserEngine
    @ObservationIgnored private let defaults: UserDefaults
    /// Counts the pages and the mode's end, so an answer for a page that
    /// has gone, or after the mode ended, is dropped.
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var pendingHover: (point: CGPoint, deep: Bool)?
    @ObservationIgnored private var hovering: Task<Void, Never>?
    @ObservationIgnored private var running = 0

    init(engine: any BrowserEngine, defaults: UserDefaults = .standard) {
        self.engine = engine
        self.defaults = defaults
        attachScreenshot = defaults.object(forKey: Self.screenshotKey) as? Bool ?? true
    }

    /// View points per CSS pixel; 1 until the page reports its viewport.
    var scale: CGFloat {
        guard let width = page.viewport?.width, width > 0, viewSize.width > 0 else { return 1 }
        return viewSize.width / width
    }

    /// A target's box in the page view's points.
    func viewRect(_ target: BrowserAnnotationTarget) -> CGRect {
        let rect = target.rect.cgRect, scale = scale
        return CGRect(x: rect.minX * scale, y: rect.minY * scale, width: rect.width * scale, height: rect.height * scale)
    }

    /// Outlines what a click at `point` would pick. Moves while a page
    /// answers are coalesced to the latest.
    func hover(at point: CGPoint, deep: Bool) {
        pendingHover = (point, deep)
        guard hovering == nil else { return }
        hovering = Task { [weak self] in
            while let self, let next = self.pendingHover {
                self.pendingHover = nil
                let css = self.css(next.point)
                await self.run("hover", ["x": css.x, "y": css.y, "deep": next.deep])
            }
            self?.hovering = nil
        }
    }

    /// The pointer left the page.
    func hoverEnded() {
        pendingHover = nil
        if page.hovered != nil { page.hovered = nil }
    }

    /// Selects what's under `point`, or the innermost element when `deep`.
    func click(at point: CGPoint, deep: Bool) async {
        pendingHover = nil
        let css = css(point)
        await run("select", ["x": css.x, "y": css.y, "deep": deep])
        error = nil
        clicks += 1
    }

    enum Step { case parent, child }

    func step(_ step: Step) async {
        await run("step", ["to": step == .parent ? "parent" : "child"])
    }

    /// Selects the selection's ancestor at `index` of its crumbs.
    func selectCrumb(_ index: Int) async {
        await run("step", ["to": "crumb", "index": index])
    }

    /// Keeps the outlines on elements that scroll, move, or resize.
    func follow() async {
        guard running == 0, page.hovered != nil || page.selected != nil else { return }
        await run("state", [:])
    }

    /// Lets the selection go, keeping the mode.
    func deselect() async {
        comment = ""
        error = nil
        await run("clear", [:])
    }

    /// The selection with its comment, captured, and its image when asked
    /// for. Nil when nothing is selected or the page can't be read.
    func capture() async -> BrowserElementAnnotation? {
        let comment = comment.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let selected = page.selected, !comment.isEmpty, !adding else { return nil }
        adding = true
        defer { adding = false }
        let page = generation
        do {
            let json = try await engine.runAnnotationScript("capture", arguments: [:])
            guard generation == page else { return nil }
            if let failure = try? JSONDecoder().decode(ScriptError.self, from: Data(json.utf8)) {
                error = failure.error
                return nil
            }
            let capture = try JSONDecoder().decode(BrowserElementCapture.self, from: Data(json.utf8))
            var screenshot: Data?
            if attachScreenshot { screenshot = try? await image(of: viewRect(selected)) }
            guard generation == page else { return nil }
            return BrowserElementAnnotation(comment: comment, capture: capture, screenshot: screenshot)
        } catch {
            self.error = "The element couldn’t be read. The page may have changed."
            return nil
        }
    }

    /// A new page shows: nothing is outlined on it yet.
    func pageChanged() {
        generation += 1
        pendingHover = nil
        page = .empty
        error = nil
    }

    /// The mode ends: the page script lets its elements go.
    func end() {
        generation += 1
        pendingHover = nil
        hovering?.cancel()
        page = .empty
        let engine = engine
        Task { _ = try? await engine.runAnnotationScript("clear", arguments: [:]) }
    }

    private struct ScriptError: Decodable { let error: String }

    private func css(_ point: CGPoint) -> CGPoint {
        let scale = scale
        return CGPoint(x: point.x / scale, y: point.y / scale)
    }

    private func run(_ operation: String, _ arguments: [String: Any]) async {
        let page = generation
        running += 1
        defer { running -= 1 }
        guard let json = try? await engine.runAnnotationScript(operation, arguments: arguments),
              let state = try? JSONDecoder().decode(BrowserAnnotationPageState.self, from: Data(json.utf8)),
              generation == page else { return }
        if self.page != state { self.page = state }
    }

    /// A PNG of `rect` of the page with a margin, clipped to the view.
    private func image(of rect: CGRect) async throws -> Data? {
        let bounds = CGRect(origin: .zero, size: viewSize)
        let area = rect.insetBy(dx: -Self.screenshotMargin, dy: -Self.screenshotMargin).intersection(bounds).integral
        guard !area.isNull, area.width >= 1, area.height >= 1 else { return nil }
        let image = try await engine.pageImage(of: area)
        return NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:])
    }
}

/// The browser annotations waiting in a session's composer. On send each
/// becomes a text block after the message, and its screenshot an image
/// attachment uploaded when it was added. Nothing is stored on the server
/// until the message is sent.
@MainActor @Observable
final class BrowserAnnotationDraft {
    struct Item: Identifiable {
        let id: UUID
        let number: Int
        let comment: String
        let capture: BrowserElementCapture
        /// The screenshot's upload in `screenshots`, if one was attached.
        var upload: UUID?
        var screenshotName: String { "browser-element-\(number).png" }
    }
    private(set) var items: [Item] = []
    /// The screenshots' uploads, apart from the composer's own files.
    let screenshots = ComposerAttachments()
    var error: String?
    @ObservationIgnored private var nextNumber = 1

    /// The most annotations one message holds.
    static let limit = 16

    var isEmpty: Bool { items.isEmpty }
    /// Whether every screenshot has uploaded, so the message can be sent.
    var ready: Bool { screenshots.ready }
    var attachmentIDs: [String] {
        items.compactMap { item in item.upload.flatMap(screenshot)?.uploaded?.id }
    }
    var hasImages: Bool { screenshots.items.contains(where: \.isImage) }

    func screenshot(_ upload: UUID) -> ComposerAttachments.Item? {
        screenshots.items.first { $0.id == upload }
    }

    /// Adds an annotation, uploading its screenshot through `client` when
    /// there is one. Returns false when the message holds all it can.
    @discardableResult
    func add(_ annotation: BrowserElementAnnotation, client: (any ComposerClient)?, session: String,
             imageRejectingModel: @escaping @MainActor () -> String? = { nil }) -> Bool {
        guard items.count < Self.limit else {
            error = "A message can contain up to \(Self.limit) browser annotations."
            return false
        }
        error = nil
        if items.isEmpty { nextNumber = 1 }
        var item = Item(id: UUID(), number: nextNumber, comment: annotation.comment, capture: annotation.capture)
        nextNumber += 1
        if let data = annotation.screenshot, let client, let upload = screenshots.reserve(filename: item.screenshotName) {
            item.upload = upload
            screenshots.add(filename: item.screenshotName, data: data, client: client, session: session, reserved: upload,
                            imageRejectingModel: imageRejectingModel)
        }
        items.append(item)
        return true
    }

    func remove(_ id: UUID) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        if let upload = items[index].upload { screenshots.remove(upload) }
        items.remove(at: index)
        // A notice about annotations that are gone no longer applies.
        if items.isEmpty { error = nil; screenshots.error = nil }
    }

    /// Sends the annotation without its screenshot, as when it can't upload.
    func dropScreenshot(_ id: UUID) {
        guard let index = items.firstIndex(where: { $0.id == id }), let upload = items[index].upload else { return }
        screenshots.remove(upload)
        items[index].upload = nil
    }

    /// The blocks the model gets after the message's text, or "" for none.
    func messageText(fence: () -> UntrustedPageContent = { UntrustedPageContent() }) -> String {
        items.map { item in
            let uploaded = item.upload.flatMap(screenshot)?.uploaded != nil
            return BrowserAnnotationText.block(number: item.number, comment: item.comment,
                                               screenshot: uploaded ? item.screenshotName : nil,
                                               capture: item.capture, fence: fence())
        }.joined(separator: "\n\n")
    }

    /// Removes the annotations a sent message carried.
    func acknowledge(_ ids: [UUID]) {
        for id in ids { remove(id) }
    }
}
