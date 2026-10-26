import AppKit
import ImageIO
import SwiftUI

/// A transcript image at full size, loaded from its attachment and shown in
/// an `ImageViewer`.
struct ImagePane: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    let id: String
    let filename: String
    @State private var image: LoadedImage?
    @State private var error: String?
    @State private var attempt = 0

    private var pane: WorkspacePane { .image(id: id, filename: filename) }

    var body: some View {
        Group {
            if let image {
                ImageViewer(workspace: state.ui.workspace, pane: pane, image: image, title: filename)
            } else {
                VStack(spacing: 0) {
                    HStack { Text(filename).font(.kit(size: 11, design: .monospaced)).lineLimit(1).truncationMode(.middle); Spacer() }
                        .foregroundStyle(theme.text).padding(.horizontal, 20).frame(height: 38)
                    Rule()
                    if let error {
                        VStack(spacing: 10) {
                            Text(error).foregroundStyle(theme.muted)
                            Button("Retry") { attempt += 1 }.buttonStyle(FormButtonStyle())
                        }
                        .font(.kit(size: 12))
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                    } else {
                        KitSpinner("Loading image…").font(.kit(size: 12)).foregroundStyle(theme.muted)
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                    }
                }
            }
        }
        .task(id: Request(server: state.serverID, session: state.selectedID, attempt: attempt)) { await load() }
    }

    private struct Request: Equatable { let server: String; let session: String; let attempt: Int }

    private func load() async {
        error = nil
        guard let client = state.attachmentClient else { error = "Image unavailable"; return }
        do {
            let resolved = try await client.resolveAttachments(state.selectedID, ids: [id])
            guard let info = resolved.attachments?.first(where: { $0.id == id }) else { error = "Image unavailable"; return }
            let data = try await client.attachmentContent(state.selectedID, info: info)
            try Task.checkCancellation()
            guard let loaded = await Task.detached(priority: .userInitiated, operation: { LoadedImage(data: data) }).value else {
                error = "This image can’t be shown."; return
            }
            image = loaded
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// A decoded image fitted to its pane, zoomable, and pannable, with its
/// size, Copy, and Save As in the header. `leading` labels where the image
/// came from, such as a file's machine.
struct ImageViewer<Leading: View>: View {
    @Environment(\.mica) private var theme
    let workspace: WorkspaceState
    let pane: WorkspacePane
    let image: LoadedImage
    let title: String
    @ViewBuilder var leading: () -> Leading
    @State private var zoom = ImageZoom()
    @State private var error: String?

    init(workspace: WorkspaceState, pane: WorkspacePane, image: LoadedImage, title: String,
         @ViewBuilder leading: @escaping () -> Leading = { EmptyView() }) {
        self.workspace = workspace; self.pane = pane; self.image = image; self.title = title; self.leading = leading
    }

    /// Zoom shortcuts apply only to the image in the focused tile.
    private var focused: Bool { workspace.selected == pane && workspace.isVisible(pane) }

    var body: some View {
        VStack(spacing: 0) {
            header
            Rule()
            if let error {
                Text(error).font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
                Rule()
            }
            ImageCanvas(image: image.cgImage, zoom: zoom, checker: (theme.raised, theme.surface))
                .accessibilityLabel("Image: " + title)
        }
        .background { if focused { shortcuts } }
    }

    private var header: some View {
        HStack(spacing: 12) {
            leading()
            Text(title).font(.kit(size: 11, design: .monospaced)).lineLimit(1).truncationMode(.middle)
            Text("\(image.width) × \(image.height) · " + ByteCountFormatter.string(fromByteCount: Int64(image.data.count), countStyle: .file))
                .font(.kit(size: 11)).foregroundStyle(theme.muted).lineLimit(1)
            Spacer(minLength: 8)
            Text("\(Int((zoom.scale * 100).rounded()))%").font(.kit(size: 11).monospacedDigit())
                .foregroundStyle(theme.muted)
                .accessibilityLabel("Zoom \(Int((zoom.scale * 100).rounded())) percent")
            HStack(spacing: 2) {
                ZoomModeButton(title: "Fit", selected: zoom.fitted) { zoom.fit() }
                    .help("Zoom to fit (⌘9)")
                ZoomModeButton(title: "100%", selected: !zoom.fitted && zoom.scale == 1) { zoom.actualSize() }
                    .help("Actual size (⌘0)")
            }
            Button(action: copy) { IconView(.copy, size: 12) }
                .buttonStyle(.plain).foregroundStyle(theme.muted)
                .help("Copy image").accessibilityLabel("Copy image")
            Button("Save As…", action: save).buttonStyle(.plain).font(.kit(size: 11))
        }
        .foregroundStyle(theme.text)
        .padding(.horizontal, 20).frame(height: 38)
    }

    /// Preview's zoom keys, while this image's tile has focus.
    private var shortcuts: some View {
        Group {
            Button("") { zoom.zoomIn() }.keyboardShortcut("=", modifiers: .command)
            Button("") { zoom.zoomIn() }.keyboardShortcut("+", modifiers: .command)
            Button("") { zoom.zoomOut() }.keyboardShortcut("-", modifiers: .command)
            Button("") { zoom.actualSize() }.keyboardShortcut("0", modifiers: .command)
            Button("") { zoom.fit() }.keyboardShortcut("9", modifiers: .command)
        }
        .opacity(0).frame(width: 0, height: 0).accessibilityHidden(true)
    }

    private func copy() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.writeObjects([NSImage(cgImage: image.cgImage, size: .zero)])
    }

    private func save() {
        let data = image.data
        let panel = NSSavePanel()
        panel.nameFieldStringValue = URL(fileURLWithPath: title).lastPathComponent
        panel.begin { response in
            guard response == .OK, let url = panel.url else { return }
            do { try data.write(to: url, options: .atomic) }
            catch { self.error = "Couldn’t save the image: " + error.localizedDescription }
        }
    }
}

/// A decoded image and the bytes it came from.
struct LoadedImage: @unchecked Sendable {
    let data: Data
    let cgImage: CGImage
    /// The image's own size in pixels, before any downsampling.
    let width: Int
    let height: Int

    /// Images larger than this on a side are downsampled for display.
    static let maximumSide = 8192

    init?(data: Data) {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil),
              let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
              var width = properties[kCGImagePropertyPixelWidth] as? Int,
              var height = properties[kCGImagePropertyPixelHeight] as? Int else { return nil }
        // EXIF orientations 5–8 swap the sides once the transform is applied.
        if let orientation = properties[kCGImagePropertyOrientation] as? Int, orientation >= 5 { swap(&width, &height) }
        guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceThumbnailMaxPixelSize: min(max(width, height), Self.maximumSide),
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
        ] as CFDictionary) else { return nil }
        self.data = data; cgImage = image; self.width = width; self.height = height
    }
}

