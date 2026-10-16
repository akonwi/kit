import Foundation
import Observation
@preconcurrency import CodeEditSourceEditor

struct WorkspaceFixture: Decodable {
    let files: [PreviewFile]
    let scratchpad: String
}

struct PreviewFile: Decodable, Identifiable {
    var id: String { path }
    let path: String
    let content: String
    let patch: String
    var beforeContent: String? = nil
}

enum WorkspacePane: Hashable, Identifiable {
    case conversation, review, scratchpad, agent(String), file(String)
    /// A transcript image, by its attachment ID and filename.
    case image(id: String, filename: String)
    var id: String {
        switch self {
        case .conversation: "conversation"
        case .review: "review"
        case .scratchpad: "scratchpad"
        case .agent(let name): "agent:" + name
        case .file(let path): "file:" + path
        case .image(let id, let filename): "image:" + id + ":" + filename
        }
    }
    var title: String {
        switch self {
        case .conversation: "Agent"
        case .review: "Diff"
        case .scratchpad: "Scratchpad"
        case .agent(let name): name
        case .file(let path): URL(fileURLWithPath: path).lastPathComponent
        case .image(_, let filename): filename
        }
    }
    var closable: Bool { self != .conversation }

    /// The pane with this identity, or nil for an unknown or empty one.
    init?(id: String) {
        switch id {
        case "conversation": self = .conversation
        case "review": self = .review
        case "scratchpad": self = .scratchpad
        default:
            if id.hasPrefix("agent:"), id.count > 6 { self = .agent(String(id.dropFirst(6))) }
            else if id.hasPrefix("file:"), id.count > 5 { self = .file(String(id.dropFirst(5))) }
            else if id.hasPrefix("image:") {
                // Attachment IDs never contain a colon; filenames may.
                let rest = id.dropFirst(6)
                guard let colon = rest.firstIndex(of: ":"), colon > rest.startIndex,
                      rest.index(after: colon) < rest.endIndex else { return nil }
                self = .image(id: String(rest[..<colon]), filename: String(rest[rest.index(after: colon)...]))
            }
            else { return nil }
        }
    }
}

/// A leaf of the workspace's split tree: a tab group of panes.
struct WorkspaceTile: Equatable {
    var panes: [WorkspacePane]
    var selected: WorkspacePane
}

@MainActor @Observable
final class WorkspaceState {
    let fixture: WorkspaceFixture
    var fileIndex = FileIndexCache()
    let filePreviews = FilePreviewCache()
    let diff = DiffState()
    /// Every open pane in the order it was opened. Views key pane content by
    /// this list so moving a pane between tiles never remounts it.
    private(set) var panes: [WorkspacePane] = [.conversation]
    private(set) var root: TileTree
    private(set) var tiles: [UUID: WorkspaceTile]
    private(set) var focusedTile: UUID
    /// The tile filling the workspace, if one is zoomed.
    private(set) var zoomedTile: UUID?
    /// The tab being dragged, if any.
    var tabDrag: TabDrag?
    /// A tab whose drag was cancelled with Esc; its gesture ignores the rest
    /// of the drag and drops nothing.
    @ObservationIgnored private(set) var cancelledDrag: WorkspacePane?
    /// The tiles' bounds when last laid out, for spatial focus and keyboard
    /// resizing. Not observed: layout shouldn't re-render the workspace.
    @ObservationIgnored var bounds = CGRect(x: 0, y: 0, width: 1000, height: 1000)

    struct TabDrag: Equatable {
        var pane: WorkspacePane
        /// Pointer location in the workspace's coordinate space.
        var location: CGPoint
        /// Where the tab would land; nil where a drop does nothing.
        var target: TileDropTarget?
    }

    var selected: WorkspacePane { tiles[focusedTile]?.selected ?? .conversation }
    /// Tiles on screen: the zoomed tile, or every leaf.
    var visibleTiles: [UUID] { zoomedTile.map { [$0] } ?? root.tiles }

    func tile(containing pane: WorkspacePane) -> UUID? {
        root.tiles.first { tiles[$0]?.panes.contains(pane) == true }
    }

    /// Whether the pane is the selected tab of a tile on screen.
    func isVisible(_ pane: WorkspacePane) -> Bool {
        guard let tile = tile(containing: pane) else { return false }
        return tiles[tile]?.selected == pane && visibleTiles.contains(tile)
    }

    func focus(tile: UUID) {
        guard tiles[tile] != nil else { return }
        focusedTile = tile
    }

    /// Selects an open pane and focuses its tile, ending a zoom that hides it.
    func select(_ pane: WorkspacePane) {
        guard let tile = tile(containing: pane) else { return }
        tiles[tile]?.selected = pane
        if let zoomedTile, zoomedTile != tile { self.zoomedTile = nil }
        focus(tile: tile)
    }

