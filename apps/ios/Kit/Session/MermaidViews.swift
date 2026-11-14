import KitMermaid
import KitMermaidScene
import KitTranscript
import SwiftUI
import UIKit

/// A fenced Mermaid diagram: drawn at full size in the fence card, where it
/// scrolls sideways when it's wider, with ⤢ to open it alone. A diagram
/// that can't be drawn shows its source with the reason.
struct MermaidBlock: View {
    let diagram: MermaidDiagram
    @State private var opened = false
    @Environment(\.diagramAnnotations) private var annotations

    var body: some View {
        switch diagram.drawing {
        case .success(let drawing):
            FenceCard(label: "mermaid", copy: diagram.source) {
                Button { opened = true } label: {
                    Image(systemName: "arrow.up.left.and.arrow.down.right")
                        .font(.system(size: 13, weight: .medium))
                        .foregroundStyle(Theme.muted)
                        .frame(width: 32, height: 24)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Open diagram")
                .accessibilityIdentifier("mermaid-open")
            } content: {
                ScrollView(.horizontal) {
                    MermaidPicture(diagram: diagram, drawing: drawing)
                        .padding(.horizontal, 14)
                        .padding(.top, 4)
                        .padding(.bottom, 8)
                }
                .scrollIndicators(.hidden)
            }
            .fullScreenCover(isPresented: $opened) {
                MermaidScreen(diagram: diagram, drawing: drawing, annotations: annotations)
            }
        case .failure(let notDrawn):
            VStack(alignment: .leading, spacing: 6) {
                CodeBlock(language: "mermaid", source: diagram.source)
                Text("Diagram not drawn: \(notDrawn.reason)")
                    .font(.system(size: 13)).italic().foregroundStyle(Theme.muted)
            }
        }
    }
}

extension EnvironmentValues {
    /// The session's diagram comments, where a diagram can be annotated.
    @Entry var diagramAnnotations: DiagramAnnotationDraft? = nil
}

/// A diagram alone, to drag around and pinch. It opens fitted to the
/// screen's width, or at full size when that's smaller. With the session's
/// diagram comments, Annotate turns taps into picking a part to comment on;
/// parts with a waiting comment carry a mark.
struct MermaidScreen: View {
    let diagram: MermaidDiagram
    let drawing: KitMermaid.Canvas
    var annotations: DiagramAnnotationDraft? = nil
    @State var annotator = DiagramAnnotator()
    @Environment(\.dismiss) private var dismiss
    @Environment(\.displayScale) private var displayScale
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                MermaidZoomingView(image: MermaidImage(diagram: diagram, drawing: drawing, scheme: scheme,
                                                       displayScale: displayScale),
                                   marks: MermaidMarks(annotating: annotator.annotating, hovered: annotator.hovered,
                                                       picked: annotator.selected,
                                                       marked: annotations?.parts(of: diagram.source) ?? []),
                                   pick: { annotator.click($0) }, hover: { annotator.hover($0) })
                    .background(Theme.raised)
                    .ignoresSafeArea(edges: annotator.selected == nil ? .bottom : [])
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("Mermaid diagram")
                    .accessibilityValue(diagram.source)
                    .accessibilityIdentifier("mermaid-diagram")
                if let annotations, let picked = annotator.selected {
                    DiagramCommentEditor(annotator: annotator,
                                         summary: (try? DiagramAnnotationText.summary(diagram.source, picked)) ?? "",
                                         error: annotations.error) {
                        annotator.commit(to: annotations, source: diagram.source)
                    }
                    .id(picked)
                }
            }
            .navigationTitle("Diagram")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if annotations != nil {
                    ToolbarItem(placement: .topBarLeading) {
                        Toggle(isOn: Binding(get: { annotator.annotating }, set: { $0 ? annotator.start() : annotator.stop() })) {
                            Label("Annotate", systemImage: "text.bubble")
                        }
                        .toggleStyle(.button)
                        .accessibilityIdentifier("mermaid-annotate")
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { dismiss() }
                        .accessibilityIdentifier("mermaid-done")
                }
            }
        }
        .accessibilityIdentifier("mermaid-screen")
    }
}

