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
        #expect(target(10, 5) == .init(tile: a, edge: .center, index: 0))
        #expect(target(990, 30) == .init(tile: b, edge: .center, index: 0))
        #expect(target(600, 330) == .init(tile: c, edge: .center, index: 0))
    }

    @Test func stripDropsLandInTheGapNearestThePointer() {
        // A's tabs: 6–86, 86–186, 186–246.
        let tabs = [CGRect(x: 6, y: 0, width: 80, height: 34), CGRect(x: 86, y: 0, width: 100, height: 34),
                    CGRect(x: 186, y: 0, width: 60, height: 34)]
        func strip(_ x: CGFloat) -> TileDropTarget? {
            TileDropTarget.resolve(CGPoint(x: x, y: 17), tiles: [a, b, c], frames: frames,
                tabs: { $0 == a ? tabs : [] }, stripHeight: 34, gap: 6)
        }
        #expect(strip(20) == .init(tile: a, edge: .center, index: 0))
        #expect(strip(60) == .init(tile: a, edge: .center, index: 1))
        #expect(strip(140) == .init(tile: a, edge: .center, index: 2))
        #expect(strip(230) == .init(tile: a, edge: .center, index: 3))
        #expect(strip(400) == .init(tile: a, edge: .center, index: 3))
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

    @MainActor @Test func stripDropsReorderAndInsertTabs() {
        let workspace = WorkspaceState(demo: false)
        let first = workspace.focusedTile
        workspace.open(.review)
        workspace.open(.scratchpad)
        // Agent, Diff, Scratchpad. Gaps either side of a tab leave it in place.
        #expect(!workspace.canMove(.review, to: .init(tile: first, edge: .center, index: 1)))
        #expect(!workspace.canMove(.review, to: .init(tile: first, edge: .center, index: 2)))
        workspace.move(.review, to: .init(tile: first, edge: .center, index: 3))
        #expect(workspace.tiles[first]?.panes == [.conversation, .scratchpad, .review])
        workspace.move(.review, to: .init(tile: first, edge: .center, index: 0))
        #expect(workspace.tiles[first]?.panes == [.review, .conversation, .scratchpad])
        #expect(workspace.selected == .review)

        workspace.split(.scratchpad, .right)
        let right = workspace.focusedTile
        workspace.move(.review, to: .init(tile: right, edge: .center, index: 0))
        #expect(workspace.tiles[right]?.panes == [.review, .scratchpad])
        workspace.move(.conversation, to: .init(tile: right, edge: .center, index: 1))
        #expect(workspace.tiles[first] == nil)
        #expect(workspace.root == .tile(right))
        #expect(workspace.tiles[right] == WorkspaceTile(panes: [.review, .conversation, .scratchpad], selected: .conversation))
    }
}
