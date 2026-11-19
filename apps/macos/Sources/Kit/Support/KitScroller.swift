import AppKit
@preconcurrency import CodeEditSourceEditor
import SwiftUI

/// A scroller in the theme's colours. When the system shows legacy scroll
/// bars, as with a mouse attached, it draws no track, so the scroll view's
/// background runs through the gutter. Its knob is a pill in `textMuted`
/// that is thin at rest and widens under the pointer or while dragged, as
/// an overlay scroller's does. Overlay scrollers are the system's own.
///
/// The gutter keeps its width, so the System Settings choice to always
/// show scroll bars still holds: the knob stays visible and clickable.
@MainActor final class KitScroller: NSScroller {
    /// Thickness of the knob at rest and when widened, in points.
    static let restingThickness: CGFloat = 6
    static let widenedThickness: CGFloat = 9
    /// Space between the knob and the gutter's far edge. The system's knob
    /// span keeps the same space from the gutter's ends.
    static let edgeInset: CGFloat = 3

    var theme = MicaTheme(dark: false) {
        didSet { if theme != oldValue { updateKnob() } }
    }

    /// 0 at rest, 1 widened. Animates through the view's animator.
    @objc dynamic var expansion: CGFloat = 0 {
        didSet { if expansion != oldValue { updateKnob() } }
    }

    /// Whether the system's Increase Contrast setting is on.
    var increasesContrast: @MainActor () -> Bool = { NSWorkspace.shared.accessibilityDisplayShouldIncreaseContrast } {
        didSet { updateKnob() }
    }

    /// The legacy knob. The system's legacy drawing tints whatever is drawn
    /// in its knob, so the knob is a view of its own.
    private let knob = KnobView()
    private var hovering = false
    private var dragging = false
    private var hoverArea: NSTrackingArea?
    private var contrastObserver: NSObjectProtocol?

    override class var isCompatibleWithOverlayScrollers: Bool { true }