/// What Annotate mode shows over a diagram: the part a pointer is over,
/// the part picked, and the parts with a waiting comment.
struct MermaidMarks: Equatable {
    var annotating = false
    var hovered: Part?
    var picked: Part?
    var marked: [Part] = []
}

/// A diagram drawn as an image at a given resolution: its scene with a
/// margin around it, in the theme's colors.
@MainActor
struct MermaidImage: Equatable {
    let diagram: MermaidDiagram
    let drawing: KitMermaid.Canvas
    let scheme: ColorScheme
    let displayScale: CGFloat

    /// The space around the diagram, in points.
    static let margin: CGFloat = 20
    /// The largest image drawn, by side and by pixel count.
    static let maximumSide: CGFloat = 8192
    static let maximumPixels: CGFloat = 16_000_000

    /// The size of the scene's cells and lines, as MermaidPicture draws it.
    var metrics: MermaidScene.Metrics {
        MermaidPicture.metrics(for: UIFont.monospacedSystemFont(ofSize: 13, weight: .regular), scale: displayScale)
    }

    /// The image's size in points: the scene's and its margin.
    var size: CGSize {
        let scene = MermaidSceneCache.shared.scene(for: diagram.source, drawing: drawing, metrics: metrics).size
        return CGSize(width: scene.width + Self.margin * 2, height: scene.height + Self.margin * 2)
    }

    /// The part drawn under a point in the image, if any.
    func part(at point: CGPoint) -> Part? {
        MermaidHighlight.part(at: CGPoint(x: point.x - Self.margin, y: point.y - Self.margin), in: drawing,
                              metrics: metrics)
    }

    /// Where a part is drawn, in the scene's points.
    func highlight(_ part: Part) -> MermaidHighlight {
        MermaidHighlight(part, in: drawing, metrics: metrics)
    }

    /// The pixels per point for a zoom: enough for the screen at that
    /// zoom, in whole steps, and no more than the image limits allow.
    func scale(zoom: CGFloat) -> CGFloat {
        let wanted = max(displayScale, (zoom * displayScale).rounded(.up))
        let size = size
        guard size.width > 0, size.height > 0 else { return wanted }
        let limit = min(Self.maximumSide / max(size.width, size.height),
                        (Self.maximumPixels / (size.width * size.height)).squareRoot())
        return max(1, min(wanted, limit.rounded(.down)))
    }

    func render(scale: CGFloat) -> UIImage? {
        let renderer = ImageRenderer(content: MermaidPicture(diagram: diagram, drawing: drawing)
            .padding(Self.margin)
            .environment(\.colorScheme, scheme)
            .environment(\.displayScale, displayScale))
        renderer.scale = scale
        renderer.isOpaque = false
        return renderer.uiImage
    }

    nonisolated static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.diagram == rhs.diagram && lhs.scheme == rhs.scheme && lhs.displayScale == rhs.displayScale
    }
}

