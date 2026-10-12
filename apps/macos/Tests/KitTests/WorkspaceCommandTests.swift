import Foundation
import Testing
@testable import Kit

/// Keyboard tile commands: spatial focus, tab moves, resizing, tab cycling,
/// and cancelling a drag.
@MainActor
struct WorkspaceCommandTests {
    /// Agent on the left; Scratchpad and Diff in a tile on the right, with
    /// Diff selected and focused.
    private func sideBySide() -> (WorkspaceState, left: UUID, right: UUID) {
        let workspace = WorkspaceState()
        workspace.bounds = CGRect(x: 0, y: 0, width: 1000, height: 600)
        let left = workspace.focusedTile
        workspace.open(.scratchpad)
        workspace.split(.scratchpad, .right)
        let right = workspace.focusedTile
        workspace.open(.review)
        return (workspace, left, right)
    }

    @Test func focusMovesToTheNeighborOnScreenAndEndsAZoom() {
        let (workspace, left, right) = sideBySide()
        #expect(workspace.canFocus(.left))
        #expect(!workspace.canFocus(.right))
        workspace.focus(.left)
        #expect(workspace.focusedTile == left)
        #expect(workspace.selected == .conversation)
        workspace.toggleZoom()
        workspace.focus(.right)
        #expect(workspace.focusedTile == right)
        #expect(workspace.zoomedTile == nil)
        #expect(workspace.selected == .review)
    }

    @Test func movingATabJoinsTheNeighborOrSplitsAtTheEdge() {
        let (workspace, left, right) = sideBySide()
        workspace.moveSelectedTab(.left)
        #expect(workspace.tiles[left] == WorkspaceTile(panes: [.conversation, .review], selected: .review))
        #expect(workspace.tiles[right] == WorkspaceTile(panes: [.scratchpad], selected: .scratchpad))
        #expect(workspace.focusedTile == left)

        workspace.moveSelectedTab(.left)
        let new = workspace.focusedTile
        #expect(workspace.root.tiles == [new, left, right])
        #expect(workspace.tiles[new] == WorkspaceTile(panes: [.review], selected: .review))
        // A tile's only tab has nowhere to go past the edge.
        #expect(!workspace.canMoveSelectedTab(.left))
    }

    @Test func resizingMovesTheDividerAndStopsAtTheMinimumWidth() {
        let (workspace, left, right) = sideBySide()
        func widths() -> (CGFloat, CGFloat) {
            let tiles = workspace.root.layout(in: workspace.bounds, gap: tileGap).tiles
            return (tiles[left]!.width, tiles[right]!.width)
        }
        #expect(widths() == (497, 497))
        // The divider moves toward the arrow, whichever side is focused.
        workspace.resize(.right)
        #expect(widths() == (537, 457))
        workspace.focus(.left)
        workspace.resize(.left)
        #expect(widths() == (497, 497))
        for _ in 0..<20 { workspace.resize(.right) }
        #expect(widths() == (817, 177))
        workspace.resize(.up)
        #expect(widths() == (817, 177))
    }

    @Test func tabCyclingWrapsWithinTheFocusedTile() {
        let (workspace, _, _) = sideBySide()
        #expect(workspace.canSelectAdjacentTab())
        workspace.selectAdjacentTab(1)
        #expect(workspace.selected == .scratchpad)
        workspace.selectAdjacentTab(-1)
        #expect(workspace.selected == .review)
        workspace.focus(.left)
        #expect(!workspace.canSelectAdjacentTab())
    }

    @Test func escCancelsADragUntilItsGestureEnds() {
        let (workspace, _, _) = sideBySide()
        workspace.tabDrag = .init(pane: .review, location: .zero, target: nil)
        workspace.cancelTabDrag()
        #expect(workspace.tabDrag == nil)
        #expect(workspace.dragCancelled(.review))
        #expect(workspace.dragCancelled(.review, ending: true))
        #expect(!workspace.dragCancelled(.review))
    }

    @Test func activitySlotReportsTheMostUrgentStatus() {
        let retry = "1970-01-01T00:00:05Z"
        #expect(ActivityLine.resolve(activity: "Reading", compaction: "Compacting session…", retryAt: retry,
                                     replaying: true, notice: "Local preview only") == .retrying(at: retry))
        #expect(ActivityLine.resolve(activity: "Reading", compaction: "Compacting session…") == .compacting("Compacting session…"))
        #expect(ActivityLine.resolve(activity: "Reading", replaying: true) == .working("Reading"))
        #expect(ActivityLine.resolve(activity: "", replaying: true, notice: "Local preview only") == .replaying)
        #expect(ActivityLine.resolve(activity: nil, notice: "Local preview only") == .notice("Local preview only"))
        #expect(ActivityLine.resolve(activity: nil) == nil)
    }
}
