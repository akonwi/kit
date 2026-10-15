import AppKit
import Observation
import SwiftUI
import WebKit

// MARK: Panes

enum PaneKind: Equatable {
    case agent, scratchpad, diff, reviewer, file(String), terminal, browser(URL)

    var icon: Ph {
        switch self {
        case .agent: .chatCircle
        case .scratchpad: .notePencil
        case .diff: .gitDiff
        case .reviewer: .robot
        case .file: .file
        case .terminal: .terminalWindow
        case .browser: .globe
        }
    }

    /// Hosted by an AppKit view that must survive tab switches, moves and zoom.
    var isNative: Bool {
        switch self {
        case .terminal, .browser: true
        default: false
        }
    }

    var closable: Bool { self != .agent }
}

@Observable final class Pane: Identifiable {
    let id = UUID()
    let kind: PaneKind
    var title: String

    init(_ kind: PaneKind, title: String) {
        self.kind = kind
        self.title = title
    }
}

// MARK: Split tree

enum SplitAxis { case horizontal, vertical } // horizontal = side by side

indirect enum TileNode: Equatable {
    case leaf(UUID)
    case split(Split)

    struct Split: Equatable {
        let id: UUID
        var axis: SplitAxis
        var ratio: CGFloat
        var first: TileNode
        var second: TileNode
    }

    var tileIDs: [UUID] {
        switch self {
        case .leaf(let id): [id]
        case .split(let s): s.first.tileIDs + s.second.tileIDs
        }
    }

    func contains(_ tile: UUID) -> Bool { tileIDs.contains(tile) }

    /// Replaces a leaf with a new node.
    func replacing(_ tile: UUID, with node: TileNode) -> TileNode {
        switch self {
        case .leaf(let id): return id == tile ? node : self
        case .split(var s):
            s.first = s.first.replacing(tile, with: node)
            s.second = s.second.replacing(tile, with: node)
            return .split(s)
        }
    }

    /// Removes a leaf; its sibling takes the parent's place.
    func removing(_ tile: UUID) -> TileNode? {
        switch self {
        case .leaf(let id): return id == tile ? nil : self
        case .split(var s):
            let first = s.first.removing(tile)
            let second = s.second.removing(tile)
            guard let first else { return second }
            guard let second else { return first }
            s.first = first
            s.second = second
            return .split(s)
        }
    }

    func settingRatio(_ split: UUID, _ ratio: CGFloat) -> TileNode {
        switch self {
        case .leaf: return self
        case .split(var s):
            if s.id == split { s.ratio = min(max(ratio, 0.15), 0.85) }
            s.first = s.first.settingRatio(split, ratio)
            s.second = s.second.settingRatio(split, ratio)
            return .split(s)
        }
    }

    /// Resets every split, nested ones included, to an even share.
    func equalized() -> TileNode {
        switch self {
        case .leaf: return self
        case .split(var s):
            s.ratio = 0.5
            s.first = s.first.equalized()
            s.second = s.second.equalized()
            return .split(s)
        }
    }
}

struct Tile: Identifiable, Equatable {
    let id: UUID
    var panes: [UUID]
    var selected: UUID
}

enum DropEdge: Equatable { case center, left, right, top, bottom }

// MARK: Session

enum SessionStatus: Equatable { case idle, running, waiting, finished }

struct Toast: Identifiable, Equatable {
    enum Variant { case info, warning, error }
    let id = UUID()
    var variant: Variant
    var title: String
    var detail: String?
    var plugin: String?
    var action: String?
    var persistent = false
    var remaining: Double = 10
}

struct Question: Equatable {
    struct Option: Equatable { var title: String; var detail: String }
    var title: String
    var prompt: String
    var options: [Option]
    var step: Int
    var steps: Int
    var highlighted = 0
}

@Observable final class Session: Identifiable {
    let id = UUID()
    var title: String
    var project: String
    var directory: String
    var branch: String
    var pr: Int?
    var status: SessionStatus
    var root: TileNode
    var tiles: [UUID: Tile]
    var focusedTile: UUID
    var zoomedTile: UUID?
    var toasts: [Toast] = []
    var toastsExpanded = false
    var question: Question?
    var activity: String? = nil
    var thinkingExpanded = false
    var commentChips: [String] = []
    var draft = ""

