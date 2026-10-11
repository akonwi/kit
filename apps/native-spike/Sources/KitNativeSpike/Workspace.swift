import SwiftUI

private let gap: CGFloat = 6

/// The session's split tree of tiles. Tiles are laid out recursively; the gap
/// between siblings is the divider's hit area, so no tile content (and no
/// AppKit view) sits under it.
struct WorkspaceView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session

    var body: some View {
        ZStack(alignment: .topLeading) {
            if let zoomed = session.zoomedTile {
                TileView(session: session, tileID: zoomed)
            } else {
                NodeView(session: session, node: session.root)
            }
            if let drag = model.drag {
                DropOverlay(drag: drag)
            }
        }
        .coordinateSpace(.named("workspace"))
    }
}

private struct NodeView: View {
    let session: Session
    let node: TileNode

    var body: some View {
        switch node {
        case .leaf(let id):
            TileView(session: session, tileID: id)
        case .split(let split):
            SplitView(session: session, split: split)
        }
    }
}

private struct SplitView: View {
    @Environment(AppModel.self) private var model
    let session: Session
    let split: TileNode.Split
    @State private var hover = false

    var body: some View {
        GeometryReader { geo in
            let horizontal = split.axis == .horizontal
            let total = (horizontal ? geo.size.width : geo.size.height) - gap
            let firstLength = (total * split.ratio).rounded()
            let layout = horizontal ? AnyLayout(HStackLayout(spacing: 0)) : AnyLayout(VStackLayout(spacing: 0))
            layout {
                NodeView(session: session, node: split.first)
                    .frame(width: horizontal ? firstLength : nil, height: horizontal ? nil : firstLength)
                Divider(horizontal: horizontal, space: split.id) { location in
                    let length = horizontal ? geo.size.width : geo.size.height
                    let value = (horizontal ? location.x : location.y) - gap / 2
                    session.root = session.root.settingRatio(split.id, value / max(length - gap, 1))
                } onDoubleClick: {
                    session.root = session.root.settingRatio(split.id, 0.5)
                }
                NodeView(session: session, node: split.second)
            }
            .coordinateSpace(.named(split.id))
        }
    }

    private struct Divider: View {
        @Environment(\.theme) private var theme
        let horizontal: Bool
        let space: UUID
        let onDrag: (CGPoint) -> Void
        let onDoubleClick: () -> Void
        @State private var active = false
        @State private var hover = false

        var body: some View {
            Rectangle()
                .fill(Color.clear)
                .frame(width: horizontal ? gap : nil, height: horizontal ? nil : gap)
                .overlay {
                    // A hairline appears while hovering or dragging.
                    Rectangle()
                        .fill(active || hover ? theme.accent.opacity(0.7) : .clear)
                        .frame(width: horizontal ? 2 : nil, height: horizontal ? nil : 2)
                }
                .contentShape(Rectangle())
                .pointerStyle(horizontal ? .columnResize : .rowResize)
                .onHover { hover = $0 }
                .gesture(
                    DragGesture(minimumDistance: 0, coordinateSpace: .named(space))
                        .onChanged { value in
                            active = true
                            onDrag(value.location)
                        }
                        .onEnded { value in active = false; spikeLog("divider released at \(value.location)") }
                )
                .simultaneousGesture(TapGesture(count: 2).onEnded(onDoubleClick))
        }
    }
}

// MARK: Tile