    override class func defaultAnimation(forKey key: NSAnimatablePropertyKey) -> Any? {
        key == "expansion" ? CABasicAnimation() : super.defaultAnimation(forKey: key)
    }

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        addSubview(knob)
        contrastObserver = NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.accessibilityDisplayOptionsDidChangeNotification, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.updateKnob() }
            }
        updateKnob()
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    isolated deinit {
        if let contrastObserver { NSWorkspace.shared.notificationCenter.removeObserver(contrastObserver) }
    }

    private var legacy: Bool { scrollerStyle == .legacy }
    private var vertical: Bool { bounds.height >= bounds.width }

    override var isOpaque: Bool { legacy ? false : super.isOpaque }

    // The system draws overlay scrollers; a legacy scroller draws no track.
    override func draw(_ dirtyRect: NSRect) {
        if !legacy { super.draw(dirtyRect) }
    }
    override func drawKnobSlot(in slotRect: NSRect, highlight flag: Bool) {
        if !legacy { super.drawKnobSlot(in: slotRect, highlight: flag) }
    }
    override func drawKnob() {
        if !legacy { super.drawKnob() }
    }

    // What moves or resizes the knob.
    override var doubleValue: Double { didSet { updateKnob() } }
    override var knobProportion: CGFloat { didSet { updateKnob() } }
    override var isEnabled: Bool { didSet { updateKnob() } }
    override var scrollerStyle: NSScroller.Style {
        didSet {
            if !legacy { hovering = false; dragging = false; expansion = 0 }
            updateKnob()
        }
    }
    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        updateKnob()
    }
    override func layout() {
        super.layout()
        updateKnob()
    }

    private func updateKnob() {
        let rect = legacy && usableParts == .allScrollerParts ? knobRect() : .zero
        knob.isHidden = rect.isEmpty
        guard !rect.isEmpty else { return }
        knob.frame = rect
        knob.color = knobColor
    }

    /// The knob's rectangle: the system's knob span along the scroller, at
    /// the current thickness against the gutter's far edge.
    func knobRect() -> NSRect {
        let span = rect(for: .knob)
        guard !span.isEmpty else { return .zero }
        let thickness = Self.restingThickness + (Self.widenedThickness - Self.restingThickness) * expansion
        let inset = Self.edgeInset
        if vertical {
            return NSRect(x: bounds.maxX - inset - thickness, y: span.minY, width: thickness, height: max(thickness, span.height))
        }
        return NSRect(x: span.minX, y: bounds.maxY - inset - thickness, width: max(thickness, span.width), height: thickness)
    }

    /// `textMuted` softened toward the background at rest and at full
    /// strength when widened. Increase Contrast moves both a step stronger.
    var knobColor: NSColor {
        let active = hovering || dragging
        let color: Color = switch (increasesContrast(), active) {
        case (false, false): theme.muted.mix(with: theme.surface, by: 0.45)
        case (false, true), (true, false): theme.muted
        case (true, true): theme.text
        }
        return NSColor(color)
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let hoverArea { removeTrackingArea(hoverArea) }
        let area = NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .activeInActiveApp, .inVisibleRect],
                                  owner: self)
        addTrackingArea(area)
        hoverArea = area
    }

    override func mouseEntered(with event: NSEvent) {
        super.mouseEntered(with: event)
        hovering = true
        widen()
    }

    override func mouseExited(with event: NSEvent) {
        super.mouseExited(with: event)
        hovering = false
        widen()
    }

    override func mouseDown(with event: NSEvent) {
        dragging = true
        widen()
        // The system tracks the drag until the button is released.
        super.mouseDown(with: event)
        dragging = false
        widen()
    }

    private func widen() {
        let target: CGFloat = legacy && (hovering || dragging) ? 1 : 0
        updateKnob()
        guard target != expansion else { return }
        if NSWorkspace.shared.accessibilityDisplayShouldReduceMotion {
            expansion = target
            return
        }
        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.12
            context.timingFunction = CAMediaTimingFunction(name: .easeOut)
            animator().expansion = target
        }
    }

    /// A pill filling its bounds.
    private final class KnobView: NSView {
        var color = NSColor.clear {
            didSet { if color != oldValue { needsDisplay = true } }
        }
        override var isOpaque: Bool { false }
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
        override func setFrameSize(_ newSize: NSSize) {
            super.setFrameSize(newSize)
            needsDisplay = true
        }
        override func draw(_ dirtyRect: NSRect) {
            color.setFill()
            let radius = min(bounds.width, bounds.height) / 2
            NSBezierPath(roundedRect: bounds, xRadius: radius, yRadius: radius).fill()
        }
    }
}

extension NSScrollView {
    /// Gives the scroll view Kit's scrollers in `theme`, keeping its
    /// scroller style and which scrollers it shows.
    @MainActor func useKitScrollers(_ theme: MicaTheme) {
        var replaced = false
        if let scroller = verticalScroller as? KitScroller {
            scroller.theme = theme
        } else {
            verticalScroller = KitScroller.replacing(verticalScroller, in: self, theme: theme)
            replaced = true
        }
        if let scroller = horizontalScroller as? KitScroller {
            scroller.theme = theme
        } else {
            horizontalScroller = KitScroller.replacing(horizontalScroller, in: self, theme: theme)
            replaced = true
        }
        // A new scroller starts disabled with no knob until the scroll view
        // next reflects its position, which may not be until it scrolls.
        if replaced { reflectScrolledClipView(contentView) }
    }
}

extension KitScroller {
    fileprivate static func replacing(_ scroller: NSScroller?, in scroll: NSScrollView, theme: MicaTheme) -> KitScroller {
        let replacement = KitScroller(frame: scroller?.frame ?? NSRect(x: 0, y: 0, width: 15, height: 15))
        replacement.theme = theme
        replacement.controlSize = scroller?.controlSize ?? .regular
        replacement.scrollerStyle = scroll.scrollerStyle
        return replacement
    }
}

/// A SwiftUI scroll view with Kit's scrollers in the environment's theme.
/// Use it in place of `ScrollView` wherever its scroll bars can show.
struct KitScrollView<Content: View>: View {
    @Environment(\.mica) private var theme
    var axes: Axis.Set
    var content: Content

