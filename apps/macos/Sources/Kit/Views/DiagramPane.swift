import AppKit
import KitMermaid
import KitMermaidScene
import KitTranscript
import SwiftUI
import UniformTypeIdentifiers

/// A transcript's Mermaid diagram on its own: fitted to its pane, zoomable,
/// and pannable, with Copy and Save As PNG in the header. It's drawn from
/// the same scene as the transcript's card, again at each zoom it settles
/// on, so it stays sharp.
struct DiagramPane: View {
    @Environment(\.mica) private var theme
    @Environment(\.displayScale) private var displayScale
    let workspace: WorkspaceState
    let pane: WorkspacePane
    let source: String
    @State private var zoom = ImageZoom()
    @State private var image: CGImage?
    @State private var error: String?

    /// Zoom shortcuts apply only to the diagram in the focused tile.
    private var focused: Bool { workspace.selected == pane && workspace.isVisible(pane) }

    var body: some View {
        let diagram = MermaidDiagram(source: source)
        VStack(spacing: 0) {
            switch diagram.drawing {
            case .success(let drawing):
                let picture = DiagramPicture(diagram: diagram, drawing: drawing, theme: theme, displayScale: displayScale)
                header(picture)
                Rule()
                if let error {
                    Text(error).font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
                    Rule()
                }
                ZStack {
                    theme.raised
                    if let image {
                        ImageCanvas(image: image, size: picture.size, zoom: zoom, checker: (theme.raised, theme.raised),
                                    smoothsEnlarged: true)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Mermaid diagram")
                .accessibilityValue(source)
                .task(id: Render(picture: picture, scale: picture.renderScale(zoom: zoom.scale))) {
                    image = picture.image(scale: picture.renderScale(zoom: zoom.scale))
                }
            case .failure(let notDrawn):
                Text("Diagram not drawn: \(notDrawn.reason)")
                    .font(.kit(size: 12)).italic().foregroundStyle(theme.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background { if focused { ZoomShortcuts(zoom: zoom) } }
    }

    private struct Render: Equatable {
        let picture: DiagramPicture
        let scale: CGFloat
    }

    private func header(_ picture: DiagramPicture) -> some View {
        HStack(spacing: 12) {
            Text(picture.description).font(.kit(size: 11, design: .monospaced)).lineLimit(1).truncationMode(.middle)
            Spacer(minLength: 8)
            ZoomControls(zoom: zoom)
            Button { copy(picture) } label: { IconView(.copy, size: 12) }
                .buttonStyle(.plain).foregroundStyle(theme.muted)
                .help("Copy image").accessibilityLabel("Copy image")
            Button("Save As…") { save(picture) }.buttonStyle(.plain).font(.kit(size: 11))
        }
        .foregroundStyle(theme.text)
        .padding(.horizontal, 20).frame(height: 38)
    }

    private func copy(_ picture: DiagramPicture) {
        guard let image = picture.image(scale: picture.exportScale, plate: true) else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.writeObjects([NSImage(cgImage: image, size: picture.plateSize)])
    }

    private func save(_ picture: DiagramPicture) {
        guard let image = picture.image(scale: picture.exportScale, plate: true),
              let data = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]) else {
            error = "Couldn’t draw the diagram as an image."; return
        }
        let panel = NSSavePanel()
        panel.nameFieldStringValue = "diagram.png"
        panel.allowedContentTypes = [.png]
        panel.begin { response in
            guard response == .OK, let url = panel.url else { return }
            do { try data.write(to: url, options: .atomic) }
            catch { self.error = "Couldn’t save the image: " + error.localizedDescription }
        }
    }
}

/// A drawn diagram in a theme, and the images made from it.
@MainActor
struct DiagramPicture: Equatable {
    let diagram: MermaidDiagram
    let drawing: KitMermaid.Canvas
    let theme: MicaTheme
    /// The screen's scale, which the scene's lines are snapped to.
    let displayScale: CGFloat
    /// The diagram's size at 100%, in points.
    let size: CGSize

    /// The margin around an exported diagram, in points.
    static let plateMargin: CGFloat = 16
    /// The largest image drawn for the pane, by side and by pixel count.
    static let maximumSide: CGFloat = 8192
    static let maximumPixels: CGFloat = 16_000_000

    init(diagram: MermaidDiagram, drawing: KitMermaid.Canvas, theme: MicaTheme, displayScale: CGFloat) {
        self.diagram = diagram; self.drawing = drawing; self.theme = theme; self.displayScale = displayScale
        let metrics = MermaidPicture.metrics(for: Typography.shared.font(size: 12, mono: true), scale: displayScale)
        size = MermaidSceneCache.shared.scene(for: diagram.source, drawing: drawing, metrics: metrics).size
    }

    /// The header's description: the kind of diagram and its size.
    var description: String {
        let kind = switch drawing.kind {
        case .flowchart: "flowchart"
        case .sequence: "sequence diagram"
        }
        return "\(kind) · \(Int(size.width.rounded())) × \(Int(size.height.rounded()))"
    }

    /// An exported image's size in points, margin included.
    var plateSize: CGSize {
        CGSize(width: size.width + Self.plateMargin * 2, height: size.height + Self.plateMargin * 2)
    }

    /// Copies and saved images are drawn at twice their size, or the
    /// screen's scale where that's more.
    var exportScale: CGFloat { max(2, displayScale) }

    /// The pixels per point to draw the pane's image at for a zoom: enough
    /// for the screen at that zoom, in whole steps so small zoom changes
    /// reuse the image, and never more than the image limits allow.
    func renderScale(zoom: CGFloat) -> CGFloat {
        let wanted = max(displayScale, (zoom * displayScale).rounded(.up))
        guard size.width > 0, size.height > 0 else { return wanted }
        let limit = min(Self.maximumSide / max(size.width, size.height),
                        (Self.maximumPixels / (size.width * size.height)).squareRoot())
        return max(1, min(wanted, limit.rounded(.down)))
    }

    /// The diagram drawn at `scale` pixels per point; on a plate of the
    /// card's color, with a margin, when it leaves the app.
    func image(scale: CGFloat, plate: Bool = false) -> CGImage? {
        let content = MermaidPicture(diagram: diagram, drawing: drawing)
            .padding(plate ? Self.plateMargin : 0)
            .background(plate ? theme.raised : .clear)
            .environment(\.mica, theme)
            .environment(\.displayScale, displayScale)
            .environment(\.colorScheme, theme.dark ? .dark : .light)
        let renderer = ImageRenderer(content: content)
        renderer.scale = scale
        return renderer.cgImage
    }

    nonisolated static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.diagram == rhs.diagram && lhs.theme == rhs.theme && lhs.displayScale == rhs.displayScale
    }
}
