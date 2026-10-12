import CoreGraphics
import Foundation
import Testing
@testable import Kit

struct TileDropTargetTests {
    let a = UUID(), b = UUID(), c = UUID()
    /// A | (B / C) in 1006 × 640 with 6 pt gaps and 34 pt tab strips.
    var frames: [UUID: CGRect] {
        [a: CGRect(x: 0, y: 0, width: 500, height: 640),
         b: CGRect(x: 506, y: 0, width: 500, height: 317),
         c: CGRect(x: 506, y: 323, width: 500, height: 317)]
    }

    func target(_ x: CGFloat, _ y: CGFloat) -> TileDropTarget? {
        TileDropTarget.resolve(CGPoint(x: x, y: y), tiles: [a, b, c], frames: frames, stripHeight: 34, gap: 6)
    }

    @Test func tabStripsAddToTheTile() {
        #expect(target(10, 5) == .init(tile: a, edge: .center))
        #expect(target(990, 30) == .init(tile: b, edge: .center))
        #expect(target(600, 330) == .init(tile: c, edge: .center))
    }

    @Test func contentSplitsNearAnEdgeAndJoinsInTheMiddle() {
        // A's content spans y 34–640 (606 tall); 30% bands are 150 wide and ~182 tall.
        #expect(target(250, 337) == .init(tile: a, edge: .center))
        #expect(target(149, 337) == .init(tile: a, edge: .left))
        #expect(target(151, 337) == .init(tile: a, edge: .center))
        #expect(target(400, 337) == .init(tile: a, edge: .right))
        #expect(target(250, 40) == .init(tile: a, edge: .top))
        #expect(target(250, 600) == .init(tile: a, edge: .bottom))
    }

    @Test func cornersGoToTheRelativelyNearerEdge() {
        // 20 pt from the left (4%) and 40 pt below the strip (6.6%): left.
        #expect(target(20, 74) == .init(tile: a, edge: .left))
        // 40 pt from the left (8%) and 20 pt below the strip (3.3%): top.
        #expect(target(40, 54) == .init(tile: a, edge: .top))
    }

    @Test func gapsSplitBetweenTheTilesBesideThem() {
        // Between A and B/C: the nearer side wins.
        #expect(target(501, 200) == .init(tile: a, edge: .right))
        #expect(target(505, 200) == .init(tile: b, edge: .left))
        #expect(target(505, 500) == .init(tile: c, edge: .left))
        // Between B and C.
        #expect(target(700, 319) == .init(tile: b, edge: .bottom))
        #expect(target(700, 322) == .init(tile: c, edge: .top))
        // Beyond every tile.
        #expect(target(1100, 200) == nil)
        #expect(target(250, 700) == nil)
    }

    @Test func previewsCoverTheTileOrTheHalfThatWouldSplit() {
        let frame = CGRect(x: 0, y: 0, width: 508, height: 408)
        #expect(TileDropTarget(tile: a, edge: .center).preview(in: frame) == CGRect(x: 4, y: 4, width: 500, height: 400))
        #expect(TileDropTarget(tile: a, edge: .right).preview(in: frame) == CGRect(x: 254, y: 4, width: 250, height: 400))
        #expect(TileDropTarget(tile: a, edge: .top).preview(in: frame) == CGRect(x: 4, y: 4, width: 500, height: 200))
    }

    @MainActor @Test func dropsThatChangeNothingAreRejected() {
        let workspace = WorkspaceState(demo: false)
        let first = workspace.focusedTile
        workspace.open(.review)
        workspace.split(.review, .right)
        let right = workspace.focusedTile
        // The Diff tile's only tab can't join or split its own tile.
        #expect(!workspace.canMove(.review, to: .init(tile: right, edge: .center)))
        #expect(!workspace.canMove(.review, to: .init(tile: right, edge: .left)))
        #expect(workspace.canMove(.review, to: .init(tile: first, edge: .center)))
        #expect(workspace.canMove(.review, to: .init(tile: first, edge: .bottom)))
        workspace.open(.scratchpad)
        #expect(workspace.canMove(.review, to: .init(tile: right, edge: .bottom)))
        #expect(!workspace.canMove(.conversation, to: .init(tile: first, edge: .center)))
    }
}
