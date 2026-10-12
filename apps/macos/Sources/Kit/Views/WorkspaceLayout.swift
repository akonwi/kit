@preconcurrency import CodeEditSourceEditor
import AppKit
import SwiftUI

/// Space between tiles, and between the tiles and the composer.
let tileGap: CGFloat = 6
private let tabStripHeight: CGFloat = 34

/// A session's workspace: its split tree of tiles above the shared composer.
struct WorkspaceLayout: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    /// The tiles' coordinate space, shared by layout, tab drags, and drops.
    static let space = "workspace-tiles"

    var body: some View {
        let workspace = state.ui.workspace
        VStack(spacing: tileGap) {
            GeometryReader { geometry in
                let bounds = CGRect(origin: .zero, size: geometry.size)
                let layout = workspace.root.layout(in: bounds, gap: tileGap)
                // A zoomed tile fills the workspace; hidden tiles keep their
                // frames so their panes don't resize while out of view.
                let frames = layout.tiles.merging(workspace.zoomedTile.map { [$0: bounds] } ?? [:]) { $1 }
                let running = Set((state.selected?.subagents?.items ?? []).filter { $0.status == "running" }.map(\.name))
                let visible = workspace.visibleTiles
                let resolve: (WorkspacePane, CGPoint) -> TileDropTarget? = { pane, point in
                    TileDropTarget.resolve(point, tiles: visible, frames: frames, stripHeight: tabStripHeight, gap: tileGap)
                        .flatMap { workspace.canMove(pane, to: $0) ? $0 : nil }
                }
                ZStack(alignment: .topLeading) {
                    if workspace.zoomedTile == nil {
                        ForEach(layout.splits, id: \.id) { split in
                            divider(split, workspace: workspace)
                        }
                    }
                    ForEach(workspace.visibleTiles, id: \.self) { id in
                        let frame = frames[id] ?? .zero
                        TileChrome(state: state, workspace: workspace, id: id, running: running, resolve: resolve)
                            .frame(width: frame.width, height: frame.height)
                            .offset(x: frame.minX, y: frame.minY)
                    }
                    // Pane identity is independent of its tile. Moving, splitting,
                    // or zooming changes geometry and visibility without
                    // remounting editors.
                    ForEach(workspace.panes) { pane in
                        let tile = workspace.tile(containing: pane)
                        let content = Self.contentFrame(tile.flatMap { frames[$0] } ?? .zero)
                        let visible = workspace.isVisible(pane)
                        paneBody(pane)
                            .id(state.serverID + "|" + state.selectedID + "|" + pane.id)
                            .frame(width: content.width, height: content.height)
                            .clipShape(UnevenRoundedRectangle(bottomLeadingRadius: 3, bottomTrailingRadius: 3))
                            .offset(x: content.minX, y: content.minY)
                            .opacity(visible ? 1 : 0)
                            .allowsHitTesting(visible).disabled(!visible).accessibilityHidden(!visible)
                            .simultaneousGesture(TapGesture().onEnded { if let tile { workspace.focus(tile: tile) } })
                    }
                    // Borders sit above pane content so the focus ring stays whole.
                    ForEach(workspace.visibleTiles, id: \.self) { id in
                        let frame = frames[id] ?? .zero
                        let focused = workspace.contentFocused && workspace.focusedTile == id && workspace.root.tiles.count > 1
                        RoundedRectangle(cornerRadius: 4)
                            .strokeBorder(focused ? theme.focusBorder : theme.border)
                            .frame(width: frame.width, height: frame.height)
                            .offset(x: frame.minX, y: frame.minY)
                            .allowsHitTesting(false)
                    }
                    TabDragOverlay(workspace: workspace, frames: frames)
                }
                .coordinateSpace(.named(Self.space))
            }
            SessionComposerRegion(state: state)
                .background(theme.surface)
                .clipShape(RoundedRectangle(cornerRadius: 4))
                .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
                .simultaneousGesture(TapGesture().onEnded { workspace.contentFocused = false })
        }
    }

    /// The part of a tile below its tab strip, inside its border.
    static func contentFrame(_ tile: CGRect) -> CGRect {
        CGRect(x: tile.minX + 1, y: tile.minY + tabStripHeight,
               width: max(0, tile.width - 2), height: max(0, tile.height - tabStripHeight - 1))
    }

    private func divider(_ split: TileTree.SplitFrame, workspace: WorkspaceState) -> some View {
        let horizontal = split.axis == .horizontal
        let offset = horizontal ? split.gap.minX - split.frame.minX : split.gap.minY - split.frame.minY
        let length = (horizontal ? split.frame.width : split.frame.height) - tileGap
        return TileDivider(axis: split.axis, offset: offset, color: theme.accent) { position in
            guard length > 0 else { return }
            // Keep both sides usable: at least 160 pt wide or 120 pt tall.
            let minimum = min(horizontal ? 160 : 120, length / 2)
            let first = min(max(position - tileGap / 2, minimum), length - minimum)
            workspace.setRatio(Double(first / length), for: split.id)
        } onDoubleClick: {
            workspace.equalize(split: split.id)
        }
        .frame(width: split.gap.width, height: split.gap.height)
        .offset(x: split.gap.minX, y: split.gap.minY)
    }

    @ViewBuilder private func paneBody(_ pane: WorkspacePane) -> some View {
        switch pane {
        case .conversation: SessionView(state: state)
        case .review: ReviewPane(state: state)
        case .scratchpad: ScratchpadPane(state: state)
        case .agent(let name): AgentPane(state: state, name: name)
        case .file(let path): FilePane(state: state, path: path)
        }
    }
}