    init(_ axes: Axis.Set = .vertical, @ViewBuilder content: () -> Content) {
        self.axes = axes
        self.content = content()
    }

    var body: some View {
        ScrollView(axes) { content }
            .background(KitScrollerInstaller(theme: theme).allowsHitTesting(false).accessibilityHidden(true))
    }
}

/// Finds the scroll view it sits behind and gives it Kit's scrollers.
private struct KitScrollerInstaller: NSViewRepresentable {
    let theme: MicaTheme
    func makeNSView(context: Context) -> KitScrollerInstallerView { KitScrollerInstallerView() }
    func updateNSView(_ view: KitScrollerInstallerView, context: Context) { view.theme = theme }
}

@MainActor final class KitScrollerInstallerView: NSView {
    var theme = MicaTheme(dark: false) {
        didSet { if theme != oldValue || target == nil { install() } }
    }
    private(set) weak var target: NSScrollView?

    override func hitTest(_ point: NSPoint) -> NSView? { nil }
    override func viewDidMoveToSuperview() {
        super.viewDidMoveToSuperview()
        target = nil
        retries = 0
    }
    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        install()
    }
    override func layout() {
        super.layout()
        if let target, matches(target) { return }
        install()
    }

    private var retries = 0

    private func install() {
        if let target, target.window === window, matches(target) {
            target.useKitScrollers(theme)
            return
        }
        target = sibling()
        if let target {
            retries = 0
            target.useKitScrollers(theme)
        } else if window != nil, retries < 5 {
            // SwiftUI may add the scroll view after its background.
            retries += 1
            DispatchQueue.main.async { [weak self] in
                guard let self, self.target == nil else { return }
                self.install()
            }
        }
    }

    /// Whether `scroll` covers the same rectangle in the window as this view.
    private func matches(_ scroll: NSScrollView) -> Bool {
        guard window != nil, scroll.window === window else { return false }
        let frame = convert(bounds, to: nil), other = scroll.convert(scroll.bounds, to: nil)
        return frame.width > 0 && frame.height > 0
            && abs(other.minX - frame.minX) < 0.5 && abs(other.minY - frame.minY) < 0.5
            && abs(other.width - frame.width) < 0.5 && abs(other.height - frame.height) < 0.5
    }

    /// The scroll view this view is the background of. SwiftUI hosts a
    /// background just before the view it backs, in sibling containers, so
    /// the nearest following sibling that covers the same rectangle is it,
    /// even where other panes' scroll views lie at the same place.
    private func sibling() -> NSScrollView? {
        guard window != nil else { return nil }
        func search(_ view: NSView, depth: Int) -> NSScrollView? {
            if let scroll = view as? NSScrollView { return matches(scroll) ? scroll : nil }
            guard depth > 0 else { return nil }
            for child in view.subviews {
                if let found = search(child, depth: depth - 1) { return found }
            }
            return nil
        }
        var branch: NSView = self
        for _ in 0..<3 {
            guard let parent = branch.superview else { return nil }
            let siblings = parent.subviews
            guard let index = siblings.firstIndex(where: { $0 === branch }) else { return nil }
            for sibling in siblings[(index + 1)...] {
                if let found = search(sibling, depth: 2) { return found }
            }
            branch = parent
        }
        return nil
    }
}

/// Gives a CodeEdit source editor's scroll view Kit's scrollers once its
/// view loads.
@MainActor final class KitScrollerCoordinator: @preconcurrency TextViewCoordinator {
    private weak var controller: TextViewController?
    var theme: MicaTheme {
        didSet { install() }
    }

    init(theme: MicaTheme) { self.theme = theme }

    func prepareCoordinator(controller: TextViewController) {
        self.controller = controller
        install()
    }

    func controllerDidAppear(controller: TextViewController) {
        self.controller = controller
        install()
    }

    private func install() {
        guard let controller, controller.isViewLoaded, let scroll = controller.scrollView else { return }
        scroll.useKitScrollers(theme)
    }
}