/// The viewer's zoom: fitted to the pane, or a fixed scale where 100% shows
/// one image pixel per point.
@MainActor @Observable
final class ImageZoom {
    static let range: ClosedRange<CGFloat> = 0.05...16
    static let steps: [CGFloat] = [0.05, 0.1, 0.25, 0.33, 0.5, 0.67, 0.75, 1, 1.25, 1.5, 2, 3, 4, 6, 8, 12, 16]

    /// Whether the image follows the pane's size.
    private(set) var fitted = true
    /// The current scale, which the canvas reports while fitted.
    var scale: CGFloat = 1
    /// Bumped when a command asks the canvas to apply `scale`.
    private(set) var request = 0

    func fit() { fitted = true; request += 1 }
    func actualSize() { set(1) }
    func zoomIn() { set(Self.steps.first { $0 > scale + 0.001 } ?? Self.range.upperBound) }
    func zoomOut() { set(Self.steps.last { $0 < scale - 0.001 } ?? Self.range.lowerBound) }

    /// A pinch or scroll zoom the canvas already applied.
    func pinched(to value: CGFloat) { fitted = false; scale = value }

    private func set(_ value: CGFloat) {
        fitted = false
        scale = min(max(value, Self.range.lowerBound), Self.range.upperBound)
        request += 1
    }
}

private struct ZoomModeButton: View {
    @Environment(\.mica) private var theme
    let title: String
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title).font(.kit(size: 11, weight: .medium))
                .foregroundStyle(selected ? theme.text : theme.muted)
                .padding(.horizontal, 8).frame(height: 22)
                .background(selected ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// The image in a magnifying scroll view: centred when smaller than the
/// pane, panned by scrolling or dragging, zoomed by pinching.
private struct ImageCanvas: NSViewRepresentable {
    let image: CGImage
    let zoom: ImageZoom
    let checker: (Color, Color)

    func makeCoordinator() -> Coordinator { Coordinator(zoom: zoom) }

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = ImageScrollView()
        scroll.contentView = CenteringClipView()
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.hasHorizontalScroller = true
        scroll.autohidesScrollers = true
        scroll.scrollerStyle = .overlay
        scroll.allowsMagnification = true
        scroll.minMagnification = ImageZoom.range.lowerBound
        scroll.maxMagnification = ImageZoom.range.upperBound
        let document = ImageDocumentView(frame: CGRect(x: 0, y: 0, width: image.width, height: image.height))
        scroll.documentView = document
        scroll.onResize = { [weak coordinator = context.coordinator] in coordinator?.refit() }
        context.coordinator.scroll = scroll
        NotificationCenter.default.addObserver(context.coordinator, selector: #selector(Coordinator.magnified),
                                               name: NSScrollView.didEndLiveMagnifyNotification, object: scroll)
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        guard let document = scroll.documentView as? ImageDocumentView else { return }
        document.image = image
        document.checker = (NSColor(checker.0), NSColor(checker.1))
        document.setFrameSize(CGSize(width: image.width, height: image.height))
        let coordinator = context.coordinator
        if coordinator.request != zoom.request {
            coordinator.request = zoom.request
            if zoom.fitted { coordinator.refit() }
            else { scroll.setMagnification(zoom.scale, centeredAt: CGPoint(x: scroll.documentVisibleRect.midX, y: scroll.documentVisibleRect.midY)) }
        }
        document.needsDisplay = true
    }

    @MainActor final class Coordinator: NSObject {
        let zoom: ImageZoom
        weak var scroll: NSScrollView?
        var request = -1

        init(zoom: ImageZoom) { self.zoom = zoom }

        /// Fits the image inside the pane's margin, never enlarging it past 100%.
        func refit() {
            guard zoom.fitted, let scroll, let document = scroll.documentView else { return }
            let margin: CGFloat = 20
            let available = CGSize(width: scroll.contentSize.width - margin * 2, height: scroll.contentSize.height - margin * 2)
            let size = document.frame.size
            guard available.width > 0, available.height > 0, size.width > 0, size.height > 0 else { return }
            let scale = min(1, available.width / size.width, available.height / size.height)
            scroll.magnification = max(scale, ImageZoom.range.lowerBound)
            if zoom.scale != scroll.magnification { zoom.scale = scroll.magnification }
        }

        @objc func magnified() {
            guard let scroll else { return }
            zoom.pinched(to: scroll.magnification)
        }
    }
}

