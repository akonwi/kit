import CoreGraphics
import Foundation

/// Which way a split divides its space. Horizontal places its children side
/// by side; vertical stacks them.
enum SplitAxis: Hashable {
    case horizontal, vertical
}

/// A direction on screen, used for spatial focus and resizing.
enum TileDirection: Hashable {
    case left, right, up, down

    var axis: SplitAxis { self == .left || self == .right ? .horizontal : .vertical }
}

/// Where a pane lands relative to a tile: into its tab group, or onto one of
/// its edges as a new tile.
enum TileDropEdge: Hashable {
    case center, left, right, top, bottom
}

/// A session workspace's split tree. Leaves are tile identities; the tiles'
/// tab groups live beside the tree so the tree stays plain, comparable data.
///
/// The shape follows Ghostty's `SplitTree`
/// (`macos/Sources/Features/Splits/SplitTree.swift`, MIT): weighted
/// equalization, spatial neighbors, and directional resizing come from it.
indirect enum TileTree: Equatable {
    case tile(UUID)
    case split(Split)

    struct Split: Equatable, Identifiable {
        let id: UUID
        var axis: SplitAxis
        /// The first child's share of the space left after the gap.
        var ratio: Double
        var first: TileTree
        var second: TileTree
    }

    /// Ratios stay within this range so neither side collapses.
    static let ratioRange = 0.1...0.9

    /// Leaves in reading order: first child before second.
    var tiles: [UUID] {
        switch self {
        case .tile(let id): [id]
        case .split(let split): split.first.tiles + split.second.tiles
        }
    }

    func contains(_ tile: UUID) -> Bool {
        switch self {
        case .tile(let id): id == tile
        case .split(let split): split.first.contains(tile) || split.second.contains(tile)
        }
    }

    /// Replaces the leaf for `tile` with `node`.
    func replacing(_ tile: UUID, with node: TileTree) -> TileTree {
        switch self {
        case .tile(let id): return id == tile ? node : self
        case .split(var split):
            split.first = split.first.replacing(tile, with: node)
            split.second = split.second.replacing(tile, with: node)
            return .split(split)
        }
    }

    /// Places `new` beside `target` on `edge`, splitting the target's space
    /// evenly. A center edge leaves the tree unchanged.
    func inserting(_ new: UUID, beside target: UUID, on edge: TileDropEdge, split id: UUID = UUID()) -> TileTree {
        let axis: SplitAxis
        let newFirst: Bool
        switch edge {
        case .center: return self
        case .left: axis = .horizontal; newFirst = true
        case .right: axis = .horizontal; newFirst = false
        case .top: axis = .vertical; newFirst = true
        case .bottom: axis = .vertical; newFirst = false
        }
        let pair = newFirst ? (TileTree.tile(new), TileTree.tile(target)) : (.tile(target), .tile(new))
        return replacing(target, with: .split(Split(id: id, axis: axis, ratio: 0.5, first: pair.0, second: pair.1)))
    }

    /// Removes the leaf for `tile`; its sibling takes the parent's place.
    /// Returns nil when the tree held only that tile.
    func removing(_ tile: UUID) -> TileTree? {
        switch self {
        case .tile(let id): return id == tile ? nil : self
        case .split(var split):
            let first = split.first.removing(tile)
            let second = split.second.removing(tile)
            guard let first else { return second }
            guard let second else { return first }
            split.first = first
            split.second = second
            return .split(split)
        }
    }

    func split(_ id: UUID) -> Split? {
        switch self {
        case .tile: return nil
        case .split(let split):
            if split.id == id { return split }
            return split.first.split(id) ?? split.second.split(id)
        }
    }

    /// Replaces the split `id`, children included, with `node`.
    func replacingSplit(_ id: UUID, with node: TileTree) -> TileTree {
        guard case .split(var split) = self else { return self }
        if split.id == id { return node }
        split.first = split.first.replacingSplit(id, with: node)
        split.second = split.second.replacingSplit(id, with: node)
        return .split(split)
    }

    func settingRatio(_ id: UUID, to ratio: Double) -> TileTree {
        switch self {
        case .tile: return self
        case .split(var split):
            if split.id == id {
                split.ratio = min(max(ratio, Self.ratioRange.lowerBound), Self.ratioRange.upperBound)
                return .split(split)
            }
            split.first = split.first.settingRatio(id, to: ratio)
            split.second = split.second.settingRatio(id, to: ratio)
            return .split(split)
        }
    }

    /// Sizes every tile evenly along each axis. A split's ratio follows how
    /// many tiles each side lays out along the split's own axis, so A | (B | C)
    /// gives each tile a third while A | (B / C) gives A half.
    func equalized() -> TileTree {
        switch self {
        case .tile: return self
        case .split(var split):
            let first = split.first.weight(along: split.axis)
            let second = split.second.weight(along: split.axis)
            split.ratio = Double(first) / Double(first + second)
            split.first = split.first.equalized()
            split.second = split.second.equalized()
            return .split(split)
        }
    }

    /// The number of tiles laid out side by side along `axis`.
    private func weight(along axis: SplitAxis) -> Int {
        switch self {
        case .tile: return 1
        case .split(let split):
            guard split.axis == axis else { return 1 }
            return split.first.weight(along: axis) + split.second.weight(along: axis)
        }
    }

    // MARK: Layout

    /// A split's placement: its whole rectangle and the gap between children.
    struct SplitFrame: Equatable {
        let id: UUID
        let axis: SplitAxis
        let frame: CGRect
        let gap: CGRect
    }

    struct Layout: Equatable {
        var tiles: [UUID: CGRect] = [:]
        var splits: [SplitFrame] = []
    }

    /// Lays the tree out in `bounds`, separating siblings by `gap`. Lengths
    /// are rounded so tile edges sit on whole points.
    func layout(in bounds: CGRect, gap: CGFloat) -> Layout {
        var result = Layout()
        place(in: bounds, gap: gap, into: &result)
        return result
    }

    private func place(in bounds: CGRect, gap: CGFloat, into result: inout Layout) {
        switch self {
        case .tile(let id):
            result.tiles[id] = bounds
        case .split(let split):
            let horizontal = split.axis == .horizontal
            let length = horizontal ? bounds.width : bounds.height
            let available = max(0, length - gap)
            let firstLength = (available * split.ratio).rounded()
            let first: CGRect, divider: CGRect, second: CGRect
            if horizontal {
                first = CGRect(x: bounds.minX, y: bounds.minY, width: firstLength, height: bounds.height)
                divider = CGRect(x: first.maxX, y: bounds.minY, width: gap, height: bounds.height)
                second = CGRect(x: divider.maxX, y: bounds.minY, width: available - firstLength, height: bounds.height)
            } else {
                first = CGRect(x: bounds.minX, y: bounds.minY, width: bounds.width, height: firstLength)
                divider = CGRect(x: bounds.minX, y: first.maxY, width: bounds.width, height: gap)
                second = CGRect(x: bounds.minX, y: divider.maxY, width: bounds.width, height: available - firstLength)
            }
            result.splits.append(SplitFrame(id: split.id, axis: split.axis, frame: bounds, gap: divider))
            split.first.place(in: first, gap: gap, into: &result)
            split.second.place(in: second, gap: gap, into: &result)
        }
    }

    // MARK: Spatial navigation

    /// The nearest tile in `direction` from `tile`, judged by on-screen
    /// position rather than tree order. Tiles that share a row (or column)
    /// with `tile` win over ones that only lie diagonally beyond it; among
    /// equally near tiles, the one sharing more of that span wins.
    func neighbor(of tile: UUID, _ direction: TileDirection,
                  in bounds: CGRect = CGRect(x: 0, y: 0, width: 1000, height: 1000)) -> UUID? {
        let frames = layout(in: bounds, gap: 0).tiles
        guard let origin = frames[tile] else { return nil }
        let epsilon: CGFloat = 0.5
        func beyond(_ frame: CGRect) -> Bool {
            switch direction {
            case .left: frame.maxX <= origin.minX + epsilon
            case .right: frame.minX >= origin.maxX - epsilon
            case .up: frame.maxY <= origin.minY + epsilon
            case .down: frame.minY >= origin.maxY - epsilon
            }
        }
        /// Distance across the gap along the direction of travel.
        func reach(_ frame: CGRect) -> CGFloat {
            switch direction {
            case .left: origin.minX - frame.maxX
            case .right: frame.minX - origin.maxX
            case .up: origin.minY - frame.maxY
            case .down: frame.minY - origin.maxY
            }
        }
        /// How much of the perpendicular span the two tiles share.
        func overlap(_ frame: CGRect) -> CGFloat {
            direction.axis == .horizontal
                ? min(frame.maxY, origin.maxY) - max(frame.minY, origin.minY)
                : min(frame.maxX, origin.maxX) - max(frame.minX, origin.minX)
        }
        /// Offset of the candidate's leading edge from the origin's, so
        /// ties go to the tile aligned with the top or leading edge.
        func offset(_ frame: CGRect) -> CGFloat {
            direction.axis == .horizontal ? abs(frame.minY - origin.minY) : abs(frame.minX - origin.minX)
        }
        let candidates = tiles.filter { $0 != tile }.compactMap { id in frames[id].map { (id, $0) } }
            .filter { beyond($0.1) }
        let aligned = candidates.filter { overlap($0.1) > epsilon }
        let pool = aligned.isEmpty ? candidates : aligned
        return pool.min { a, b in
            let reachA = reach(a.1), reachB = reach(b.1)
            if abs(reachA - reachB) > epsilon { return reachA < reachB }
            let overlapA = overlap(a.1), overlapB = overlap(b.1)
            if abs(overlapA - overlapB) > epsilon { return overlapA > overlapB }
            return offset(a.1) < offset(b.1)
        }?.0
    }

    // MARK: Directional resize

    /// Moves the divider of the innermost split on `direction`'s axis that
    /// encloses `tile` by `points` toward `direction`. Returns the tree
    /// unchanged when no split on that axis encloses the tile.
    func resizing(_ tile: UUID, _ direction: TileDirection, by points: CGFloat,
                  in bounds: CGRect, gap: CGFloat) -> TileTree {
        guard let id = enclosingSplit(of: tile, along: direction.axis),
              let split = split(id),
              let frame = layout(in: bounds, gap: gap).splits.first(where: { $0.id == id }) else { return self }
        let length = (split.axis == .horizontal ? frame.frame.width : frame.frame.height) - gap
        guard length > 0 else { return self }
        let delta = Double(points / length)
        let grow = direction == .right || direction == .down
        return settingRatio(id, to: split.ratio + (grow ? delta : -delta))
    }

    /// The innermost split on `axis` that contains `tile`.
    private func enclosingSplit(of tile: UUID, along axis: SplitAxis) -> UUID? {
        guard case .split(let split) = self, contains(tile) else { return nil }
        let inner = split.first.enclosingSplit(of: tile, along: axis) ?? split.second.enclosingSplit(of: tile, along: axis)
        return inner ?? (split.axis == axis ? split.id : nil)
    }
}