/// A tile's surface and tab strip. Pane content is laid over it separately.
private struct TileChrome: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    let workspace: WorkspaceState
    let id: UUID
    let running: Set<String>
    let resolve: (WorkspacePane, CGPoint) -> TileDropTarget?
    @State private var hovering = false

    var body: some View {
        let tile = workspace.tiles[id]
        let focused = workspace.contentFocused && workspace.focusedTile == id
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                ScrollView(.horizontal) {
                    HStack(spacing: 0) {
                        ForEach(tile?.panes ?? []) { pane in
                            WorkspaceTab(state: state, workspace: workspace, pane: pane,
                                selected: tile?.selected == pane, focused: focused,
                                running: { if case .agent(let name) = pane { return running.contains(name) }; return false }(),
                                resolve: resolve)
                        }
                    }.frame(maxHeight: .infinity)
                }.scrollIndicators(.hidden)
                if let selected = tile?.selected, focused || hovering {
                    HStack(spacing: 2) {
                        TileIconButton(symbol: "rectangle.split.2x1", help: "Split Right") { workspace.split(selected, .right) }
                            .disabled(!workspace.canSplit(selected))
                        TileIconButton(symbol: "rectangle.split.1x2", help: "Split Down") { workspace.split(selected, .bottom) }
                            .disabled(!workspace.canSplit(selected))
                        TileIconButton(symbol: workspace.zoomedTile == id ? "arrow.down.right.and.arrow.up.left" : "arrow.up.left.and.arrow.down.right",
                                       help: workspace.zoomedTile == id ? "Unzoom" : "Zoom") {
                            workspace.focus(tile: id); workspace.toggleZoom()
                        }.disabled(workspace.root.tiles.count < 2)
                    }.padding(.leading, 8).padding(.trailing, 2)
                }
            }
            .padding(.horizontal, 6)
            .frame(height: tabStripHeight)
            .overlay(alignment: .bottom) { Rule() }
            Spacer(minLength: 0)
        }
        .background(theme.surface)
        .clipShape(RoundedRectangle(cornerRadius: 4))
        .contentShape(Rectangle())
        .onHover { hovering = $0 }
        .simultaneousGesture(TapGesture().onEnded { workspace.focus(tile: id) })
    }
}