struct TileView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    let tileID: UUID

    var body: some View {
        if let tile = session.tiles[tileID] {
            let focused = session.focusedTile == tileID
            VStack(spacing: 0) {
                TabStrip(session: session, tile: tile, focused: focused)
                ZStack {
                    ForEach(tile.panes, id: \.self) { paneID in
                        if paneID == tile.selected, let pane = model.pane(paneID) {
                            PaneContent(session: session, tileID: tileID, pane: pane, focused: focused)
                        }
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            .background(theme.surface)
            .clipShape(RoundedRectangle(cornerRadius: 4))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(focused ? theme.focusBorder : theme.border))
            .contentShape(Rectangle())
            .simultaneousGesture(TapGesture().onEnded {
                if session.focusedTile != tileID { model.focus(tile: tileID) }
            })
            .onGeometryChange(for: CGRect.self) { $0.frame(in: .named("workspace")) } action: {
                model.tileFrames[tileID] = $0
            }
            .onDisappear { model.tileFrames[tileID] = nil }
        }
    }
}

struct PaneContent: View {
    let session: Session
    let tileID: UUID
    let pane: Pane
    let focused: Bool

    var body: some View {
        switch pane.kind {
        case .agent: AgentPane(session: session, tileID: tileID, focused: focused)
        case .reviewer: ReviewerPane(session: session, tileID: tileID, focused: focused)
        case .diff: DiffPane(session: session)
        case .file(let path): FilePane(path: path)
        case .scratchpad: ScratchpadPane(session: session, tileID: tileID, focused: focused)
        case .terminal, .browser: NativePaneHost(pane: pane)
        }
    }
}

// MARK: Tab strip (style B: underline)

private struct TabStrip: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    let tile: Tile
    let focused: Bool
    @State private var hover = false

    var body: some View {
        HStack(spacing: 0) {
            ForEach(tile.panes, id: \.self) { paneID in
                if let pane = model.pane(paneID) {
                    TabItem(session: session, tile: tile, pane: pane, focused: focused)
                }
            }
            Spacer(minLength: 8)
            if focused || hover {
                HStack(spacing: 2) {
                    IconButton(glyph: .squareSplitHorizontal, help: "Split right (⌘D)") {
                        model.focus(tile: tile.id); model.split(.horizontal)
                    }
                    IconButton(glyph: .squareSplitVertical, help: "Split down (⇧⌘D)") {
                        model.focus(tile: tile.id); model.split(.vertical)
                    }
                    IconButton(glyph: session.zoomedTile == tile.id ? .arrowsIn : .arrowsOut, help: "Zoom (⇧⌘↩)") {
                        model.focus(tile: tile.id); model.toggleZoom()
                    }
                }
                .padding(.trailing, 2)
            } else if let meta = meta {
                meta.padding(.trailing, 8)
            }
        }
        .padding(.horizontal, 6)
        .frame(height: 34)
        .background(alignment: .bottom) { Rectangle().fill(theme.border).frame(height: 1) }
        .onHover { hover = $0 }
    }

    private var meta: AnyView? {
        guard let pane = model.pane(tile.selected), pane.kind == .diff else { return nil }
        return AnyView(HStack(spacing: 8) {
            Text("3 files").foregroundStyle(theme.muted)
            Text("+86").foregroundStyle(theme.success)
            Text("−31").foregroundStyle(theme.danger)
        }.font(Fonts.mono(11)))
    }
}