/// Reports size changes so a fitted image follows the pane.
private final class ImageScrollView: NSScrollView {
    var onResize: (() -> Void)?
    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        onResize?()
    }
}

/// Keeps a document smaller than the visible area in its centre.
private final class CenteringClipView: NSClipView {
    override func constrainBoundsRect(_ proposedBounds: NSRect) -> NSRect {
        var rect = super.constrainBoundsRect(proposedBounds)
        guard let document = documentView else { return rect }
        let size = document.frame.size
        if rect.width > size.width { rect.origin.x = (size.width - rect.width) / 2 }
        if rect.height > size.height { rect.origin.y = (size.height - rect.height) / 2 }
        return rect
    }
}

/// Draws the image over a checkerboard of fixed on-screen size, and pans the
/// scroll view while dragged.
private final class ImageDocumentView: NSView {
    var image: CGImage?
    var checker: (NSColor, NSColor) = (.windowBackgroundColor, .controlBackgroundColor)
    private var dragStart: (point: CGPoint, origin: CGPoint)?

    override var isFlipped: Bool { true }

    override func draw(_ dirtyRect: NSRect) {
        guard let context = NSGraphicsContext.current?.cgContext, let image else { return }
        let magnification = enclosingScrollView?.magnification ?? 1
        let square = 8 / magnification
        context.setFillColor(checker.0.cgColor)
        context.fill(bounds)
        context.setFillColor(checker.1.cgColor)
        let firstColumn = Int(dirtyRect.minX / square), lastColumn = Int(dirtyRect.maxX / square)
        let firstRow = Int(dirtyRect.minY / square), lastRow = Int(dirtyRect.maxY / square)
        for row in firstRow...lastRow {
            for column in firstColumn...lastColumn where (row + column) % 2 == 1 {
                context.fill(CGRect(x: CGFloat(column) * square, y: CGFloat(row) * square, width: square, height: square))
            }
        }
        // Enlarged pixels stay sharp; reduced images are smoothed.
        context.interpolationQuality = magnification >= 2 ? .none : .high
        context.saveGState()
        context.translateBy(x: 0, y: bounds.height)
        context.scaleBy(x: 1, y: -1)
        context.draw(image, in: bounds)
        context.restoreGState()
    }

    override func resetCursorRects() {
        if let scroll = enclosingScrollView, scroll.documentVisibleRect.width < bounds.width || scroll.documentVisibleRect.height < bounds.height {
            addCursorRect(visibleRect, cursor: .openHand)
        }
    }

    override func mouseDown(with event: NSEvent) {
        guard let clip = enclosingScrollView?.contentView else { return }
        dragStart = (event.locationInWindow, clip.bounds.origin)
        NSCursor.closedHand.push()
    }

    override func mouseDragged(with event: NSEvent) {
        guard let start = dragStart, let scroll = enclosingScrollView else { return }
        let magnification = scroll.magnification
        let delta = CGPoint(x: (event.locationInWindow.x - start.point.x) / magnification,
                            y: (event.locationInWindow.y - start.point.y) / magnification)
        // Window coordinates rise upward; the flipped document's fall.
        scroll.contentView.scroll(to: CGPoint(x: start.origin.x - delta.x, y: start.origin.y + delta.y))
        scroll.reflectScrolledClipView(scroll.contentView)
    }

    override func mouseUp(with event: NSEvent) {
        if dragStart != nil { NSCursor.pop() }
        dragStart = nil
    }
}
