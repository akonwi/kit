import Foundation

/// A session workspace's tile layout as restoration intent: its split tree
/// and ratios, each tile's tabs and selected tab, the focused tile, and zoom.
/// Panes are recorded by identity; tile and split identities are regenerated
/// on restore.
struct TileArrangement: Codable, Equatable {
    indirect enum Node: Equatable {
        case tile(Tile)
        case split(Split)
    }

    struct Tile: Codable, Equatable {
        /// Pane identities, as `WorkspacePane.id`.
        var panes: [String]
        var selected: String
    }

    struct Split: Codable, Equatable {
        var axis: Axis
        var ratio: Double
        var first: Node
        var second: Node
    }

    enum Axis: String, Codable { case horizontal, vertical }

    var root: Node
    /// The focused tile's index in reading order.
    var focused: Int
    var zoomed: Bool
    /// Each browser tab's address, by pane identity. Tabs without one
    /// restore blank. Absent when no browser tab has an address.
    var browsers: [String: String]? = nil

    /// Restore keeps at most this many tiles and panes, and this split depth.
    static let maximumTiles = 32
    static let maximumPanes = 200
    static let maximumDepth = 16

    /// One Agent tile, unzoomed: the layout a session starts with.
    var isDefault: Bool {
        root == .tile(Tile(panes: [WorkspacePane.conversation.id], selected: WorkspacePane.conversation.id)) && !zoomed
    }
}

/// A tile encodes as its tabs; a split as its axis, ratio, and children.
extension TileArrangement.Node: Codable {
    init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let split = try? container.decode(TileArrangement.Split.self) {
            self = .split(split)
        } else {
            self = .tile(try container.decode(TileArrangement.Tile.self))
        }
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .tile(let tile): try container.encode(tile)
        case .split(let split): try container.encode(split)
        }
    }
}