    init(title: String, project: String, directory: String, branch: String, pr: Int?, status: SessionStatus,
         root: TileNode, tiles: [Tile], focused: UUID) {
        self.title = title
        self.project = project
        self.directory = directory
        self.branch = branch
        self.pr = pr
        self.status = status
        self.root = root
        self.tiles = Dictionary(uniqueKeysWithValues: tiles.map { ($0.id, $0) })
        self.focusedTile = focused
    }

    func tile(containing pane: UUID) -> UUID? {
        tiles.values.first { $0.panes.contains(pane) }?.id
    }
}

// MARK: App model

@Observable final class AppModel {
    var theme: Theme = .micaDark { didSet { ShellTheme.apply(theme) } }
    var sideTabs = true
    var sessions: [Session] = []
    var currentID: UUID
    var panes: [UUID: Pane] = [:]
    var paletteOpen = false { didSet { spikeLog("palette \(paletteOpen ? "open" : "closed")") } }
    /// Tab drag in progress (in-window gesture, not system drag and drop).
    var drag: TabDrag?
    /// Tile frames in the workspace coordinate space, for drop targeting.
    @ObservationIgnored var tileFrames: [UUID: CGRect] = [:]
    /// Bumped to ask the focus bridge to move AppKit's first responder.
    var focusGeneration = 0

    struct TabDrag: Equatable {
        var pane: UUID
        var source: UUID
        var location: CGPoint
        var target: UUID?
        var edge: DropEdge = .center
    }

    var current: Session { sessions.first { $0.id == currentID }! }

    init() {
        currentID = UUID()
        sessions = Fixtures.sessions(self)
        currentID = sessions[0].id
    }

    func pane(_ id: UUID) -> Pane? { panes[id] }

    @discardableResult
    func makePane(_ kind: PaneKind, title: String) -> UUID {
        let pane = Pane(kind, title: title)
        panes[pane.id] = pane
        return pane.id
    }

    // MARK: Focus

    func focus(tile: UUID, in session: Session? = nil) {
        let session = session ?? current
        guard session.tiles[tile] != nil else { return }
        spikeLog("focus tile \(tile.uuidString.prefix(4)) pane=\(session.tiles[tile].flatMap { panes[$0.selected]?.title } ?? "?")")
        session.focusedTile = tile
        focusGeneration += 1
    }

    /// Called when AppKit focus moved on its own (click into a terminal).
    func nativeFocused(pane: UUID) {
        spikeLog("native focus \(panes[pane]?.title ?? "?")")
        guard let tile = current.tile(containing: pane), current.focusedTile != tile else { return }
        current.focusedTile = tile
    }

    func select(pane: UUID, in tile: UUID) {
        current.tiles[tile]?.selected = pane
        focus(tile: tile)
    }

    // MARK: Commands

    func open(_ kind: PaneKind, title: String) {
        let session = current
        let id = makePane(kind, title: title)
        session.tiles[session.focusedTile]?.panes.append(id)
        session.tiles[session.focusedTile]?.selected = id
        session.zoomedTile = nil
        focus(tile: session.focusedTile)
    }

    func split(_ axis: SplitAxis, kind: PaneKind? = nil, title: String = "Terminal") {
        let session = current
        let source = session.focusedTile
        let id = makePane(kind ?? .terminal, title: title)
        let tile = Tile(id: UUID(), panes: [id], selected: id)
        session.tiles[tile.id] = tile
        session.root = session.root.replacing(source, with: .split(.init(
            id: UUID(), axis: axis, ratio: 0.5, first: .leaf(source), second: .leaf(tile.id))))
        session.zoomedTile = nil
        focus(tile: tile.id)
    }

    func closeSelected() {
        let session = current
        guard let tile = session.tiles[session.focusedTile] else { return }
        close(pane: tile.selected, in: tile.id)
    }

    func close(pane: UUID, in tileID: UUID) {
        let session = current
        guard var tile = session.tiles[tileID], let kind = panes[pane]?.kind, kind.closable else { return }
        tile.panes.removeAll { $0 == pane }
        NativeViews.shared.release(pane)
        panes[pane] = nil
        if tile.panes.isEmpty {
            removeTile(tileID, in: session)
        } else {
            if tile.selected == pane { tile.selected = tile.panes.last! }
            session.tiles[tileID] = tile
            focus(tile: tileID)
        }
    }

    private func removeTile(_ tileID: UUID, in session: Session) {
        guard session.tiles.count > 1, let root = session.root.removing(tileID) else { return }
        session.root = root
        session.tiles[tileID] = nil
        if session.zoomedTile == tileID { session.zoomedTile = nil }
        if session.focusedTile == tileID { focus(tile: root.tileIDs.first!, in: session) }
    }

