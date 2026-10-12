import CoreGraphics
import Foundation

/// Where a dragged tab would land: a tile, and whether it joins the tile's
/// tabs or splits the tile on one edge.
struct TileDropTarget: Equatable {
    let tile: UUID
    let edge: TileDropEdge
    /// For a drop on a tab strip, the gap between tabs it lands in: 0 is
    /// before the first tab, and the tab count is after the last. Nil
    /// appends to the tile's tabs.
    var index: Int? = nil

    /// Share of a tile's content, measured from each edge, that splits it.
    static let edgeBand: CGFloat = 0.3

    /// Resolves a pointer location against the visible tiles' frames.
    ///
    /// Over a tab strip, the tab joins that tile at the gap between tabs
    /// nearest the pointer; `tabs` gives a tile's tab frames in order. Over content, the nearest
    /// edge splits the tile when the pointer is within `edgeBand` of it,
    /// judged relative to the content's size so the zones meet at the
    /// corners' diagonals (as in Ghostty's split drop zones); otherwise the
    /// tab joins the tile. In a gap, the nearest tile edge within `gap`
    /// wins, so a drop between two tiles splits between them.
    static func resolve(_ point: CGPoint, tiles: [UUID], frames: [UUID: CGRect],
                        tabs: (UUID) -> [CGRect] = { _ in [] },
                        stripHeight: CGFloat, gap: CGFloat) -> TileDropTarget? {
        let placed = tiles.compactMap { id in frames[id].map { (id, $0) } }
        if let (id, frame) = placed.first(where: { $0.1.contains(point) }) {
            if point.y < frame.minY + stripHeight {
                return TileDropTarget(tile: id, edge: .center, index: insertion(at: point.x, among: tabs(id)))
            }
            let content = CGRect(x: frame.minX, y: frame.minY + stripHeight,
                                 width: frame.width, height: max(1, frame.height - stripHeight))
            let distances: [(TileDropEdge, CGFloat)] = [
                (.left, (point.x - content.minX) / content.width),
                (.right, (content.maxX - point.x) / content.width),
                (.top, (point.y - content.minY) / content.height),
                (.bottom, (content.maxY - point.y) / content.height),
            ]
            let nearest = distances.min { $0.1 < $1.1 }!
            return TileDropTarget(tile: id, edge: nearest.1 < edgeBand ? nearest.0 : .center)
        }
        // In a gap: the closest edge of a neighboring tile.
        var best: (target: TileDropTarget, distance: CGFloat)?
        for (id, frame) in placed {
            let candidates: [(TileDropEdge, CGFloat, Bool)] = [
                (.left, frame.minX - point.x, point.y >= frame.minY && point.y <= frame.maxY),
                (.right, point.x - frame.maxX, point.y >= frame.minY && point.y <= frame.maxY),
                (.top, frame.minY - point.y, point.x >= frame.minX && point.x <= frame.maxX),
                (.bottom, point.y - frame.maxY, point.x >= frame.minX && point.x <= frame.maxX),
            ]
            for (edge, distance, alongside) in candidates where alongside && distance >= 0 && distance <= gap {
                if best == nil || distance < best!.distance { best = (TileDropTarget(tile: id, edge: edge), distance) }
            }
        }
        return best?.target
    }

    /// The gap between tabs nearest `x`: the number of tabs whose middle
    /// lies before it.
    static func insertion(at x: CGFloat, among tabs: [CGRect]) -> Int {
        tabs.filter { $0.midX < x }.count
    }

    /// The area a drop preview covers within the target tile.
    func preview(in frame: CGRect) -> CGRect {
        let inset: CGFloat = 4
        let tile = frame.insetBy(dx: inset, dy: inset)
        switch edge {
        case .center: return tile
        case .left: return CGRect(x: tile.minX, y: tile.minY, width: tile.width / 2, height: tile.height)
        case .right: return CGRect(x: tile.midX, y: tile.minY, width: tile.width / 2, height: tile.height)
        case .top: return CGRect(x: tile.minX, y: tile.minY, width: tile.width, height: tile.height / 2)
        case .bottom: return CGRect(x: tile.minX, y: tile.midY, width: tile.width, height: tile.height / 2)
        }
    }
}
