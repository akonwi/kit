import Foundation
import KitMermaid
import SwiftUI
import Testing
@testable import Kit

struct MermaidPresentationTests {
    @Test func aClosedMermaidFenceIsDrawn() throws {
        let content = MarkdownContent(source: "The flow:\n\n```mermaid\nflowchart LR\nA --> B\n```\n")
        guard case .mermaid(let diagram) = content.blocks.last else {
            Issue.record("expected a diagram, got \(content.blocks)")
            return
        }
        #expect(diagram.source == "flowchart LR\nA --> B\n")
        let drawing = try diagram.drawing.get()
        #expect(drawing.lines() == [
            "┌───┐    ┌───┐",
            "│ A ├───►│ B │",
            "└───┘    └───┘",
        ])
    }

    @Test func aDiagramThatIsNotDrawnKeepsItsSourceAndReason() {
        let content = MarkdownContent(source: "```mermaid\nclassDiagram\n  A <|-- B\n```\n")
        #expect(content.blocks == [.mermaid(MermaidDiagram(source: "classDiagram\n  A <|-- B\n"))])
        guard case .mermaid(let diagram) = content.blocks.first else { return }
        #expect(diagram.drawing == .failure(NotDrawn(reason: "`classDiagram` diagrams aren't drawn yet")))
    }

    @Test func aStreamingDiagramShowsItsSourceUntilItsFenceCloses() {
        let streaming = MarkdownContent(source: "Here:\n\n```mermaid\nflowchart TD\nA --> B")
        #expect(streaming.blocks.last == .code("mermaid", "flowchart TD\nA --> B\n"))

        let inList = MarkdownContent(source: "- step\n  ```mermaid\n  flowchart TD\n  A --> B")
        #expect(inList.blocks == [.list([.init(marker: "•", blocks: [
            .paragraph(AttributedString("step")),
            .code("mermaid", "flowchart TD\nA --> B\n"),
        ])])])

        let inQuote = MarkdownContent(source: "> ```mermaid\n> flowchart TD\n> A")
        #expect(inQuote.blocks == [.quote([.code("mermaid", "flowchart TD\nA\n")])])

        // A later open fence leaves earlier, closed diagrams drawn.
        let later = MarkdownContent(source: "```mermaid\nflowchart TD\nA\n```\n\n```swift\nlet a")
        #expect(later.blocks == [.mermaid(MermaidDiagram(source: "flowchart TD\nA\n")), .code("swift", "let a\n")])
    }

    @Test func fencesCloseOnlyOnALongEnoughRunOfTheirOwnCharacter() {
        #expect(MarkdownContent.endsInsideFence("```mermaid\nA\n"))
        #expect(!MarkdownContent.endsInsideFence("```mermaid\nA\n```"))
        #expect(!MarkdownContent.endsInsideFence("````mermaid\nA\n`````\n"))
        #expect(MarkdownContent.endsInsideFence("````mermaid\nA\n```\n"))
        #expect(MarkdownContent.endsInsideFence("~~~mermaid\nA\n```\n"))
        #expect(MarkdownContent.endsInsideFence("```mermaid\r\nA\r\n```js\r\n"))
        #expect(!MarkdownContent.endsInsideFence("```mermaid\r\nA\r\n```\r\n"))
        #expect(!MarkdownContent.endsInsideFence("> ```mermaid\n> A\n> ```\n"))
        // A backtick fence's info string can't contain backticks.
        #expect(!MarkdownContent.endsInsideFence("``` a`b\n"))
    }

    @Test func copyingAsPlainTextKeepsADiagramsSource() {
        #expect(MessageText.plain("Flow:\n\n```mermaid\nflowchart TD\nA --> B\n```\n") == "Flow:\n\nflowchart TD\nA --> B")
    }

    // MARK: Scene geometry

    private static let metrics = MermaidScene.Metrics(cell: CGSize(width: 10, height: 20), line: 1, scale: 1)

    private static func scene(_ source: String) throws -> MermaidScene {
        MermaidScene(try Mermaid.render(source).get(), metrics: metrics)
    }

    private static func path(_ points: [CGPoint]) -> Path {
        var path = Path()
        path.move(to: points[0])
        for point in points.dropFirst() { path.addLine(to: point) }
        return path
    }

    private static func node(_ rect: CGRect, radius: CGFloat = 5) -> MermaidScene.Element {
        .shape(Path(roundedRect: rect, cornerRadius: radius, style: .continuous), fill: .nodeFill,
               stroke: .nodeStroke, width: 1, dash: [], lifted: true)
    }

    @Test func aFlowchartsNodesAreLiftedShapesWithTheirTextCentered() throws {
        // ┌───┐         ┌───┐
        // │ A ├─── go ─►│ B │
        // └───┘         └───┘
        let scene = try Self.scene("flowchart LR\nA -->|go| B")
        // Outlines run through their border cells' centers.
        #expect(scene.elements.contains(Self.node(CGRect(x: 5, y: 10, width: 40, height: 40))))
        #expect(scene.elements.contains(Self.node(CGRect(x: 145, y: 10, width: 40, height: 40))))
        #expect(scene.elements.contains(.text("A", at: CGPoint(x: 25, y: 30), .node, leading: false)))
        #expect(scene.elements.contains(.text("B", at: CGPoint(x: 165, y: 30), .node, leading: false)))
    }

    @Test func edgeTextSitsOnAnUnbrokenEdge() throws {
        let across = try Self.scene("flowchart LR\nA -->|go| B")
        #expect(across.elements.contains(.text("go", at: CGPoint(x: 100, y: 30), .edgeLabel, leading: false)))
        // The terminal's gap around the text is joined beneath its pill.
        for x in stride(from: 50, to: 130, by: 10) {
            #expect(across.elements.contains(.stroke(Self.path([CGPoint(x: x, y: 30), CGPoint(x: x + 10, y: 30)]),
                                                     .line, width: 1.25, dash: [], phase: 0)))
        }

        //  ┌───┐
        //  │ A │
        //  └─┬─┘
        //    │
        //   go
        //    ▼
        let down = try Self.scene("flowchart TD\nA -->|go| B")
        // Centered on the line it runs down, not on its cells.
        #expect(down.elements.contains(.text("go", at: CGPoint(x: 35, y: 90), .edgeLabel, leading: false)))
        #expect(down.elements.contains(.stroke(Self.path([CGPoint(x: 35, y: 80), CGPoint(x: 35, y: 100)]), .line,
                                               width: 1.25, dash: [], phase: 0)))
    }

    @Test func edgesBendInCurves() throws {
        //     ┌───┐
        //     │ A │
        //     └─┬─┘
        //   ┌───┴───┐
        //   ▼       ▼
        let scene = try Self.scene("flowchart TD\nA --> B\nA --> C")
        var bend = Path()
        bend.move(to: CGPoint(x: 30, y: 70))
        bend.addQuadCurve(to: CGPoint(x: 25, y: 75), control: CGPoint(x: 25, y: 70))
        bend.addLine(to: CGPoint(x: 25, y: 80))
        #expect(scene.elements.contains(.stroke(bend, .line, width: 1.25, dash: [], phase: 0)))
    }

    @Test func aSequenceDiagramDrawsWholeLifelinesBlocksAndLegends() throws {
        // ┌───┐   ┌───┐
        // │ A │   │ B │
        // └─┬─┘   └─┬─┘
        //   │       │
        //   │  hi   │
        //   ├──────►│
        //   │       │
        // ┌ alt [ok] ──┐
        // │ │  yes  │  │
        // │ │◄┈┈┈┈┈┈┤  │
        // │ │       │  │
        // ├ else [no] ┈┤
        // │ │   x   │  │
        // │ │◄┈┈┈┈┈┈┤  │
        // │ │       │  │
        // └─┼───────┼──┘
        //   │       │
        let scene = try Self.scene("sequenceDiagram\nA->>B: hi\nalt ok\nB-->>A: yes\nelse no\nB-->>A: x\nend")
        #expect(scene.elements.contains(Self.node(CGRect(x: 5, y: 10, width: 40, height: 40), radius: 6)))
        #expect(scene.elements.contains(.text("A", at: CGPoint(x: 25, y: 30), .name, leading: false)))
        // A lifeline runs whole from its participant to the drawing's end.
        #expect(scene.elements.contains(.stroke(Self.path([CGPoint(x: 25, y: 50), CGPoint(x: 25, y: 340)]),
                                                .lifeline, width: 1, dash: [], phase: 0)))
        #expect(scene.elements.contains(.text("hi", at: CGPoint(x: 60, y: 90), .message, leading: false)))
        #expect(scene.elements.contains(.shape(
            Path(roundedRect: CGRect(x: 5, y: 150, width: 130, height: 160), cornerRadius: 6, style: .continuous),
            fill: nil, stroke: .blockStroke, width: 1, dash: [], lifted: false)))
        #expect(scene.elements.contains(.legend(keyword: "alt", condition: "ok", at: CGPoint(x: 20, y: 150))))
        // A divider spans its block beneath its legend.
        let third = 10.0 / 3
        #expect(scene.elements.contains(.stroke(Self.path([CGPoint(x: 5, y: 230), CGPoint(x: 135, y: 230)]),
                                                .blockStroke, width: 1, dash: [third, third], phase: 5)))
        #expect(scene.elements.contains(.legend(keyword: "else", condition: "no", at: CGPoint(x: 20, y: 230))))
    }

    @Test func aDashedMessageIsDashedToItsLifeline() throws {
        let scene = try Self.scene("sequenceDiagram\nA->>B: hi\nalt ok\nB-->>A: yes\nelse no\nB-->>A: x\nend")
        // The junction where the reply leaves B's lifeline takes its dashes.
        let leaving = Self.path([CGPoint(x: 100, y: 190), CGPoint(x: 105, y: 190)])
        #expect(scene.elements.contains { element in
            guard case .stroke(let path, .line, _, let dash, _) = element else { return false }
            return path == leaving && dash == [10.0 / 3, 10.0 / 3]
        })
    }

    @Test func anActorIsAStadium() throws {
        // ╭───╮
        // │ U │
        // ╰─┬─╯
        //   │
        let scene = try Self.scene("sequenceDiagram\nactor U")
        #expect(scene.elements.first == .stroke(Self.path([CGPoint(x: 25, y: 50), CGPoint(x: 25, y: 80)]), .lifeline,
                                                width: 1, dash: [], phase: 0))
        #expect(scene.elements.contains(.shape(Path(roundedRect: CGRect(x: 5, y: 10, width: 40, height: 40),
                                                    cornerRadius: 20), fill: .nodeFill, stroke: .nodeStroke,
                                               width: 1, dash: [], lifted: true)))
        #expect(scene.elements.contains(.text("U", at: CGPoint(x: 25, y: 30), .name, leading: false)))
    }

    @Test func aTallSceneIsDrawnInBandsOfWholeCells() throws {
        let messages = (1...20).map { "  A->>B: step \($0)" }.joined(separator: "\n")
        let scene = try Self.scene("sequenceDiagram\n\(messages)")
        // 20-point cells fit 51 to a 1,024-point band.
        #expect(scene.size.height == 1280)
        #expect(scene.bands.map(\.top) == [0, 1020])
        #expect(scene.bands.map(\.height) == [1020, 260])
        // A lifeline crossing the seam is drawn in both bands.
        let lifeline = try #require(scene.elements.firstIndex(of: .stroke(
            Self.path([CGPoint(x: 25, y: 50), CGPoint(x: 25, y: 1280)]), .lifeline, width: 1, dash: [], phase: 0)))
        #expect(scene.bands.allSatisfy { $0.elements.contains(lifeline) })
        #expect(Set(scene.bands.flatMap(\.elements)) == Set(scene.elements.indices))
    }
}