private struct WorkspaceTab: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    let workspace: WorkspaceState
    let pane: WorkspacePane
    let selected: Bool
    /// Whether this tab's tile is focused.
    let focused: Bool
    let running: Bool
    let resolve: (WorkspacePane, CGPoint) -> TileDropTarget?
    @State private var hovering = false
    @FocusState private var keyboardFocused: Bool

    var body: some View {
        HStack(spacing: 8) {
            Button { workspace.select(pane) } label: {
                HStack(spacing: 8) {
                    icon.frame(width: 14, height: 14)
                    Text(pane.title).lineLimit(1).truncationMode(.middle)
                        .font(.kit(size: 12, weight: selected ? .semibold : .regular))
                        .foregroundStyle(selected ? theme.text : theme.muted)
                }
                .frame(maxHeight: .infinity)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .focused($keyboardFocused)
            if pane.closable && (selected || hovering || keyboardFocused) {
                Button(action: close) {
                    Image(systemName: "xmark").font(.system(size: 9, weight: .medium))
                        .foregroundStyle(theme.muted)
                        .frame(width: 14, height: 14).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Close \(pane.title)")
            }
        }
        .padding(.horizontal, 10)
        .frame(maxHeight: .infinity)
        .overlay(alignment: .bottom) {
            if selected { Rectangle().fill(focused ? theme.accent : theme.strongBorder).frame(height: 2) }
        }
        .contentShape(Rectangle())
        .opacity(workspace.tabDrag?.pane == pane ? 0.4 : 1)
        .onHover { hovering = $0 }
        .simultaneousGesture(DragGesture(minimumDistance: 4, coordinateSpace: .named(WorkspaceLayout.space))
            .onChanged { value in
                workspace.tabDrag = .init(pane: pane, location: value.location, target: resolve(pane, value.location))
            }
            .onEnded { value in
                // Resolve at release so the drop matches the pointer, not the last update.
                if let target = resolve(pane, value.location) { workspace.move(pane, to: target.tile, edge: target.edge) }
                workspace.tabDrag = nil
            })
        .onChange(of: keyboardFocused) { if keyboardFocused { workspace.select(pane) } }
        .contextMenu { WorkspaceTabActions(state: state, workspace: workspace, pane: pane) }
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    /// Paper shows an icon on the selected tab only; a running subagent keeps
    /// its spinner either way.
    @ViewBuilder private var icon: some View {
        let tint = selected && focused ? theme.accent : theme.muted
        if case .agent = pane, running {
            KitSpinner().accessibilityLabel("Running")
        } else if selected {
            switch pane {
            case .agent: RobotTabIcon().foregroundStyle(tint)
            case .conversation: Image(systemName: "bubble.left").font(.system(size: 12)).foregroundStyle(tint)
            case .review: Image(systemName: "chevron.left.forwardslash.chevron.right").font(.system(size: 11)).foregroundStyle(tint)
            case .scratchpad: Image(systemName: "note.text").font(.system(size: 12)).foregroundStyle(tint)
            case .file: Image(systemName: "doc").font(.system(size: 12)).foregroundStyle(tint)
            }
        }
    }

    private func close() { WorkspaceTabActions.close(pane, state: state, workspace: workspace) }
}

/// The drop preview and a floating copy of the dragged tab, drawn above
/// every tile. Reading the drag here keeps pointer updates from
/// re-evaluating the whole workspace.
private struct TabDragOverlay: View {
    @Environment(\.mica) private var theme
    let workspace: WorkspaceState
    let frames: [UUID: CGRect]

    var body: some View {
        ZStack(alignment: .topLeading) {
            Color.clear
            if let drag = workspace.tabDrag {
                if let target = drag.target, let frame = frames[target.tile] {
                    let zone = target.preview(in: frame)
                    RoundedRectangle(cornerRadius: 4)
                        .fill(theme.accent.opacity(0.14))
                        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.accent, lineWidth: 1.5))
                        .frame(width: zone.width, height: zone.height)
                        .offset(x: zone.minX, y: zone.minY)
                        .animation(.easeOut(duration: 0.12), value: target)
                }
                Text(drag.pane.title)
                    .font(.kit(size: 12, weight: .semibold))
                    .foregroundStyle(theme.text)
                    .padding(.horizontal, 10)
                    .frame(height: 28)
                    .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
                    .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                    .shadow(color: .black.opacity(0.3), radius: 8, y: 4)
                    .fixedSize()
                    .offset(x: drag.location.x + 10, y: drag.location.y + 10)
            }
        }
        .allowsHitTesting(false)
    }
}

private struct WorkspaceTabActions: View {
    let state: SessionStore
    let workspace: WorkspaceState
    let pane: WorkspacePane

    var body: some View {
        Button("Split Right") { workspace.split(pane, .right) }.disabled(!workspace.canSplit(pane))
        Button("Split Down") { workspace.split(pane, .bottom) }.disabled(!workspace.canSplit(pane))
        if workspace.root.tiles.count > 1 {
            Button("Join All Tabs") { workspace.joinTiles(selecting: pane) }
            Divider()
            Button(workspace.zoomedTile == nil ? "Zoom Tile" : "Unzoom Tile") {
                if let tile = workspace.tile(containing: pane) { workspace.focus(tile: tile) }
                workspace.toggleZoom()
            }
            Button("Equalize Tiles") { workspace.equalize() }
        }
        if pane.closable {
            Divider()
            Button("Close Tab") { Self.close(pane, state: state, workspace: workspace) }
        }
    }