    /// Opens a pane as a tab in the focused tile, or selects it if it is open.
    func open(_ pane: WorkspacePane) {
        if !panes.contains(pane) {
            panes.append(pane)
            tiles[focusedTile]?.panes.append(pane)
            zoomedTile = nil
        }
        select(pane)
    }

    /// Opens a pane beside `anchor`'s tile: in the neighbouring tile, preferring
    /// the one to its right, or in a new tile on its right when it has none.
    /// A pane that is already open is selected where it is.
    func open(_ pane: WorkspacePane, beside anchor: WorkspacePane) {
        guard !panes.contains(pane) else { select(pane); return }
        guard let home = tile(containing: anchor) else { open(pane); return }
        let neighbor = [TileDirection.right, .down, .left, .up].lazy
            .compactMap { self.root.neighbor(of: home, $0, in: self.bounds) }.first
            ?? root.tiles.first { $0 != home }
        panes.append(pane)
        zoomedTile = nil
        if let neighbor {
            tiles[neighbor]?.panes.append(pane)
            select(pane)
        } else {
            tiles[home]?.panes.append(pane)
            move(pane, to: home, edge: .right)
        }
    }

    /// Closes a pane. A tile left without tabs is removed, and its sibling
    /// takes its place.
    func close(_ pane: WorkspacePane) {
        guard pane.closable, let id = tile(containing: pane), var tile = tiles[id],
              let index = tile.panes.firstIndex(of: pane) else { return }
        if case .file(let path) = pane { filePreviews.remove(path) }
        panes.removeAll { $0 == pane }
        tile.panes.remove(at: index)
        if tile.panes.isEmpty {
            removeTile(id)
        } else {
            if tile.selected == pane { tile.selected = tile.panes[min(index, tile.panes.count - 1)] }
            tiles[id] = tile
        }
    }

    /// Moves a pane into `target`'s tab group, or onto one of its edges as a
    /// new tile. The source tile is removed if the move empties it.
    func move(_ pane: WorkspacePane, to target: UUID, edge: TileDropEdge) {
        move(pane, to: TileDropTarget(tile: target, edge: edge))
    }

    /// Moves a pane to a drop target. A strip drop with an index inserts the
    /// tab at that gap, which reorders it within its own tile.
    func move(_ pane: WorkspacePane, to drop: TileDropTarget) {
        guard canMove(pane, to: drop), let source = tile(containing: pane), var from = tiles[source],
              let index = from.panes.firstIndex(of: pane) else { return }
        let target = drop.tile, edge = drop.edge
        from.panes.remove(at: index)
        if from.selected == pane, !from.panes.isEmpty { from.selected = from.panes[min(index, from.panes.count - 1)] }
        tiles[source] = from
        if from.panes.isEmpty { removeTile(source) }
        if edge == .center {
            // Gaps count the moved tab while it was still in its own strip.
            let gap = source == target && (drop.index ?? 0) > index ? drop.index! - 1 : drop.index
            let count = tiles[target]?.panes.count ?? 0
            tiles[target]?.panes.insert(pane, at: min(max(gap ?? count, 0), count))
            tiles[target]?.selected = pane
            focus(tile: target)
        } else {
            let id = UUID()
            tiles[id] = WorkspaceTile(panes: [pane], selected: pane)
            root = root.inserting(id, beside: target, on: edge)
            focus(tile: id)
        }
        zoomedTile = nil
    }

    /// Whether moving `pane` to `target` would change anything. A tab can't
    /// join its own tile, and a tile's only tab can't split away from it.
    func canMove(_ pane: WorkspacePane, to target: TileDropTarget) -> Bool {
        guard let source = tile(containing: pane), let from = tiles[source], tiles[target.tile] != nil,
              let position = from.panes.firstIndex(of: pane) else { return false }
        guard source == target.tile else { return true }
        if target.edge != .center { return from.panes.count > 1 }
        // Within its own strip, a tab moves only if it lands in another gap.
        guard let index = target.index else { return false }
        return index != position && index != position + 1
    }

    /// Moves a pane into a new tile beside its own, on `edge`.
    func split(_ pane: WorkspacePane, _ edge: TileDropEdge) {
        guard let tile = tile(containing: pane) else { return }
        move(pane, to: tile, edge: edge)
    }

    /// Whether `split` would move anything: the pane's tile has other tabs.
    func canSplit(_ pane: WorkspacePane) -> Bool {
        tile(containing: pane).flatMap { tiles[$0] }.map { $0.panes.count > 1 } ?? false
    }

