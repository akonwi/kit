import AppKit
import SwiftUI

/// What the pointer shows over each kind of clickable transcript element.
/// Links, and elements that open something as a link does, take the
/// pointing hand; buttons, disclosures, and rows keep the arrow and show a
/// hover fill instead, as macOS controls do.
enum TranscriptClickable: CaseIterable {
    /// A link in rendered Markdown: a web URL, a file path, or a session mention.
    case markdownLink
    /// A tool row's ↗ chip, which opens its file, session, agent, or scratchpad.
    case toolChip
    /// A file path in a tool call's detail well.
    case wellFilePath
    /// The session a fork marker names.
    case forkedSession
    /// An image attachment's preview, which opens the image viewer.
    case imagePreview
    /// A tool row that expands its detail well.
    case toolRow
    /// The "N tool calls" disclosure above a group of tool rows.
    case toolGroup
    /// The compaction marker, which expands its summary.
    case compaction
    /// A plugin message row, which expands the message.
    case pluginMessage
    /// Copy or Open on a fenced code, diff, Mermaid, or figure card.
    case fenceAction
    /// Save As… or Retry on an attachment card.
    case attachmentAction

    enum Pointer {
        case hand, arrow

        var cursor: NSCursor {
            switch self {
            case .hand: .pointingHand
            case .arrow: .arrow
            }
        }
    }

    /// The pointer shown over the element.
    var pointer: Pointer {
        switch self {
        case .markdownLink, .toolChip, .wellFilePath, .forkedSession, .imagePreview: .hand
        case .toolRow, .toolGroup, .compaction, .pluginMessage, .fenceAction, .attachmentAction: .arrow
        }
    }

    /// The SwiftUI pointer style for `pointer`: the link pointer, or nil for
    /// the arrow.
    var pointerStyle: PointerStyle? { pointer == .hand ? .link : nil }
}

/// Shows that links in selectable text are clickable: while the pointer is
/// over a link in a selectable text field inside `region`, the cursor is the
/// pointing hand and the link is underlined in `color`.
///
/// SwiftUI draws selectable `Text` in an `NSTextField` that sets the hand
/// over its links only once it's being edited, after a click. One tracking
/// area over the region finds the link under the pointer as it moves, so a
/// long transcript has no cursor rects or tracking areas per link. The
/// underline is a view over the field that only paints: neither the field's
/// text nor any layout changes, and selecting by dragging across a link
/// works as before.
///
/// With `linkActions` set, right-clicking or Control-clicking a web link
/// shows a menu for the link in place of AppKit's text menu, whose Open Link
/// opens the system's default browser; see `linkMenu(for:)`.
@MainActor final class TextLinkHover: NSResponder {
    /// The link under the pointer.
    struct Hovered: Equatable {
        weak var field: NSView?
        let range: NSRange
        /// The underline's rects, in the field's coordinates.
        let underlines: [NSRect]
    }

    /// What a web link's menu does.
    struct LinkActions {
        /// Opens a link as clicking it does.
        var open: @MainActor (URL) -> Void
        /// Opens a link in the system's default browser.
        var openInDefaultBrowser: @MainActor (URL) -> Void = { NSWorkspace.shared.open($0) }
        /// Where Copy Link copies.
        var pasteboard: NSPasteboard = .general
    }

    private weak var region: NSView?
    private var area: NSTrackingArea?
    private var scrollMonitor: Any?
    private var menuMonitor: Any?
    /// What a web link's menu does; without them, links keep AppKit's menu.
    var linkActions: LinkActions?
    /// The hovers tracking a region, which right-clicks on links go to.
    private static var attached: [Weak] = []
    private struct Weak { weak var hover: TextLinkHover? }
    private var refreshQueued = false
    private var layout: FieldLayout?
    private let underline = TextLinkUnderlineView()
    /// The underline's color.
    var color: NSColor {
        didSet { underline.color = color }
    }
    private(set) var hovered: Hovered?