    /// Closes a tab, saving the scratchpad first so a draft isn't lost.
    static func close(_ pane: WorkspacePane, state: SessionStore, workspace: WorkspaceState) {
        if pane == .scratchpad, let client = state.scratchpadClient {
            Task { await workspace.closeScratchpad(client: client, session: state.selectedID) }
        } else { workspace.close(pane) }
    }
}

/// A 24 pt icon button for a tile's tab strip.
private struct TileIconButton: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let symbol: String
    let help: String
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 12))
                .foregroundStyle(theme.muted)
                .frame(width: 24, height: 24)
                .background(hovering && enabled ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .opacity(enabled ? 1 : 0.45)
        .onHover { hovering = $0 }
        .help(help)
        .accessibilityLabel(help)
    }
}

/// Phosphor Robot (regular), rendered from the bundled vector asset.
private struct RobotTabIcon: View {
    var body: some View {
        if let url = Bundle.module.url(forResource: "robot", withExtension: "svg"), let image = NSImage(contentsOf: url) {
            Image(nsImage: image).renderingMode(.template).resizable().scaledToFit()
        }
    }
}

struct ScratchpadPane: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    private var workspace: WorkspaceState { state.ui.workspace }
    private var pad: ScratchpadState { workspace.scratchpadState }
    var body: some View {
        VStack(spacing: 0) {
            if pad.status == .conflict {
                HStack {
                    Text("Shared scratchpad changed. Your draft is preserved.")
                    Spacer()
                    Button("Review changes") { pad.review() }
                }.font(.kit(size: 11)).foregroundStyle(theme.warning).padding(12)
                Rule()
            }
            if pad.reviewing {
                ScrollView([.vertical, .horizontal]) {
                    HStack {
                        Text(pad.reviewText).font(.kit(size: 12, design: .monospaced))
                            .textSelection(.enabled).fixedSize(horizontal: true, vertical: false)
                        Spacer(minLength: 0)
                    }.frame(maxWidth: .infinity, alignment: .leading).padding(16)
                }.accessibilityLabel("Scratchpad changes")
                Rule()
                HStack {
                    Button("Keep editing") { pad.keepEditing() }
                    Button("Use shared") { pad.useShared() }
                    Spacer()
                    Button("Replace shared with mine") {
                        guard let client = state.scratchpadClient else { return }
                        Task { await pad.replaceReviewed(client: client, session: state.selectedID) }
                    }
                }.padding(12)
            } else {
                @Bindable var workspace = workspace
                ScratchpadEditor(text: $workspace.scratchpad, documentVersion: pad.documentVersion,
                    editorState: $workspace.scratchpadPosition)
            }
            if pad.status != .saved {
                Rule()
                HStack {
                    Spacer()
                    switch pad.status {
                    case .loading: Text("Loading…")
                    case .saved: EmptyView()
                    case .unsaved: Text("Unsaved")
                    case .saving: Text("Saving…")
                    case .conflict: Text("Conflict")
                    case .failed(let message):
                        Text(message).lineLimit(1)
                        Button("Retry") {
                            guard let client = state.scratchpadClient else { return }
                            Task { _ = await pad.save(client: client, session: state.selectedID) }
                        }
                    }
                }.font(.kit(size: 10)).foregroundStyle(theme.muted).padding(12)
            }
        }
        .task(id: state.selectedID) {
            guard let client = state.scratchpadClient else { return }
            pad.bind(client: client, session: state.selectedID)
            await pad.load(client: client, session: state.selectedID)
        }
    }
}