/// A diagram's image in a scroll view that pans and pinches, from the
/// width that fits up to three times its size. The image is drawn again
/// at each zoom it settles on, so it stays sharp. It isn't a hosted
/// SwiftUI view under the zoom's transform: that one lays itself out at
/// its scaled frame and draws only part of a diagram wider than the screen.
/// In Annotate mode a tap picks the part under it, and a pointer outlines
/// the part it's over; the outlines ride the zoom.
struct MermaidZoomingView: UIViewRepresentable {
    let image: MermaidImage
    var marks = MermaidMarks()
    var pick: (Part?) -> Void = { _ in }
    var hover: (Part?) -> Void = { _ in }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIView(context: Context) -> MermaidScrollView {
        let scroll = MermaidScrollView()
        scroll.delegate = context.coordinator
        scroll.maximumZoomScale = 3
        scroll.showsHorizontalScrollIndicator = false
        scroll.showsVerticalScrollIndicator = false
        scroll.bouncesZoom = true
        scroll.contentInsetAdjustmentBehavior = .never
        let tap = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.tapped(_:)))
        tap.isEnabled = false
        scroll.addGestureRecognizer(tap)
        context.coordinator.tap = tap
        let pointer = UIHoverGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.hovered(_:)))
        pointer.isEnabled = false
        scroll.addGestureRecognizer(pointer)
        context.coordinator.pointer = pointer
        return scroll
    }

    func updateUIView(_ scroll: MermaidScrollView, context: Context) {
        let coordinator = context.coordinator
        coordinator.scroll = scroll
        coordinator.pick = pick
        coordinator.hover = hover
        coordinator.tap?.isEnabled = marks.annotating
        coordinator.pointer?.isEnabled = marks.annotating
        if coordinator.image != image {
            coordinator.image = image
            scroll.show(size: image.size)
            coordinator.redraw()
        }
        scroll.overlay.show(marks, in: image)
    }

    @MainActor final class Coordinator: NSObject, UIScrollViewDelegate {
        var image: MermaidImage?
        weak var scroll: MermaidScrollView?
        weak var tap: UITapGestureRecognizer?
        weak var pointer: UIHoverGestureRecognizer?
        var pick: (Part?) -> Void = { _ in }
        var hover: (Part?) -> Void = { _ in }
        /// The scale the shown image was drawn at.
        private(set) var drawnScale: CGFloat = 0

        /// The part under a point in the image's own points.
        func part(at point: CGPoint) -> Part? { image?.part(at: point) }

        @objc func tapped(_ gesture: UITapGestureRecognizer) {
            guard let view = scroll?.imageView, gesture.state == .ended else { return }
            tapped(at: gesture.location(in: view))
        }

        /// Picks the part under a tap at a point in the image's own points.
        func tapped(at point: CGPoint) { pick(part(at: point)) }

        @objc func hovered(_ gesture: UIHoverGestureRecognizer) {
            guard let view = scroll?.imageView else { return }
            switch gesture.state {
            case .began, .changed: hover(part(at: gesture.location(in: view)))
            default: hover(nil)
            }
        }

        func viewForZooming(in scroll: UIScrollView) -> UIView? { (scroll as? MermaidScrollView)?.imageView }

        func scrollViewDidZoom(_ scroll: UIScrollView) {
            (scroll as? MermaidScrollView)?.center()
        }

        func scrollViewDidEndZooming(_ scroll: UIScrollView, with view: UIView?, atScale scale: CGFloat) {
            redraw()
        }

        /// Draws the image for the current zoom, when that needs more pixels
        /// than the one shown, or it's the first.
        func redraw() {
            guard let image, let scroll else { return }
            let scale = image.scale(zoom: max(scroll.zoomScale, 1))
            guard scale != drawnScale, let drawn = image.render(scale: scale) else { return }
            drawnScale = scale
            scroll.imageView.image = drawn
        }
    }
}

/// A scroll view that shows an image at its size in points, fits its width
/// once laid out, and keeps an image smaller than itself in the middle.
final class MermaidScrollView: UIScrollView {
    let imageView = UIImageView()
    /// Annotate mode's outlines and marks, over the image in its points.
    let overlay = MermaidAnnotationOverlay()
    private var fitted = false

    func show(size: CGSize) {
        if imageView.superview == nil {
            imageView.contentMode = .scaleToFill
            addSubview(imageView)
            imageView.addSubview(overlay)
        }
        zoomScale = 1
        imageView.frame = CGRect(origin: .zero, size: size)
        overlay.frame = CGRect(origin: .zero, size: size)
        contentSize = size
        fitted = false
        setNeedsLayout()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let natural = imageView.bounds.size
        guard bounds.width > 0, bounds.height > 0, natural.width > 0 else { return }
        if !fitted {
            fitted = true
            // Opens at the width that fits, and zooms out until it all shows.
            let width = min(1, bounds.width / natural.width)
            minimumZoomScale = min(width, bounds.height / natural.height)
            zoomScale = width
        }
        center()
    }

