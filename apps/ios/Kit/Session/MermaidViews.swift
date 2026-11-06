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
                MermaidScreen(diagram: diagram, drawing: drawing)
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

/// A diagram alone, to drag around and pinch. It opens fitted to the
/// screen's width, or at full size when that's smaller.
private struct MermaidScreen: View {
    let diagram: MermaidDiagram
    let drawing: KitMermaid.Canvas
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ZoomingView { MermaidPicture(diagram: diagram, drawing: drawing).padding(20) }
                .background(Theme.raised)
                .ignoresSafeArea(edges: .bottom)
                .navigationTitle("Diagram")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("Done") { dismiss() }
                            .accessibilityIdentifier("mermaid-done")
                    }
                }
        }
        .accessibilityIdentifier("mermaid-screen")
    }
}

/// SwiftUI content in a scroll view that pans and pinches, from the width
/// that fits up to three times its size.
private struct ZoomingView<Content: View>: UIViewRepresentable {
    @ViewBuilder var content: () -> Content

    func makeCoordinator() -> Coordinator { Coordinator(UIHostingController(rootView: content())) }

    func makeUIView(context: Context) -> UIScrollView {
        let scroll = FittingScrollView()
        scroll.delegate = context.coordinator
        scroll.maximumZoomScale = 3
        scroll.showsHorizontalScrollIndicator = false
        scroll.showsVerticalScrollIndicator = false
        scroll.bouncesZoom = true
        let hosted = context.coordinator.host.view!
        hosted.backgroundColor = .clear
        let size = context.coordinator.host.sizeThatFits(in: CGSize(width: CGFloat.infinity, height: .infinity))
        hosted.frame = CGRect(origin: .zero, size: size)
        scroll.addSubview(hosted)
        scroll.contentSize = size
        scroll.content = hosted
        return scroll
    }

    func updateUIView(_ scroll: UIScrollView, context: Context) {
        context.coordinator.host.rootView = content()
    }

    final class Coordinator: NSObject, UIScrollViewDelegate {
        let host: UIHostingController<Content>
        init(_ host: UIHostingController<Content>) { self.host = host }

        func viewForZooming(in scroll: UIScrollView) -> UIView? { host.view }

        func scrollViewDidZoom(_ scroll: UIScrollView) {
            (scroll as? FittingScrollView)?.center()
        }
    }
}

/// A scroll view that fits its content's width once laid out and keeps
/// content smaller than itself in the middle.
private final class FittingScrollView: UIScrollView {
    weak var content: UIView?
    private var fitted = false

    override func layoutSubviews() {
        super.layoutSubviews()
        guard let content, bounds.width > 0 else { return }
        if !fitted {
            fitted = true
            let natural = content.bounds.size
            let fit = min(1, bounds.width / max(natural.width, 1))
            minimumZoomScale = fit
            zoomScale = fit
        }
        center()
    }

    func center() {
        guard let content else { return }
        let x = max(0, (bounds.width - content.frame.width) / 2)
        let y = max(0, (bounds.height - content.frame.height) / 2)
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
