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
    var id: String {
        switch self {
        case .conversation: "conversation"
        case .review: "review"
        case .scratchpad: "scratchpad"
        case .agent(let name): "agent:" + name
        case .file(let path): "file:" + path
        }
    }
    var title: String {
        switch self {
        case .conversation: "Agent"
        case .review: "Diff"
        case .scratchpad: "Scratchpad"
        case .agent(let name): name
        case .file(let path): URL(fileURLWithPath: path).lastPathComponent
        }
    }
    var closable: Bool { self != .conversation }
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
    /// False while the shared composer, not a tile, has the user's attention.
    var contentFocused = true

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
        contentFocused = true
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
        guard let source = tile(containing: pane), tiles[target] != nil, var from = tiles[source],
              let index = from.panes.firstIndex(of: pane) else { return }
        if source == target && (edge == .center || from.panes.count == 1) { return }
        from.panes.remove(at: index)
        if from.selected == pane, !from.panes.isEmpty { from.selected = from.panes[min(index, from.panes.count - 1)] }
        tiles[source] = from
        if from.panes.isEmpty { removeTile(source) }
        if edge == .center {
            tiles[target]?.panes.append(pane)
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
