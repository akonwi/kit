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
@MainActor final class TextLinkHover: NSResponder {
    /// The link under the pointer.
    struct Hovered: Equatable {
        weak var field: NSView?
        let range: NSRange
        /// The underline's rects, in the field's coordinates.
        let underlines: [NSRect]
    }

    private weak var region: NSView?
    private var area: NSTrackingArea?
    private var scrollMonitor: Any?
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
    }

    func detach() {
        if let area, let region { region.removeTrackingArea(area) }
        area = nil
        if let scrollMonitor { NSEvent.removeMonitor(scrollMonitor) }
        scrollMonitor = nil
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
        // The region may sit behind the text it tracks, so it is a place, not
        // an ancestor. `hitTest` takes a point in the superview's coordinates.
        guard let region, let window = region.window, let root = window.contentView,
              region.visibleRect.contains(region.convert(point, from: nil)),
              let hit = root.hitTest(root.superview?.convert(point, from: nil) ?? point) else {
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
/// `TextLinkHover`. Put it behind a SwiftUI region of selectable text.
struct TextLinkHoverRegion: NSViewRepresentable {
    let color: Color

    func makeNSView(context: Context) -> RegionView { RegionView(hover: context.coordinator) }
    func updateNSView(_ view: RegionView, context: Context) { context.coordinator.color = NSColor(color) }
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