    func toggleZoom() {
        let session = current
        session.zoomedTile = session.zoomedTile == nil && session.tiles.count > 1 ? session.focusedTile : nil
        focus(tile: session.focusedTile)
    }

    func equalize() { current.root = current.root.equalized() }

    /// Moves a pane into a tile (center) or onto one of its edges (split).
    func move(pane: UUID, from source: UUID, to target: UUID, edge: DropEdge) {
        let session = current
        spikeLog("move \(panes[pane]?.title ?? "?") -> \(target.uuidString.prefix(4)) \(edge)")
        guard var from = session.tiles[source], session.tiles[target] != nil else {
            spikeLog("move rejected: tile not in session")
            return
        }
        if source == target && (edge == .center || from.panes.count == 1) { return }
        from.panes.removeAll { $0 == pane }
        if from.selected == pane, let last = from.panes.last { from.selected = last }
        session.tiles[source] = from

        if edge == .center {
            session.tiles[target]?.panes.append(pane)
            session.tiles[target]?.selected = pane
            if from.panes.isEmpty { removeTile(source, in: session) }
            focus(tile: target)
            return
        }
        let tile = Tile(id: UUID(), panes: [pane], selected: pane)
        session.tiles[tile.id] = tile
        let axis: SplitAxis = edge == .left || edge == .right ? .horizontal : .vertical
        let before = edge == .left || edge == .top
        session.root = session.root.replacing(target, with: .split(.init(
            id: UUID(), axis: axis, ratio: 0.5,
            first: before ? .leaf(tile.id) : .leaf(target),
            second: before ? .leaf(target) : .leaf(tile.id))))
        if from.panes.isEmpty { removeTile(source, in: session) }
        session.zoomedTile = nil
        focus(tile: tile.id)
    }

    func switchSession(_ index: Int) {
        guard sessions.indices.contains(index) else { return }
        currentID = sessions[index].id
        if sessions[index].status == .finished { sessions[index].status = .idle }
        focusGeneration += 1
    }

    func newSession() {
        let agent = makePane(.agent, title: "Agent")
        let tile = Tile(id: UUID(), panes: [agent], selected: agent)
        let session = Session(
            title: "New session", project: "KIT", directory: "~/Developer/agent/kit", branch: "main", pr: nil,
            status: .idle, root: .leaf(tile.id), tiles: [tile], focused: tile.id)
        sessions.insert(session, at: 3)
        currentID = session.id
        focusGeneration += 1
    }

    // MARK: Feedback

    func toast(_ toast: Toast) {
        current.toasts.insert(toast, at: 0)
    }

    func demoToasts() {
        let samples: [Toast] = [
            Toast(variant: .info, title: "Session compacted", detail: "Context went from 182k to 41k tokens."),
            Toast(variant: .warning, title: "Images not attached", detail: "Sonnet 4.6 doesn't accept images: diagram.png."),
            Toast(variant: .error, title: "Plugin command failed", detail: "autoresearch: experiment runner exited with status 2.",
                  plugin: "autoresearch"),
            Toast(variant: .error, title: "Session connection lost", detail: "Reconnecting failed after 3 attempts.",
                  action: "Reconnect", persistent: true),
            Toast(variant: .info, title: "Copied to clipboard"),
        ]
        toast(samples[current.toasts.count % samples.count])
    }

    func askDemo() {
        let session = current
        session.question = Question(
            title: "Set up the 0.40 release", prompt: "Which channel?",
            options: [
                .init(title: "Stable", detail: "Tag, publish the binary and update Homebrew"),
                .init(title: "Beta", detail: "Tag and publish; Homebrew stays on 0.39"),
                .init(title: "Draft", detail: "Only create the release notes"),
            ], step: 0, steps: 4)
        session.status = .waiting
        focus(tile: session.root.tileIDs.first { session.tiles[$0]?.panes.contains { panes[$0]?.kind == .agent } == true }
              ?? session.focusedTile)
    }

    func answer(_ index: Int?) {
        let session = current
        guard var q = session.question else { return }
        if let index { q.highlighted = index }
        if index == nil || q.step + 1 >= q.steps {
            session.question = nil
            session.status = .running
            return
        }
        q.step += 1
        q.prompt = ["Which channel?", "What should the notes lead with?", "Who should get the announcement?",
                    "Anything else for the notes?"][q.step]
        q.highlighted = 0
        session.question = q
    }
}
