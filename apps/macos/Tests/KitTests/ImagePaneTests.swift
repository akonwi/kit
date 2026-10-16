import AppKit
import ImageIO
import Testing
import UniformTypeIdentifiers
@testable import Kit

/// Opening transcript images in the viewer, and its zoom.
@MainActor
struct ImagePaneTests {
    private let image = WorkspacePane.image(id: "att_1", filename: "screen: shot.png")

    private func tabs(_ workspace: WorkspaceState) -> TileArrangement.Node { workspace.arrangement.root }

    @Test func imagePanesKeepTheirFilenameThroughRestoration() {
        #expect(image.id == "image:att_1:screen: shot.png")
        #expect(image.title == "screen: shot.png")
        #expect(WorkspacePane(id: image.id) == image)
        #expect(WorkspacePane(id: "image:att_1") == nil)
        #expect(WorkspacePane(id: "image::shot.png") == nil)
        #expect(WorkspacePane(id: "image:att_1:") == nil)
    }

    @Test func opensInANewTileRightOfTheTranscript() {
        let workspace = WorkspaceState()
        workspace.open(image, beside: .conversation)
        #expect(tabs(workspace) == .split(.init(axis: .horizontal, ratio: 0.5,
            first: .tile(.init(panes: ["conversation"], selected: "conversation")),
            second: .tile(.init(panes: [image.id], selected: image.id)))))
        #expect(workspace.selected == image)
    }

    @Test func joinsTheTileBesideTheTranscriptAndReselectsAnOpenImage() {
        let workspace = WorkspaceState()
        workspace.open(.review)
        workspace.split(.review, .right)
        workspace.open(.scratchpad)
        workspace.focus(tile: workspace.tile(containing: .conversation)!)
        workspace.open(image, beside: .conversation)
        let second = WorkspacePane.image(id: "att_2", filename: "b.png")
        workspace.open(second, beside: .conversation)
        #expect(tabs(workspace) == .split(.init(axis: .horizontal, ratio: 0.5,
            first: .tile(.init(panes: ["conversation"], selected: "conversation")),
            second: .tile(.init(panes: ["review", "scratchpad", image.id, second.id], selected: second.id)))))

        workspace.open(image, beside: .conversation)
        #expect(workspace.selected == image)
        #expect(workspace.panes.filter { $0 == image }.count == 1)
    }

    @Test func zoomStepsThroughPreviewsScalesWithinRange() {
        let zoom = ImageZoom()
        #expect(zoom.fitted)
        zoom.scale = 0.42
        zoom.zoomIn()
        #expect(!zoom.fitted)
        #expect(zoom.scale == 0.5)
        zoom.zoomOut(); zoom.zoomOut()
        #expect(zoom.scale == 0.25)
        zoom.actualSize()
        #expect(zoom.scale == 1)
        zoom.scale = 16
        zoom.zoomIn()
        #expect(zoom.scale == 16)
        zoom.fit()
        #expect(zoom.fitted)
    }

    @Test func decodesThePixelSizeAndRejectsData() throws {
        let context = try #require(CGContext(data: nil, width: 30, height: 20, bitsPerComponent: 8, bytesPerRow: 0,
                                             space: CGColorSpaceCreateDeviceRGB(),
                                             bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        let data = NSMutableData()
        let destination = try #require(CGImageDestinationCreateWithData(data, UTType.png.identifier as CFString, 1, nil))
        CGImageDestinationAddImage(destination, try #require(context.makeImage()), nil)
        #expect(CGImageDestinationFinalize(destination))
        let loaded = try #require(LoadedImage(data: data as Data))
        #expect(loaded.width == 30 && loaded.height == 20)
        #expect(loaded.cgImage.width == 30)
        #expect(LoadedImage(data: Data("not an image".utf8)) == nil)
    }
}
