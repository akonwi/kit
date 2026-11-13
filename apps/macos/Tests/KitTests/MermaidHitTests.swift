import KitMermaid
import Testing

/// Pointing at a drawn diagram finds the part drawn there, as the
/// reference's hit-testing does (`apps/cli/tui/mermaid/canvas.ard`).
struct MermaidHitTests {
    /// The part a cell points at, as `target index`, or `none`.
    static func pointed(_ drawing: Canvas, _ x: Int, _ y: Int) -> String {
        drawing.hit(x, y).map { "\($0.target.rawValue) \($0.index)" } ?? "none"
    }

    static func drawn(_ source: String) throws -> Canvas {
        try Mermaid.render(source).get()
    }

    /// A node's box, an edge's own line and its text point at them; where
    /// edges branch or merge, and off the drawing, nothing is pointed at.
    @Test func pointsOnAFlowchartFindItsNodesAndEdges() throws {
        // Drawn as testdata/mermaid/flowchart-labeled-branches-merge-again.txt.
        let drawing = try Self.drawn("""
            flowchart TD
            A{Valid?} -- yes --> B[Save]
            A -- no --> C[Reject]
            B --> D[Done]
            C --> D
            """)
        let got = [(7, 1), (16, 5), (4, 4), (10, 3), (10, 10), (0, 0)].map { Self.pointed(drawing, $0.0, $0.1) }
        #expect(got == ["node 0", "edge 1", "edge 0", "none", "none", "none"])
    }

    /// A subgraph's box and its section's heading point at the subgraph;
    /// its nodes, drawn in the section, at themselves.
    @Test func pointsOnAFlowchartWithSubgraphsFindThem() throws {
        let drawing = try Self.drawn("flowchart TD\nA --> one\nsubgraph one\n  B --> C\nend")
        var found: [String] = []
        for y in 0..<drawing.height {
            for x in 0..<drawing.width {
                let here = Self.pointed(drawing, x, y)
                if here != "none" && !found.contains(here) { found.append(here) }
            }
        }
        #expect(found == ["node 0", "edge 0", "subgraph 0", "node 2", "edge 1", "node 3"])
    }

    /// A message's text and arrow point at it over the lifelines it
    /// crosses; a block's frame and legend at the block; a lifeline alone at
    /// its participant; and where a frame crosses a lifeline, nothing.
    @Test func pointsOnASequenceFindItsParts() throws {
        // Drawn as testdata/mermaid/sequence-nested-blocks-with-dividers-and-heads.txt.
        let drawing = try Self.drawn("""
            sequenceDiagram
            par fetch
            A->>B: x
            and
            critical lock
            A-)B: y
            option timeout
            B<<-->>A: z
            end
            end
            """)
        let got = [(4, 1), (8, 5), (12, 6), (4, 7), (5, 4), (0, 12), (6, 13), (4, 17), (20, 1)]
            .map { Self.pointed(drawing, $0.0, $0.1) }
        #expect(got == ["participant 0", "message 1", "message 1", "participant 0", "block 0", "block 0",
                        "block 3", "none", "none"])
    }
}
