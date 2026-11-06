import AppKit
import KitMermaid
import KitMermaidScene
import KitTranscript
import SwiftUI

/// A drawn Mermaid diagram, in the transcript's monospaced font and the
/// theme's colors for each role.
struct MermaidPicture: View {
    @Environment(\.mica) private var theme
    @Environment(\.displayScale) private var displayScale
    let diagram: MermaidDiagram
    let drawing: KitMermaid.Canvas
    var fontSize: CGFloat = 12

    var body: some View {
        // The layout's cells are as wide as the transcript's code font, so
        // text in the interface font fits wherever the terminal's does.
        let metrics = Self.metrics(for: Typography.shared.font(size: fontSize, mono: true), scale: displayScale)
        let scene = MermaidSceneCache.shared.scene(for: diagram.source, drawing: drawing, metrics: metrics)
        let colors = Dictionary(uniqueKeysWithValues: MermaidScene.Ink.allCases.map { ($0, color($0)) })
        let fonts = Fonts(size: fontSize)
        VStack(spacing: 0) {
            ForEach(scene.bands.indices, id: \.self) { index in
                let band = scene.bands[index]
                SwiftUI.Canvas { context, _ in
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
    }

    private struct Fonts {
        let node: Font, name: Font, small: Font, tag: Font, title: Font

        @MainActor init(size: CGFloat) {
            func font(_ size: CGFloat, _ weight: NSFont.Weight = .regular) -> Font {
                Font(Typography.shared.font(size: size, weight: weight))
            }
            node = font(size)
            name = font(size, .semibold)
            small = font(size - 1)
            tag = font(size - 3, .bold)
            title = font(size + 1, .semibold)
        }
    }

    private func draw(_ element: MermaidScene.Element, in context: inout GraphicsContext,
                      colors: [MermaidScene.Ink: Color], fonts: Fonts) {
        func ink(_ value: MermaidScene.Ink) -> Color { colors[value, default: theme.text] }
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
                        layer.addFilter(.shadow(color: .black.opacity(theme.dark ? 0.35 : 0.07), radius: 2, y: 1))
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

    /// The theme color of each ink. Lines are quieter than text, and fills
    /// stay close to the card so the diagram reads as part of it.
    private func color(_ ink: MermaidScene.Ink) -> Color {
        switch ink {
        case .line: theme.muted.mix(with: theme.border, by: 0.2)
        case .lifeline: theme.border.mix(with: theme.muted, by: 0.25)
        case .nodeFill: theme.dark ? theme.hover : theme.surface
        case .nodeStroke: theme.border.mix(with: theme.muted, by: 0.35)
        case .noteFill: theme.raised.mix(with: theme.accent, by: 0.08)
        case .noteStroke: theme.border.mix(with: theme.accent, by: 0.35)
        case .blockStroke: theme.border.mix(with: theme.muted, by: 0.15)
        case .tagFill: theme.raised.mix(with: theme.accent, by: 0.14)
        case .accent: theme.accent
        case .text: theme.text
        case .secondary: theme.muted
        case .card: theme.raised
        }
    }

    /// One cell is the font's advance wide and its line tall.
    static func metrics(for font: NSFont, scale: CGFloat) -> MermaidScene.Metrics {
        let advance = ("M" as NSString).size(withAttributes: [.font: font]).width
        let height = NSLayoutManager().defaultLineHeight(for: font)
        return MermaidScene.Metrics(cell: CGSize(width: advance, height: ceil(height)),
                                    line: max(1, (font.pointSize / 12).rounded()), scale: max(scale, 1))
    }
}

/// A fenced Mermaid diagram in Markdown: drawn when it can be, otherwise its
/// source with the reason it wasn't drawn.
struct MermaidBlock: View {
    @Environment(\.mica) private var theme
    @Environment(\.compactCodeBlocks) private var compact
    @Environment(\.openMermaidDiagram) private var openDiagram
    let diagram: MermaidDiagram

    var body: some View {
        switch diagram.drawing {
        case .success(let drawing):
            if compact {
                ScrollView(.horizontal) { MermaidPicture(diagram: diagram, drawing: drawing) }
                    .compactFence()
            } else {
                FenceCard(label: "MERMAID", copy: diagram.source,
                          open: openDiagram.map { open in { [source = diagram.source] in open(source) } }) {
                    ScrollView(.horizontal) {
                        MermaidPicture(diagram: diagram, drawing: drawing).padding(14).frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        case .failure(let notDrawn):
            VStack(alignment: .leading, spacing: 6) {
                CodeBlock(language: "mermaid", source: diagram.source)
                Text("Diagram not drawn: \(notDrawn.reason)")
                    .font(.kit(size: 12)).italic().foregroundStyle(theme.muted)
            }
        }
    }
}
