import AppKit
import KitTranscript
import Testing
@testable import Kit

/// Opening a transcript's Mermaid diagram in a tab, and drawing it there.
@MainActor
struct DiagramPaneTests {
    private let flow = "flowchart LR\nA --> B\n"

    private func tabs(_ workspace: WorkspaceState) -> TileArrangement.Node { workspace.arrangement.root }

    @Test func opensInANewTileRightOfTheTranscript() throws {
        let workspace = WorkspaceState()
        workspace.openDiagram(flow, beside: .conversation)
        let pane = workspace.selected
        guard case .diagram = pane else { Issue.record("expected a diagram tab, got \(pane)"); return }
        #expect(pane.title == "Diagram")
        #expect(workspace.diagram(pane) == flow)
        #expect(tabs(workspace) == .split(.init(axis: .horizontal, ratio: 0.5,
            first: .tile(.init(panes: ["conversation"], selected: "conversation")),
            second: .tile(.init(panes: [pane.id], selected: pane.id)))))
    }

    @Test func reselectsTheTabShowingTheSameDiagram() {
        let workspace = WorkspaceState()
        workspace.openDiagram(flow, beside: .conversation)
        let first = workspace.selected
        workspace.openDiagram("flowchart TD\nC --> D\n", beside: .conversation)
        let second = workspace.selected
        #expect(second != first)

        workspace.openDiagram(flow, beside: .conversation)
        #expect(workspace.selected == first)
        #expect(workspace.panes == [.conversation, first, second])
    }

    @Test func aClosedOrRestoredDiagramTabIsGone() {
        let workspace = WorkspaceState()
        workspace.openDiagram(flow, beside: .conversation)
        let pane = workspace.selected
        workspace.close(pane)
        #expect(workspace.panes == [.conversation])
        #expect(workspace.diagram(pane) == nil)
        workspace.open(pane)
        #expect(workspace.panes == [.conversation])

        workspace.openDiagram(flow, beside: .conversation)
        let reopened = workspace.selected
        #expect(WorkspacePane(id: reopened.id) == nil)
        workspace.restore(workspace.arrangement)
        #expect(workspace.panes == [.conversation])
        #expect(workspace.diagram(reopened) == nil)
    }

    @Test func drawsSharpAtEachZoomWithinTheImageLimits() throws {
        let diagram = MermaidDiagram(source: flow)
        let picture = DiagramPicture(diagram: diagram, drawing: try diagram.drawing.get(),
                                     theme: MicaTheme(dark: true), displayScale: 2)
        #expect(picture.description == "flowchart · \(Int(picture.size.width.rounded())) × \(Int(picture.size.height.rounded()))")
        #expect(picture.renderScale(zoom: 0.5) == 2)
        #expect(picture.renderScale(zoom: 1) == 2)
        #expect(picture.renderScale(zoom: 1.25) == 3)
        #expect(picture.renderScale(zoom: 3) == 6)
        // A small diagram reaches 16× on a Retina screen.
        #expect(picture.size.width * 32 <= DiagramPicture.maximumSide)
        #expect(picture.size.width * picture.size.height * 32 * 32 <= DiagramPicture.maximumPixels)
        #expect(picture.renderScale(zoom: 16) == 32)

        let chain = "flowchart LR\n" + (0..<40).map { "N\($0) --> N\($0 + 1)" }.joined(separator: "\n")
        let wide = MermaidDiagram(source: chain)
        let long = DiagramPicture(diagram: wide, drawing: try wide.drawing.get(), theme: MicaTheme(dark: true), displayScale: 2)
        #expect(long.renderScale(zoom: 16) == (DiagramPicture.maximumSide / long.size.width).rounded(.down))

        let image = try #require(picture.image(scale: picture.renderScale(zoom: 1)))
        #expect(image.width == Int((picture.size.width * 2).rounded()))
        #expect(image.height == Int((picture.size.height * 2).rounded()))
        let plate = try #require(picture.image(scale: picture.exportScale, plate: true))
        #expect(plate.width == Int(((picture.size.width + 32) * 2).rounded()))
        #expect(plate.height == Int(((picture.size.height + 32) * 2).rounded()))
    }
}