    /// Gathers every pane into one tile, Agent first, selecting `pane`.
    func joinTiles(selecting pane: WorkspacePane) {
        let id = UUID()
        let ordered = [WorkspacePane.conversation] + root.tiles.flatMap { tiles[$0]?.panes ?? [] }.filter { $0 != .conversation }
        tiles = [id: WorkspaceTile(panes: ordered, selected: ordered.contains(pane) ? pane : .conversation)]
        root = .tile(id)
        zoomedTile = nil
        focus(tile: id)
    }

    func toggleZoom() {
        zoomedTile = zoomedTile == nil && root.tiles.count > 1 ? focusedTile : nil
        focus(tile: focusedTile)
    }

    func equalize() { root = root.equalized() }

    /// Evens out one split and the splits nested in it.
    func equalize(split id: UUID) {
        guard let split = root.split(id) else { return }
        let even = TileTree.split(split).equalized()
        root = root.replacingSplit(id, with: even)
    }

    func setRatio(_ ratio: Double, for split: UUID) { root = root.settingRatio(split, to: ratio) }

    // MARK: Restoration

    /// The layout as restoration intent.
    var arrangement: TileArrangement {
        func node(_ tree: TileTree) -> TileArrangement.Node {
            switch tree {
            case .tile(let id):
                let tile = tiles[id] ?? WorkspaceTile(panes: [], selected: .conversation)
                return .tile(.init(panes: tile.panes.map(\.id), selected: tile.selected.id))
            case .split(let split):
                return .split(.init(axis: split.axis == .horizontal ? .horizontal : .vertical,
                                    ratio: split.ratio, first: node(split.first), second: node(split.second)))
            }
        }
        return TileArrangement(root: node(root), focused: root.tiles.firstIndex(of: focusedTile) ?? 0,
                               zoomed: zoomedTile != nil)
    }

    /// Replaces the layout with a saved one. Unknown and repeated panes are
    /// dropped, tiles left without tabs collapse into their siblings, and the
    /// Agent tab is kept, in the first tile if the layout lost it.
    func restore(_ arrangement: TileArrangement) {
        var seen = Set<WorkspacePane>()
        var restored: [UUID: WorkspaceTile] = [:]
        func build(_ node: TileArrangement.Node, depth: Int) -> TileTree? {
            switch node {
            case .tile(let saved):
                guard restored.count < TileArrangement.maximumTiles else { return nil }
                let panes = saved.panes.compactMap(WorkspacePane.init(id:)).filter { pane in
                    seen.count < TileArrangement.maximumPanes && seen.insert(pane).inserted
                }
                guard let first = panes.first else { return nil }
                let id = UUID()
                let selected = WorkspacePane(id: saved.selected).flatMap { panes.contains($0) ? $0 : nil } ?? first
                restored[id] = WorkspaceTile(panes: panes, selected: selected)
                return .tile(id)
            case .split(let split):
                guard depth < TileArrangement.maximumDepth else { return nil }
                let first = build(split.first, depth: depth + 1)
                let second = build(split.second, depth: depth + 1)
                // Unwrap each side on its own: older compilers flag the
                // autoclosure in `first ?? second` as a data race.
                guard let first else { return second }
                guard let second else { return first }
                let ratio = split.ratio.isFinite ? split.ratio : 0.5
                return .split(.init(id: UUID(), axis: split.axis == .horizontal ? .horizontal : .vertical,
                                    ratio: min(max(ratio, TileTree.ratioRange.lowerBound), TileTree.ratioRange.upperBound),
                                    first: first, second: second))
            }
        }
        guard let tree = build(arrangement.root, depth: 0), let leading = tree.tiles.first else { return }
        if !seen.contains(.conversation) { restored[leading]?.panes.insert(.conversation, at: 0) }
        root = tree
        tiles = restored
        panes = tree.tiles.flatMap { restored[$0]?.panes ?? [] }
        let order = tree.tiles
        focusedTile = order[min(max(arrangement.focused, 0), order.count - 1)]
        zoomedTile = arrangement.zoomed && order.count > 1 ? focusedTile : nil
    }

    // MARK: Keyboard commands

    /// Focuses the nearest tile in `direction`, ending a zoom.
    func focus(_ direction: TileDirection) {
        guard let neighbor = root.neighbor(of: focusedTile, direction, in: bounds) else { return }
        zoomedTile = nil
        focus(tile: neighbor)
    }

    func canFocus(_ direction: TileDirection) -> Bool {
        root.neighbor(of: focusedTile, direction, in: bounds) != nil
    }

    /// Tiles never shrink below this from the keyboard, matching divider drags.
    static let minimumTileSize = CGSize(width: 160, height: 120)
    static let resizeStep: CGFloat = 40