    init(color: NSColor) {
        self.color = color
        underline.color = color
        super.init()
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    /// Tracks the pointer over `view`.
    func attach(to view: NSView) {
        detach()
        region = view
        let area = NSTrackingArea(rect: .zero, options: [.mouseMoved, .mouseEnteredAndExited, .activeInKeyWindow, .inVisibleRect],
                                  owner: self, userInfo: nil)
        view.addTrackingArea(area)
        self.area = area
        // Content scrolled under a still pointer changes what it's over.
        scrollMonitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] event in
            guard let self, let region = self.region, event.window === region.window, !self.refreshQueued else { return event }
            // One refresh per pass of the main loop, however many events a
            // momentum scroll delivers.
            self.refreshQueued = true
            DispatchQueue.main.async { [weak self] in
                self?.refreshQueued = false
                self?.refresh()
            }
            return event
        }
        menuMonitor = NSEvent.addLocalMonitorForEvents(matching: [.rightMouseDown, .leftMouseDown]) { [weak self] event in
            guard let self, let menu = self.linkMenu(for: event), let field = self.field(at: event.locationInWindow) else { return event }
            NSMenu.popUpContextMenu(menu, with: event, for: field)
            self.refresh()
            return nil
        }
        Self.attached.removeAll { $0.hover == nil || $0.hover === self }
        Self.attached.append(Weak(hover: self))
    }

    func detach() {
        if let area, let region { region.removeTrackingArea(area) }
        area = nil
        if let scrollMonitor { NSEvent.removeMonitor(scrollMonitor) }
        scrollMonitor = nil
        if let menuMonitor { NSEvent.removeMonitor(menuMonitor) }
        menuMonitor = nil
        Self.attached.removeAll { $0.hover == nil || $0.hover === self }
        clear(resetCursor: hovered != nil)
        region = nil
        layout = nil
    }

    override func mouseMoved(with event: NSEvent) { update(at: event.locationInWindow) }
    override func mouseEntered(with event: NSEvent) { update(at: event.locationInWindow) }
    override func mouseExited(with event: NSEvent) { clear(resetCursor: hovered != nil) }

    /// Re-evaluates the pointer's current location, as after a scroll.
    func refresh() {
        guard let window = region?.window, window.isKeyWindow else { clear(resetCursor: hovered != nil); return }
        let point = window.convertPoint(fromScreen: NSEvent.mouseLocation)
        guard region.map({ $0.visibleRect.contains($0.convert(point, from: nil)) }) == true else {
            clear(resetCursor: hovered != nil)
            return
        }
        update(at: point)
    }

    /// Shows the link under `point`, in window coordinates, if any.
    func update(at point: NSPoint) {
        guard let hit = view(at: point) else {
            clear(resetCursor: hovered != nil)
            return
        }
        let field = hit as? NSTextField
        guard let field, field.isSelectable, let next = link(in: field, at: field.convert(point, from: nil)) else {
            if hovered != nil {
                clear(resetCursor: false)
                (field?.isSelectable == true ? NSCursor.iBeam : NSCursor.arrow).set()
            }
            return
        }
        TranscriptClickable.markdownLink.pointer.cursor.set()
        guard next != hovered else { return }
        hovered = next
        underline.removeFromSuperview()
        underline.frame = field.bounds
        underline.autoresizingMask = [.width, .height]
        underline.drawsFlipped = field.isFlipped
        underline.rects = next.underlines
        field.addSubview(underline)
    }

    /// The view at `point`, in window coordinates, within the region.
    private func view(at point: NSPoint) -> NSView? {
        // The region may sit behind the text it tracks, so it is a place, not
        // an ancestor. `hitTest` takes a point in the superview's coordinates.
        // A workspace keeps hidden tabs, transparent, where shown ones are.
        guard let region, let window = region.window, let root = window.contentView,
              region.visibleRect.contains(region.convert(point, from: nil)),
              sequence(first: region, next: \.superview).allSatisfy({ !$0.isHidden && $0.alphaValue > 0 }) else { return nil }
        return root.hitTest(root.superview?.convert(point, from: nil) ?? point)
    }

    /// The selectable text field at `point`, in window coordinates. A field
    /// with a selection lies under its field editor.
    private func field(at point: NSPoint) -> NSTextField? {
        let hit = view(at: point)
        let editor = (hit as? NSTextView).flatMap { $0.isFieldEditor ? $0 : nil }
        guard let field = (hit as? NSTextField) ?? (editor?.delegate as? NSTextField), field.isSelectable else { return nil }
        return field
    }

    /// The menu a right-click or Control-click `event` on a web link shows:
    /// Open Link, which opens it as a click does; Open in Default Browser;
    /// and Copy Link. When the link's text has a selection, AppKit's items
    /// for the selection follow, without its own items for the link. Nil for
    /// any other event, which keeps AppKit's menu, as for a file path or a
    /// session mention.
    func linkMenu(for event: NSEvent) -> NSMenu? {
        let secondary = event.type == .rightMouseDown
            || (event.type == .leftMouseDown && event.modifierFlags.contains(.control))
        guard secondary, let actions = linkActions, let window = region?.window, event.window === window,
              let field = field(at: event.locationInWindow),
              let link = link(in: field, at: field.convert(event.locationInWindow, from: nil)),
              let url = Self.url(field.attributedStringValue.attribute(.link, at: link.range.location, effectiveRange: nil)),
              BrowserAddress.isWeb(url) else { return nil }
        let menu = NSMenu()
        menu.autoenablesItems = false
        let target = LinkMenuTarget(url: url, actions: actions)
        for (title, action) in [("Open Link", #selector(LinkMenuTarget.open)),
                                ("Open in Default Browser", #selector(LinkMenuTarget.openInDefaultBrowser)),
                                ("Copy Link", #selector(LinkMenuTarget.copyLink))] {
            let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
            item.target = target
            // The menu keeps its items, and each its represented object, so
            // the target lives as long as the menu.
            item.representedObject = target
            menu.addItem(item)
        }
        let selectionItems = Self.selectionItems(in: field, for: event)
        if !selectionItems.isEmpty {
            menu.autoenablesItems = true
            menu.addItem(.separator())
            for item in selectionItems { menu.addItem(item) }
        }
        return menu
    }

    /// The menu a tracked region shows for `event`, a click on a web link.
    static func anyLinkMenu(for event: NSEvent) -> NSMenu? {
        attached.lazy.compactMap { $0.hover?.linkMenu(for: event) }.first
    }

    /// Whether some tracked region shows its own menu for `event`, a click
    /// on a web link, in place of the menus others would show.
    static func showsLinkMenu(for event: NSEvent) -> Bool { anyLinkMenu(for: event) != nil }

    private static func url(_ value: Any?) -> URL? {
        switch value {
        case let url as URL: url
        case let string as String: URL(string: string)
        default: nil
        }
    }

    /// AppKit's menu items for the text selected in `field`, without its
    /// items for the link `event` is over; none without a selection there.
    private static func selectionItems(in field: NSTextField, for event: NSEvent) -> [NSMenuItem] {
        // The field edits, and selects, in the window's field editor.
        guard let editor = field.window?.firstResponder as? NSTextView, editor.isFieldEditor, editor.delegate === field,
              editor.selectedRange().length > 0,
              let menu = editor.menu(for: event) else { return [] }
        var items = menu.items
        menu.removeAllItems()
        let linkActions: Set<Selector> = [NSSelectorFromString("_openLinkFromMenu:"), NSSelectorFromString("copyLink:")]
        // Open With, a submenu of applications, follows Open Link.
        if let open = items.firstIndex(where: { $0.action == NSSelectorFromString("_openLinkFromMenu:") }),
           open + 1 < items.count, items[open + 1].hasSubmenu {
            items.remove(at: open + 1)
        }
        items.removeAll { $0.action.map(linkActions.contains) == true }
        // No separator leads, trails, or follows another.
        var kept: [NSMenuItem] = []
        for item in items where !(item.isSeparatorItem && (kept.last?.isSeparatorItem ?? true)) { kept.append(item) }
        while kept.last?.isSeparatorItem == true { kept.removeLast() }
        return kept
    }

    private func clear(resetCursor: Bool) {
        if resetCursor { NSCursor.arrow.set() }
        hovered = nil
        underline.removeFromSuperview()
    }

    /// The link at `point`, in `field`'s coordinates, and its underline.
    func link(in field: NSTextField, at point: NSPoint) -> Hovered? {
        let text = field.attributedStringValue
        let bounds = field.cell?.drawingRect(forBounds: field.bounds) ?? field.bounds
        let layout: FieldLayout
        if let cached = self.layout, cached.field === field, cached.width == bounds.width, cached.text.isEqual(to: text) {
            layout = cached
        } else {
            layout = FieldLayout(field: field, text: text, width: bounds.width)
            self.layout = layout
        }
        guard layout.hasLinks else { return nil }
        let local = NSPoint(x: point.x - bounds.minX, y: field.isFlipped ? point.y - bounds.minY : bounds.maxY - point.y)
        guard let range = layout.link(at: local) else { return nil }
        let rects = layout.underlines(for: range).map { rect in
            let origin = field.isFlipped ? rect.minY + bounds.minY : bounds.maxY - rect.maxY
            return NSRect(x: rect.minX + bounds.minX, y: origin, width: rect.width, height: rect.height)
        }
        return Hovered(field: field, range: range, underlines: rects)
    }

    /// A field's text laid out as the field lays it out for editing, where
    /// its clicks land.
    private final class FieldLayout {
        weak var field: NSTextField?
        let text: NSAttributedString
        let width: CGFloat
        let hasLinks: Bool
        private let storage: NSTextStorage
        private let manager = NSLayoutManager()
        private let container: NSTextContainer
        /// The text field cell's inset of each line fragment.
        static let padding: CGFloat = 2

        init(field: NSTextField, text: NSAttributedString, width: CGFloat) {
            self.field = field
            self.text = text
            self.width = width
            var links = false
            text.enumerateAttribute(.link, in: NSRange(location: 0, length: text.length)) { value, _, stop in
                if value != nil { links = true; stop.pointee = true }
            }
            hasLinks = links
            storage = NSTextStorage(attributedString: text)
            container = NSTextContainer(size: NSSize(width: width, height: .greatestFiniteMagnitude))
            container.lineFragmentPadding = Self.padding
            manager.addTextContainer(container)
            storage.addLayoutManager(manager)
        }

        func link(at point: NSPoint) -> NSRange? {
            guard storage.length > 0 else { return nil }
            let glyph = manager.glyphIndex(for: point, in: container)
            let box = manager.boundingRect(forGlyphRange: NSRange(location: glyph, length: 1), in: container)
            guard box.contains(point) else { return nil }
            let character = manager.characterIndexForGlyph(at: glyph)
            var range = NSRange()
            guard storage.attribute(.link, at: character, longestEffectiveRange: &range,
                                    in: NSRange(location: 0, length: storage.length)) != nil else { return nil }
            return range
        }

        /// An underline under each line's part of `range`, as the text
        /// system draws one, omitting the whitespace a line wraps after.
        func underlines(for range: NSRange) -> [NSRect] {
            let glyphs = manager.glyphRange(forCharacterRange: range, actualCharacterRange: nil)
            let string = storage.string as NSString
            var rects: [NSRect] = []
            manager.enumerateLineFragments(forGlyphRange: glyphs) { line, _, _, lineGlyphs, _ in
                var part = NSIntersectionRange(glyphs, lineGlyphs)
                while part.length > 0 {
                    let character = self.manager.characterIndexForGlyph(at: NSMaxRange(part) - 1)
                    guard let scalar = Unicode.Scalar(string.character(at: character)),
                          CharacterSet.whitespacesAndNewlines.contains(scalar) else { break }
                    part.length -= 1
                }
                guard part.length > 0 else { return }
                let box = self.manager.boundingRect(forGlyphRange: part, in: self.container)
                let character = self.manager.characterIndexForGlyph(at: part.location)
                let font = self.storage.attribute(.font, at: character, effectiveRange: nil) as? NSFont
                    ?? NSFont.systemFont(ofSize: NSFont.systemFontSize)
                let baseline = line.minY + self.manager.location(forGlyphAt: part.location).y
                let thickness = max(1, font.underlineThickness)
                rects.append(NSRect(x: box.minX, y: baseline - font.underlinePosition - thickness / 2,
                                    width: box.width, height: thickness))
            }
            return rects
        }
    }
}

/// Carries out a web link menu's items.
@MainActor private final class LinkMenuTarget: NSObject {
    let url: URL
    let actions: TextLinkHover.LinkActions

    init(url: URL, actions: TextLinkHover.LinkActions) {
        self.url = url
        self.actions = actions
    }

    @objc func open() { actions.open(url) }
    @objc func openInDefaultBrowser() { actions.openInDefaultBrowser(url) }
    @objc func copyLink() {
        actions.pasteboard.clearContents()
        actions.pasteboard.writeObjects([url as NSURL])
        actions.pasteboard.setString(url.absoluteString, forType: .string)
    }
}

/// Paints a hovered link's underline over its text field.
final class TextLinkUnderlineView: NSView {
    var color: NSColor = .linkColor { didSet { needsDisplay = true } }
    var rects: [NSRect] = [] { didSet { needsDisplay = true } }
    var drawsFlipped = true
    override var isFlipped: Bool { drawsFlipped }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
    override func draw(_ dirtyRect: NSRect) {
        color.setFill()
        for rect in rects { backingAlignedRect(rect, options: .alignAllEdgesNearest).fill() }
    }
}

/// Tracks links under the pointer over the view it backs; see
/// `TextLinkHover`. Put it behind a SwiftUI region of selectable text. A web
/// link's menu opens the link with the environment's `openURL`, as a click
/// on it does.
struct TextLinkHoverRegion: NSViewRepresentable {
    let color: Color

    func makeNSView(context: Context) -> RegionView { RegionView(hover: context.coordinator) }
    func updateNSView(_ view: RegionView, context: Context) {
        context.coordinator.color = NSColor(color)
        let open = context.environment.openURL
        context.coordinator.linkActions = .init(open: { open($0) })
    }
    func makeCoordinator() -> TextLinkHover { TextLinkHover(color: NSColor(color)) }
    static func dismantleNSView(_ view: RegionView, coordinator: TextLinkHover) { coordinator.detach() }

    final class RegionView: NSView {
        let hover: TextLinkHover
        init(hover: TextLinkHover) {
            self.hover = hover
            super.init(frame: .zero)
        }
        required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if window == nil { hover.detach() } else { hover.attach(to: self) }
        }
    }
}