    func center() {
        let x = max(0, (bounds.width - imageView.frame.width) / 2)
        let y = max(0, (bounds.height - imageView.frame.height) / 2)
        contentInset = UIEdgeInsets(top: y, left: x, bottom: y, right: x)
    }
}

/// A drawn Mermaid diagram from Kit's layout (ADR 0046), in the system
/// font and the theme's colors for each role. Its cells are as wide as the
/// monospaced font's, so text fits wherever the terminal's does.
struct MermaidPicture: View {
    @Environment(\.displayScale) private var displayScale
    @Environment(\.colorScheme) private var scheme
    let diagram: MermaidDiagram
    let drawing: KitMermaid.Canvas
    var fontSize: CGFloat = 13

    var body: some View {
        let metrics = Self.metrics(for: UIFont.monospacedSystemFont(ofSize: fontSize, weight: .regular), scale: displayScale)
        let scene = MermaidSceneCache.shared.scene(for: diagram.source, drawing: drawing, metrics: metrics)
        let colors = Dictionary(uniqueKeysWithValues: MermaidScene.Ink.allCases.map { ($0, color($0)) })
        let fonts = Fonts(size: fontSize)
        VStack(spacing: 0) {
            ForEach(scene.bands.indices, id: \.self) { index in
                let band = scene.bands[index]
                Canvas { context, _ in
                    context.translateBy(x: 0, y: -band.top)
                    for index in band.elements {
                        draw(scene.elements[index], in: &context, colors: colors, fonts: fonts)
                    }
                }
                .frame(width: scene.size.width, height: band.height)
            }
        }
        .frame(width: scene.size.width, height: scene.size.height)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Mermaid diagram")
        .accessibilityValue(diagram.source)
        .accessibilityIdentifier("mermaid-diagram")
    }

    private struct Fonts {
        let node: Font, name: Font, small: Font, tag: Font, title: Font

        init(size: CGFloat) {
            node = .system(size: size)
            name = .system(size: size, weight: .semibold)
            small = .system(size: size - 1)
            tag = .system(size: size - 3, weight: .bold)
            title = .system(size: size + 1, weight: .semibold)
        }
    }