    /// Moves the focused tile's nearest divider on `direction`'s axis one step
    /// toward `direction`. A step that would shrink a tile below the minimum
    /// size is refused.
    func resize(_ direction: TileDirection) {
        guard zoomedTile == nil else { return }
        let resized = root.resizing(focusedTile, direction, by: Self.resizeStep, in: bounds, gap: tileGap)
        guard resized != root else { return }
        let before = root.layout(in: bounds, gap: tileGap).tiles
        let after = resized.layout(in: bounds, gap: tileGap).tiles
        // A tile already below the minimum, as in a small window, may still grow.
        let fits = after.allSatisfy { id, frame in
            let old = before[id] ?? frame
            return (frame.width >= Self.minimumTileSize.width || frame.width >= old.width)
                && (frame.height >= Self.minimumTileSize.height || frame.height >= old.height)
        }
        if fits { root = resized }
    }

    /// Moves the focused tile's selected tab into the neighboring tile in
    /// `direction`. At the workspace's edge, the tab splits into a new tile on
    /// that side instead, if its tile has others.
    func moveSelectedTab(_ direction: TileDirection) {
        guard let target = moveTarget(direction) else { return }
        move(selected, to: target)
    }

    func canMoveSelectedTab(_ direction: TileDirection) -> Bool { moveTarget(direction) != nil }

    private func moveTarget(_ direction: TileDirection) -> TileDropTarget? {
        let target: TileDropTarget
        if let neighbor = root.neighbor(of: focusedTile, direction, in: bounds) {
            target = TileDropTarget(tile: neighbor, edge: .center)
        } else {
            let edge: TileDropEdge = switch direction {
            case .left: .left
            case .right: .right
            case .up: .top
            case .down: .bottom
            }
            target = TileDropTarget(tile: focusedTile, edge: edge)
        }
        return canMove(selected, to: target) ? target : nil
    }

    /// Selects the tab `offset` places from the focused tile's selected tab,
    /// wrapping around.
    func selectAdjacentTab(_ offset: Int) {
        guard let tile = tiles[focusedTile], tile.panes.count > 1,
              let index = tile.panes.firstIndex(of: tile.selected) else { return }
        let count = tile.panes.count
        select(tile.panes[((index + offset) % count + count) % count])
    }

    func canSelectAdjacentTab() -> Bool { (tiles[focusedTile]?.panes.count ?? 0) > 1 }

    /// Ends a tab drag without dropping it.
    func cancelTabDrag() {
        guard let drag = tabDrag else { return }
        cancelledDrag = drag.pane
        tabDrag = nil
    }

    /// Whether the drag of `pane` was cancelled. Ending the gesture clears it.
    func dragCancelled(_ pane: WorkspacePane, ending: Bool = false) -> Bool {
        guard cancelledDrag == pane else { return false }
        if ending { cancelledDrag = nil }
        return true
    }

    private func removeTile(_ id: UUID) {
        guard let remaining = root.removing(id) else { return }
        root = remaining
        tiles[id] = nil
        if zoomedTile == id { zoomedTile = nil }
        if focusedTile == id, let first = remaining.tiles.first { focusedTile = first }
    }

    var scratchpadState: ScratchpadState
    var scratchpadPosition = SourceEditorState()
    var scratchpad: String {
        get { scratchpadState.draft }
        set { scratchpadState.edit(newValue) }
    }
    var selectedFile = ""
    var changesVisible = true
    var notes: [String: String] = [:]
    var editingLine: Int?
    var noteDraft = ""

    init(demo: Bool = true) {
        let tile = UUID()
        root = .tile(tile)
        tiles = [tile: WorkspaceTile(panes: [.conversation], selected: .conversation)]
        focusedTile = tile
        fixture = WorkspaceFixture(files: [], scratchpad: demo ? "# Scratchpad\n\n" : "")
        scratchpadState = ScratchpadState(demo: demo)
        selectedFile = fixture.files.first(where: { !$0.patch.isEmpty })?.path ?? ""
    }

    func closeScratchpad(client: any ScratchpadClient, session: String) async {
        if scratchpadState.dirty {
            guard await scratchpadState.save(client: client, session: session) else { select(.scratchpad); return }
        }
        close(.scratchpad)
    }

    func invalidateDirectory() {
        fileIndex = FileIndexCache()
        filePreviews.clear()
        diff.invalidate()
        for pane in panes { if case .file = pane { close(pane) } }
        selectedFile = ""
        editingLine = nil
    }

    func noteKey(_ line: Int) -> String { "\(selectedFile):\(line)" }

    func saveNote() {
        guard let line = editingLine else { return }
        let value = noteDraft.trimmingCharacters(in: .whitespacesAndNewlines)
        if value.isEmpty { notes.removeValue(forKey: noteKey(line)) }
        else { notes[noteKey(line)] = value }
        editingLine = nil
        noteDraft = ""
    }

    func editNote(line: Int) {
        editingLine = line
        noteDraft = notes[noteKey(line)] ?? ""
    }
}