private struct TabItem: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    let tile: Tile
    let pane: Pane
    let focused: Bool
    @State private var hover = false

    var body: some View {
        let selected = tile.selected == pane.id
        let dragging = model.drag?.pane == pane.id
        HStack(spacing: 8) {
            if pane.kind == .reviewer {
                BrailleSpinner(color: theme.accent)
            } else {
                Icon(glyph: pane.kind.icon, size: 14, color: selected && focused ? theme.accent : theme.muted)
            }
            Text(pane.title)
                .font(Fonts.ui(12, weight: selected ? .semibold : .regular))
                .foregroundStyle(selected ? theme.text : theme.muted)
                .lineLimit(1)
            if pane.kind.closable && (selected || hover) {
                Button { model.close(pane: pane.id, in: tile.id) } label: {
                    Icon(glyph: .x, size: 10, color: theme.muted).frame(width: 14, height: 14).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 10)
        .frame(maxHeight: .infinity)
        .overlay(alignment: .bottom) {
            if selected {
                Rectangle().fill(focused ? theme.accent : theme.strongBorder).frame(height: 2)
            }
        }
        .opacity(dragging ? 0.4 : 1)
        .contentShape(Rectangle())
        .onHover { hover = $0 }
        .onTapGesture { model.select(pane: pane.id, in: tile.id) }
        .gesture(
            DragGesture(minimumDistance: 4, coordinateSpace: .named("workspace"))
                .onChanged { value in
                    var drag = model.drag ?? .init(pane: pane.id, source: tile.id, location: value.location)
                    drag.location = value.location
                    (drag.target, drag.edge) = Self.target(at: value.location, frames: visibleFrames)
                    model.drag = drag
                }
                .onEnded { _ in
                    if let drag = model.drag, let target = drag.target {
                        model.move(pane: drag.pane, from: drag.source, to: target, edge: drag.edge)
                    }
                    model.drag = nil
                }
        )
    }

    /// Frames of the tiles on screen now: the zoomed tile, or every leaf of
    /// the current session's tree. Other sessions' tiles never qualify.
    private var visibleFrames: [UUID: CGRect] {
        let ids = session.zoomedTile.map { [$0] } ?? session.root.tileIDs
        return Dictionary(uniqueKeysWithValues: ids.compactMap { id in model.tileFrames[id].map { (id, $0) } })
    }

    static func target(at point: CGPoint, frames: [UUID: CGRect]) -> (UUID?, DropEdge) {
        guard let (id, frame) = frames.first(where: { $0.value.contains(point) }) else { return (nil, .center) }
        // Over the tab strip is always "add as a tab".
        if point.y < frame.minY + 34 { return (id, .center) }
        let x = (point.x - frame.minX) / frame.width
        let y = (point.y - frame.minY) / frame.height
        let band: CGFloat = 0.25
        let distances: [(DropEdge, CGFloat)] = [(.left, x), (.right, 1 - x), (.top, y), (.bottom, 1 - y)]
        let nearest = distances.min { $0.1 < $1.1 }!
        return (id, nearest.1 < band ? nearest.0 : .center)
    }
}

/// Drop indicator and drag preview, drawn over tiles (and their AppKit views).
private struct DropOverlay: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let drag: AppModel.TabDrag

    var body: some View {
        ZStack(alignment: .topLeading) {
            Color.clear
            if let target = drag.target, let frame = model.tileFrames[target] {
                let rect = zone(frame, drag.edge)
                RoundedRectangle(cornerRadius: 4)
                    .fill(theme.accent.opacity(0.14))
                    .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.accent, lineWidth: 1.5))
                    .frame(width: rect.width, height: rect.height)
                    .offset(x: rect.minX, y: rect.minY)
                    .animation(.easeOut(duration: 0.12), value: drag.edge)
                    .animation(.easeOut(duration: 0.12), value: drag.target)
            }
            if let pane = model.pane(drag.pane) {
                HStack(spacing: 8) {
                    Icon(glyph: pane.kind.icon, size: 14, color: theme.accent)
                    Text(pane.title).font(Fonts.ui(12, weight: .semibold)).foregroundStyle(theme.text)
                }
                .padding(.horizontal, 10)
                .frame(height: 28)
                .background(RoundedRectangle(cornerRadius: 3).fill(theme.raised))
                .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                .shadow(color: .black.opacity(0.3), radius: 8, y: 4)
                .fixedSize()
                .offset(x: drag.location.x + 8, y: drag.location.y + 8)
            }
        }
        .allowsHitTesting(false)
    }

    private func zone(_ f: CGRect, _ edge: DropEdge) -> CGRect {
        switch edge {
        case .center: f.insetBy(dx: 4, dy: 4)
        case .left: CGRect(x: f.minX + 4, y: f.minY + 4, width: f.width / 2 - 6, height: f.height - 8)
        case .right: CGRect(x: f.midX + 2, y: f.minY + 4, width: f.width / 2 - 6, height: f.height - 8)
        case .top: CGRect(x: f.minX + 4, y: f.minY + 4, width: f.width - 8, height: f.height / 2 - 6)
        case .bottom: CGRect(x: f.minX + 4, y: f.midY + 2, width: f.width - 8, height: f.height / 2 - 6)
        }
    }
}