    private func draw(_ element: MermaidScene.Element, in context: inout GraphicsContext,
                      colors: [MermaidScene.Ink: Color], fonts: Fonts) {
        func ink(_ value: MermaidScene.Ink) -> Color { colors[value, default: Theme.text] }
        switch element {
        case .stroke(let path, let value, let width, let dash, let phase):
            context.stroke(path, with: .color(ink(value)),
                           style: StrokeStyle(lineWidth: width, lineCap: .butt, lineJoin: .round, dash: dash,
                                              dashPhase: phase))
        case .fill(let path, let value):
            context.fill(path, with: .color(ink(value)))
        case .shape(let path, let fill, let stroke, let width, let dash, let lifted):
            if let fill {
                if lifted {
                    context.drawLayer { layer in
                        layer.addFilter(.shadow(color: .black.opacity(scheme == .dark ? 0.35 : 0.07), radius: 2, y: 1))
                        layer.fill(path, with: .color(ink(fill)))
                    }
                } else {
                    context.fill(path, with: .color(ink(fill)))
                }
            }
            context.stroke(path, with: .color(ink(stroke)),
                           style: StrokeStyle(lineWidth: width, lineJoin: .round, dash: dash))
        case .text(let text, let point, let style, let leading):
            let font: Font, color: Color
            switch style {
            case .node: (font, color) = (fonts.node, ink(.text))
            case .elsewhere: (font, color) = (fonts.node, ink(.secondary))
            case .name: (font, color) = (fonts.name, ink(.text))
            case .edgeLabel: (font, color) = (fonts.small, ink(.secondary))
            case .message: (font, color) = (fonts.node, ink(.text))
            case .note: (font, color) = (fonts.small, ink(.text))
            case .title: (font, color) = (fonts.title, ink(.text))
            }
            let resolved = context.resolve(Text(text).font(font).foregroundColor(color))
            if style == .message {
                // Clear the lifelines behind a message's text.
                let size = resolved.measure(in: CGSize(width: CGFloat.infinity, height: .infinity))
                context.fill(Path(CGRect(x: point.x - size.width / 2 - 3, y: point.y - size.height / 2,
                                         width: size.width + 6, height: size.height)), with: .color(ink(.card)))
            }
            if style == .edgeLabel {
                // Edge text sits on a pill over its line.
                let size = resolved.measure(in: CGSize(width: CGFloat.infinity, height: .infinity))
                let left = leading ? point.x - 6 : point.x - size.width / 2 - 6
                let pill = CGRect(x: left, y: point.y - size.height / 2 - 1.5,
                                  width: size.width + 12, height: size.height + 3)
                let shape = Path(roundedRect: pill, cornerRadius: pill.height / 2)
                context.fill(shape, with: .color(ink(.nodeFill)))
                context.stroke(shape, with: .color(ink(.blockStroke)), lineWidth: 1)
            }
            context.draw(resolved, at: point, anchor: leading ? .leading : .center)
        case .legend(let keyword, let condition, let point):
            // The keyword as a tag on the frame's line, then the condition,
            // both cleared from the line behind them.
            let tag = context.resolve(Text(keyword.uppercased()).font(fonts.tag).tracking(0.4)
                .foregroundColor(ink(.accent)))
            let tagSize = tag.measure(in: CGSize(width: CGFloat.infinity, height: .infinity))
            let pill = CGRect(x: point.x, y: point.y - tagSize.height / 2 - 2, width: tagSize.width + 10,
                              height: tagSize.height + 4)
            var end = pill.maxX
            var label: GraphicsContext.ResolvedText?
            if !condition.isEmpty {
                let text = context.resolve(Text(condition).font(fonts.small).foregroundColor(ink(.secondary)))
                end += 6 + text.measure(in: CGSize(width: CGFloat.infinity, height: .infinity)).width
                label = text
            }
            context.fill(Path(CGRect(x: pill.minX - 4, y: pill.minY, width: end - pill.minX + 8, height: pill.height)),
                         with: .color(ink(.card)))
            let tagShape = Path(roundedRect: pill, cornerRadius: 4, style: .continuous)
            context.fill(tagShape, with: .color(ink(.tagFill)))
            context.draw(tag, at: CGPoint(x: pill.midX, y: point.y), anchor: .center)
            if let label {
                context.draw(label, at: CGPoint(x: pill.maxX + 6, y: point.y), anchor: .leading)
            }
        }
    }

    /// The theme color of each ink, as on the Mac: lines are quieter than
    /// text, and fills stay close to the card so the diagram reads as part
    /// of it.
    private func color(_ ink: MermaidScene.Ink) -> Color {
        switch ink {
        case .line: Theme.muted.mix(with: Theme.border, by: 0.2)
        case .lifeline: Theme.border.mix(with: Theme.muted, by: 0.25)
        case .nodeFill: scheme == .dark ? Theme.hover : Theme.surface
        case .nodeStroke: Theme.border.mix(with: Theme.muted, by: 0.35)
        case .noteFill: Theme.raised.mix(with: Theme.accent, by: 0.08)
        case .noteStroke: Theme.border.mix(with: Theme.accent, by: 0.35)
        case .blockStroke: Theme.border.mix(with: Theme.muted, by: 0.15)
        case .tagFill: Theme.raised.mix(with: Theme.accent, by: 0.14)
        case .accent: Theme.accent
        case .text: Theme.text
        case .secondary: Theme.muted
        case .card: Theme.raised
        }
    }

    /// One cell is the font's advance wide and its line tall.
    static func metrics(for font: UIFont, scale: CGFloat) -> MermaidScene.Metrics {
        let advance = ("M" as NSString).size(withAttributes: [.font: font]).width
        return MermaidScene.Metrics(cell: CGSize(width: advance, height: ceil(font.lineHeight)),
                                    line: max(1, (font.pointSize / 12).rounded()), scale: max(scale, 1))
    }
}