struct FilePane: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    let path: String
    @State private var revision = 0
    @State private var position = SourceEditorState()
    private var entry: FilePreviewCache.Entry? { workspace.filePreviews.entries[path] }
    private var preview: WireWorkspaceFileRead? { entry?.preview }
    private var error: String? { entry?.error }
    private var loading: Bool { entry?.loading == true }
    private var workspace: WorkspaceState { state.ui.workspace }
    private var active: Bool { workspace.isVisible(.file(path)) }
    private var request: String {
        state.serverID + "|" + state.selectedID + "|" + (state.selected?.cwd ?? "") + "|" + String(active) + "|" + String(revision) + "|" + String(workspace.filePreviews.revision)
    }

    private var selectedLines: ClosedRange<Int>? {
        guard let preview, let ranges = position.cursorPositions, ranges.count == 1 else { return nil }
        return AnnotationSelection.lines(in: preview.content, selection: ranges[0].range)
    }
    private func beginAnnotation() {
        guard let preview, let lines = selectedLines else { return }
        state.annotationState.begin(anchor: .init(kind: .value0, workspaceFile: .init(
            workspaceId: preview.workspace.workspaceId, path: preview.path, fileRevision: preview.revision,
            startLine: lines.lowerBound, endLine: lines.upperBound), workingTreeDiff: nil))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(path).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted).lineLimit(1).truncationMode(.middle)
                Spacer()
                if loading { KitSpinner() }
                if state.catalogClient is any AnnotationClient {
                    Button(state.annotationState.replacement == nil && state.annotationState.selectionDraft == nil ? "Annotate selection" : "Use selected range") { beginAnnotation() }
                        .font(.kit(size: 11)).buttonStyle(.plain)
                        .help("Annotate up to 200 selected lines")
                        .disabled(selectedLines == nil || loading || entry?.stale == true ||
                            state.annotationState.pending || state.annotationState.editor != nil)
                }
                Button { revision += 1 } label: { Image(systemName: "arrow.clockwise") }
                    .buttonStyle(.plain).accessibilityLabel("Refresh file").help("Refresh file").disabled(loading)
            }.padding(.horizontal, 20).padding(.vertical, 10)
            Rule()
            if let error {
                Text((preview == nil ? "" : "Showing an older preview. ") + error)
                    .font(.kit(size: 12)).foregroundStyle(theme.muted).padding(12)
                Rule()
            }
            ForEach(state.annotationState.records.filter { $0.path == path && $0.stale && !$0.isDiff }) { note in
                Button {
                    state.annotationState.inspected = note
                } label: {
                    HStack {
                        Label("File changed", systemImage: "exclamationmark.triangle")
                        Text(note.body).lineLimit(1)
                        Spacer()
                        Text("View captured source")
                    }.font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
                }.buttonStyle(.plain)
            }
            if let anchor = state.annotationState.editor?.anchor.workspaceFile,
               anchor.path == path, let preview, anchor.fileRevision != preview.revision {
                HStack {
                    Text("File changed. Your unsaved comment is preserved.")
                    Spacer()
                    Button("Select new range") { state.annotationState.reselectEditor() }
                    Button("Cancel") { state.annotationState.cancelEditor() }
                }.font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
            }
            if state.annotationState.replacement?.path == path || state.annotationState.selectionDraft?.path == path {
                HStack {
                    Text(state.annotationState.evidenceConflict == .staleFile ?
                        "Refreshing the changed file. Select a new source range for your comment." :
                        "Select a new source range for your comment.")
                    Spacer()
                    Button("Cancel") { state.annotationState.cancelEditor() }
                }.font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
            }
            if let preview {
                if let client = state.catalogClient as? any AnnotationClient {
                    AnnotatedFileEditor(file: preview, position: $position, annotations: state.annotationState,
                        client: client, session: state.selectedID, canAnnotate: !loading && entry?.stale == false)
                        .id(preview.workspace.workspaceId + preview.revision).clipped()
                } else {
                    ReadOnlyFileEditor(path: path, source: preview.content, position: $position)
                }
            } else if let file = workspace.fixture.files.first(where: { $0.path == path }) {
                ReadOnlyFileEditor(path: path, source: file.content)
            } else {
                Text(loading ? "Loading file…" : error == nil ? "File preview unavailable" : "Refresh to try again")
                    .foregroundStyle(theme.muted).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            Text((entry?.stale == true && preview != nil ? "Read-only · Preview may be out of date" : "Read-only") + (preview?.truncated == true ? " · Preview truncated" : ""))
                .font(.kit(size: 10)).foregroundStyle(theme.muted).padding(12)
        }.onChange(of: state.annotationState.evidenceConflict) {
            guard active, state.annotationState.selectionDraft?.path == path,
                  state.annotationState.evidenceConflict == .staleFile || state.annotationState.evidenceConflict == .staleWorkspace else { return }
            workspace.filePreviews.invalidate(path)
        }.task(id: request) {
            guard active, !state.isDemo, let client = state.catalogClient as? any WorkspaceFileClient else { return }
            guard let cwd = state.selected?.cwd else { return }
            do { try await Task.sleep(for: .milliseconds(200)) } catch { return }
            await workspace.filePreviews.load(path, session: state.selectedID, cwd: cwd,
                client: client, force: revision > 0)
            if let annotationClient = state.catalogClient as? any AnnotationClient {
                let annotations = state.annotationState
                try? await annotations.refresh(client: annotationClient, session: state.selectedID)
                if let id = annotations.revealed, let stale = annotations.records.first(where: { $0.id == id && $0.stale }) {
                    annotations.inspected = stale; annotations.revealed = nil
                }
            }
        }
    }
}
