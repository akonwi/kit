import CoreGraphics
import Foundation
import Testing
@testable import Kit

struct TileTreeTests {
    let a = UUID(), b = UUID(), c = UUID(), d = UUID()
    let s1 = UUID(), s2 = UUID(), s3 = UUID()

    /// A | (B / C)
    var sideAndStack: TileTree {
        .split(.init(id: s1, axis: .horizontal, ratio: 0.5, first: .tile(a),
            second: .split(.init(id: s2, axis: .vertical, ratio: 0.5, first: .tile(b), second: .tile(c)))))
    }

    @Test func insertingBesideATileSplitsItsSpaceOnThatEdge() {
        let right = TileTree.tile(a).inserting(b, beside: a, on: .right, split: s1)
        #expect(right == .split(.init(id: s1, axis: .horizontal, ratio: 0.5, first: .tile(a), second: .tile(b))))
        let top = right.inserting(c, beside: b, on: .top, split: s2)
        #expect(top == .split(.init(id: s1, axis: .horizontal, ratio: 0.5, first: .tile(a),
            second: .split(.init(id: s2, axis: .vertical, ratio: 0.5, first: .tile(c), second: .tile(b))))))
        #expect(top.tiles == [a, c, b])
        #expect(TileTree.tile(a).inserting(b, beside: a, on: .center) == .tile(a))
    }

    @Test func removingATileLetsItsSiblingTakeTheParentsPlace() {
        #expect(sideAndStack.removing(b) == .split(.init(id: s1, axis: .horizontal, ratio: 0.5, first: .tile(a), second: .tile(c))))
        #expect(sideAndStack.removing(a) == .split(.init(id: s2, axis: .vertical, ratio: 0.5, first: .tile(b), second: .tile(c))))
        #expect(TileTree.tile(a).removing(a) == nil)
        #expect(sideAndStack.removing(d) == sideAndStack)
    }

    @Test func ratiosAreClampedSoNeitherSideCollapses() {
        #expect(sideAndStack.settingRatio(s2, to: 0.7).split(s2)?.ratio == 0.7)
        #expect(sideAndStack.settingRatio(s2, to: 0.99).split(s2)?.ratio == 0.9)
        #expect(sideAndStack.settingRatio(s1, to: -1).split(s1)?.ratio == 0.1)
        #expect(sideAndStack.settingRatio(s1, to: 0.3).split(s2)?.ratio == 0.5)
    }

    @Test func equalizingWeighsEachSideByTheTilesAlongTheSplitsAxis() {
        // A | (B | C): each tile gets a third of the width.
        let row = TileTree.split(.init(id: s1, axis: .horizontal, ratio: 0.8, first: .tile(a),
            second: .split(.init(id: s2, axis: .horizontal, ratio: 0.2, first: .tile(b), second: .tile(c)))))
        let evenRow = row.equalized()
        #expect(evenRow.split(s1)?.ratio == 1.0 / 3.0)
        #expect(evenRow.split(s2)?.ratio == 0.5)
        // A | (B / C): the stack counts as one column, so A gets half.
        let mixed = sideAndStack.settingRatio(s1, to: 0.8).settingRatio(s2, to: 0.2).equalized()
        #expect(mixed.split(s1)?.ratio == 0.5)
        #expect(mixed.split(s2)?.ratio == 0.5)
    }

    @Test func layoutSeparatesSiblingsByTheGapOnWholePoints() {
        let layout = sideAndStack.layout(in: CGRect(x: 0, y: 0, width: 1006, height: 606), gap: 6)
        #expect(layout.tiles[a] == CGRect(x: 0, y: 0, width: 500, height: 606))
        #expect(layout.tiles[b] == CGRect(x: 506, y: 0, width: 500, height: 300))
        #expect(layout.tiles[c] == CGRect(x: 506, y: 306, width: 500, height: 300))
        #expect(layout.splits == [
            .init(id: s1, axis: .horizontal, frame: CGRect(x: 0, y: 0, width: 1006, height: 606), gap: CGRect(x: 500, y: 0, width: 6, height: 606)),
            .init(id: s2, axis: .vertical, frame: CGRect(x: 506, y: 0, width: 500, height: 606), gap: CGRect(x: 506, y: 300, width: 500, height: 6)),
        ])
        let uneven = sideAndStack.settingRatio(s1, to: 1.0 / 3.0).layout(in: CGRect(x: 0, y: 0, width: 1001, height: 600), gap: 6)
        #expect(uneven.tiles[a]?.width == 332)
        #expect(uneven.tiles[b]?.minX == 338)
        #expect(uneven.tiles[b]?.width == 663)
    }

    @Test func neighborsFollowScreenPositionNotTreeOrder() {
        // A | (B / C)
        #expect(sideAndStack.neighbor(of: a, .right) == b)
        #expect(sideAndStack.neighbor(of: c, .left) == a)
        #expect(sideAndStack.neighbor(of: b, .down) == c)
        #expect(sideAndStack.neighbor(of: c, .up) == b)
        #expect(sideAndStack.neighbor(of: a, .left) == nil)
        #expect(sideAndStack.neighbor(of: a, .down) == nil)
        // (A / B) | (C / D), right column split at 80%: C shares more of
        // B's height than D does.
        let grid = TileTree.split(.init(id: s1, axis: .horizontal, ratio: 0.5,
            first: .split(.init(id: s2, axis: .vertical, ratio: 0.5, first: .tile(a), second: .tile(b))),
            second: .split(.init(id: s3, axis: .vertical, ratio: 0.8, first: .tile(c), second: .tile(d)))))
        #expect(grid.neighbor(of: b, .right) == c)
        #expect(grid.neighbor(of: a, .right) == c)
        #expect(grid.neighbor(of: d, .left) == b)
        #expect(grid.neighbor(of: c, .left) == a)
        // Split at 20%, D shares all of B's height and C none of it.
        let raised = grid.settingRatio(s3, to: 0.2)
        #expect(raised.neighbor(of: b, .right) == d)
        #expect(raised.neighbor(of: d, .left) == b)
        #expect(raised.neighbor(of: c, .left) == a)
    }

    @Test func resizingMovesTheNearestDividerOnThatAxis() {
        let bounds = CGRect(x: 0, y: 0, width: 1006, height: 606)
        // From B, horizontal moves use the outer split and vertical moves the inner one.
        let wider = sideAndStack.resizing(b, .left, by: 100, in: bounds, gap: 6)
        #expect(wider.split(s1)?.ratio == 0.4)
        #expect(wider.split(s2)?.ratio == 0.5)
        let taller = sideAndStack.resizing(b, .down, by: 60, in: bounds, gap: 6)
        #expect(taller.split(s2)?.ratio == 0.6)
        // A has no vertical split around it.
        #expect(sideAndStack.resizing(a, .up, by: 60, in: bounds, gap: 6) == sideAndStack)
    }
}
