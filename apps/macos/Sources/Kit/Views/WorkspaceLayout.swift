@preconcurrency import CodeEditSourceEditor
import AppKit
import SwiftUI

/// Space between tiles.
let tileGap: CGFloat = 6
private let tabStripHeight: CGFloat = 34

/// A session's workspace: its split tree of tiles.
struct WorkspaceLayout: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    /// The tiles' coordinate space, shared by layout, tab drags, and drops.
    static let space = "workspace-tiles"
    @State private var tabFrames = TabFrames()

    var body: some View {
        let workspace = state.ui.workspace
        GeometryReader { geometry in
            let bounds = CGRect(origin: .zero, size: geometry.size)
            let layout = workspace.root.layout(in: bounds, gap: tileGap)
            // A zoomed tile fills the workspace; hidden tiles keep their
            // frames so their panes don't resize while out of view.
            let frames = layout.tiles.merging(workspace.zoomedTile.map { [$0: bounds] } ?? [:]) { $1 }
            let running = Set((state.selected?.subagents?.items ?? []).filter { $0.status == "running" }.map(\.name))
            let visible = workspace.visibleTiles
            let resolve: (WorkspacePane, CGPoint) -> TileDropTarget? = { pane, point in
                TileDropTarget.resolve(point, tiles: visible, frames: frames,
                    tabs: { tile in (workspace.tiles[tile]?.panes ?? []).compactMap { tabFrames.frames[$0] } },
                    stripHeight: tabStripHeight, gap: tileGap)
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
                    TileChrome(state: state, workspace: workspace, id: id, running: running, resolve: resolve, tabFrames: tabFrames)
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
                    let focused = workspace.focusedTile == id && workspace.root.tiles.count > 1
                    RoundedRectangle(cornerRadius: 4)
                        .strokeBorder(focused ? theme.focusBorder : theme.border)
                        .frame(width: frame.width, height: frame.height)
                        .offset(x: frame.minX, y: frame.minY)
                        .allowsHitTesting(false)
                }
                TabDragOverlay(workspace: workspace, frames: frames, tabFrames: tabFrames)
            }
            .coordinateSpace(.named(Self.space))
            .background(WorkspaceFocusBridge(workspace: workspace,
                frames: frames.filter { visible.contains($0.key) },
                overlayOpen: { [ui = state.ui] in ui.palette || ui.filePicker }))
            .onGeometryChange(for: CGSize.self) { $0.size } action: {
                workspace.bounds = CGRect(origin: .zero, size: $0)
            }
        }
        .background(SessionMonitors(state: state))
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
        case .hostFile, .localFile: ExternalFilePane(state: state, pane: pane)
        case .image(let id, let filename): ImagePane(state: state, id: id, filename: filename)
        case .browser:
            if let tab = state.ui.workspace.browser(pane) { BrowserPane(workspace: state.ui.workspace, tab: tab) }
        case .terminal:
            if let tab = state.ui.workspace.terminal(pane) { TerminalPane(workspace: state.ui.workspace, tab: tab) }
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
    let tabFrames: TabFrames
    @State private var hovering = false

    var body: some View {
        let tile = workspace.tiles[id]
        let focused = workspace.focusedTile == id
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                TileTabStrip(state: state, workspace: workspace, id: id, focused: focused, running: running,
                             resolve: resolve, tabFrames: tabFrames)
                // The actions keep their place while hidden, so showing them
                // on hover doesn't reflow the tabs.
                let actions = (focused || hovering) && tile?.selected != nil
                HStack(spacing: 2) {
                    let selected = tile?.selected
                    TileIconButton(icon: .squareSplitHorizontal, help: "Split Right") { selected.map { workspace.split($0, .right) } }
                        .disabled(!(selected.map(workspace.canSplit) ?? false))
                    TileIconButton(icon: .squareSplitVertical, help: "Split Down") { selected.map { workspace.split($0, .bottom) } }
                        .disabled(!(selected.map(workspace.canSplit) ?? false))
                    TileIconButton(icon: workspace.zoomedTile == id ? .cornersIn : .cornersOut,
                                   help: workspace.zoomedTile == id ? "Unzoom" : "Zoom") {
                        workspace.focus(tile: id); workspace.toggleZoom()
                    }.disabled(workspace.root.tiles.count < 2)
                }
                .padding(.leading, 8).padding(.trailing, 2)
                .opacity(actions ? 1 : 0)
                .allowsHitTesting(actions)
                .disabled(!actions)
                .accessibilityHidden(!actions)
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

/// A tile's tabs and its add button. Tabs shrink to share the strip
/// (``TileTabWidths``); past that they scroll, with faded edges, the
/// selected tab kept in view, and a count that lists the hidden ones. The
/// add button follows the last tab, and stays beside the count while the
/// tabs scroll.
private struct TileTabStrip: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let state: SessionStore
    let workspace: WorkspaceState
    let id: UUID
    let focused: Bool
    let running: Set<String>
    let resolve: (WorkspacePane, CGPoint) -> TileDropTarget?
    let tabFrames: TabFrames
    @State private var width: CGFloat = 0
    @State private var contentWidth: CGFloat = 0
    @State private var viewportWidth: CGFloat = 0
    @State private var spans: [WorkspacePane: ClosedRange<CGFloat>] = [:]
    @State private var position = ScrollPosition(edge: .leading)
    /// Read only by the views that follow the scrolling, so the tabs don't
    /// re-render as it moves.
    @State private var scroll = TabStripScroll()
    /// Whether the strip keeps the selected tab in view. Scrolling by hand
    /// stops it until another tab is selected.
    @State private var following = true

    var body: some View {
        let tile = workspace.tiles[id]
        let panes = tile?.panes ?? []
        let selected = tile?.selected
        let available = width > 0 ? width - TileAddButton.width : nil
        let scrolls = available.map { contentWidth > $0 + 0.5 } ?? false
        // Until the tabs are measured the strip takes the room it has.
        let fits = !scrolls && contentWidth > 0
        HStack(spacing: 0) {
            ScrollView(.horizontal) {
                TileTabRow(available: available, selected: selected.flatMap(panes.firstIndex(of:))) {
                    ForEach(panes) { pane in
                        WorkspaceTab(state: state, workspace: workspace, pane: pane,
                            selected: selected == pane, focused: focused,
                            running: { if case .agent(let name) = pane { return running.contains(name) }; return false }(),
                            resolve: resolve)
                        .onGeometryChange(for: CGRect.self) { $0.frame(in: .named(WorkspaceLayout.space)) } action: {
                            tabFrames.frames[pane] = $0
                        }
                        .onGeometryChange(for: ClosedRange<CGFloat>.self) {
                            let frame = $0.frame(in: .named(Self.space))
                            return frame.minX...frame.maxX
                        } action: { spans[pane] = $0 }
                    }
                }
                .coordinateSpace(.named(Self.space))
                .frame(maxHeight: .infinity)
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { contentWidth = $0 }
            }
            // `.hidden` still shows a legacy scroller, which also squeezes
            // the tabs, when the system shows scroll bars always.
            .scrollIndicators(.never)
            .scrollPosition($position)
            .onScrollGeometryChange(for: CGFloat.self) { $0.contentOffset.x } action: { [scroll] _, x in scroll.offset = x }
            .onScrollPhaseChange { _, phase in
                if phase == .tracking || phase == .interacting || phase == .decelerating { following = false }
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { viewportWidth = $0 }
            .mask { TileTabStripMask(scroll: scroll, spans: spans, viewport: viewportWidth) }
            .background(HorizontalWheelScrolling())
            // While the tabs fit, the strip is as wide as they are, so the
            // add button follows the last one.
            .frame(maxWidth: fits ? contentWidth : .infinity)
            if scrolls {
                TileTabOverflowMenu(workspace: workspace, panes: panes, scroll: scroll, spans: spans,
                                    viewport: viewportWidth, choose: choose)
            }
            TileAddButton(state: state, workspace: workspace, tile: id)
            if fits { Spacer(minLength: 0) }
        }
        .frame(maxWidth: .infinity)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .onChange(of: panes) { spans = spans.filter { panes.contains($0.key) } }
        .onChange(of: Reveal(pane: selected, span: selected.flatMap { spans[$0] }, viewport: viewportWidth,
                             content: contentWidth), initial: true) { old, new in
            if old.pane != new.pane { following = true }
            guard following else { return }
            // A new selection, or the selected tab growing into its
            // readable width, glides; layout changes follow at once.
            let width = { (span: ClosedRange<CGFloat>?) in span.map { $0.upperBound - $0.lowerBound } }
            reveal(animated: old.pane != new.pane || width(old.span) != width(new.span))
        }
    }

    /// Selects a tab from the overflow menu. The selected tab, scrolled
    /// out of view by hand, is listed too; choosing it brings it back.
    private func choose(_ pane: WorkspacePane) {
        let reselected = workspace.tiles[id]?.selected == pane
        workspace.focus(tile: id)
        workspace.select(pane)
        following = true
        if reselected { reveal(animated: true) }
    }

    /// Scrolls the selected tab into view.
    private func reveal(animated: Bool) {
        guard let pane = workspace.tiles[id]?.selected, let span = spans[pane],
              let x = TabStripOverflow<WorkspacePane>.revealOffset(span, offset: scroll.offset, viewport: viewportWidth,
                  content: contentWidth, leading: TileTabFade.leading, trailing: TileTabFade.trailing) else { return }
        if animated && !reduceMotion {
            withAnimation(.easeOut(duration: 0.15)) { position.scrollTo(x: x) }
        } else {
            position.scrollTo(x: x)
        }
    }

    /// What moving the selected tab into view depends on.
    private struct Reveal: Equatable {
        let pane: WorkspacePane?
        let span: ClosedRange<CGFloat>?
        let viewport: CGFloat
        let content: CGFloat
    }

    private static let space = "TileTabStrip"
}

/// Where a strip is scrolled to.
@Observable private final class TabStripScroll {
    var offset: CGFloat = 0
}

/// Fades the edges of a tile's strip where tabs are hidden.
private struct TileTabStripMask: View {
    let scroll: TabStripScroll
    let spans: [WorkspacePane: ClosedRange<CGFloat>]
    let viewport: CGFloat

    var body: some View {
        let overflow = TabStripOverflow(frames: spans, offset: scroll.offset, viewport: viewport)
        TabStripEdgeMask(hiddenBefore: overflow.hiddenBefore, hiddenAfter: overflow.hiddenAfter,
                         leading: TileTabFade.leading, trailing: TileTabFade.trailing)
    }
}

/// The count of a tile's hidden tabs; choosing one selects it.
private struct TileTabOverflowMenu: View {
    let workspace: WorkspaceState
    let panes: [WorkspacePane]
    let scroll: TabStripScroll
    let spans: [WorkspacePane: ClosedRange<CGFloat>]
    let viewport: CGFloat
    let choose: (WorkspacePane) -> Void

    var body: some View {
        let overflow = TabStripOverflow(frames: spans, offset: scroll.offset, viewport: viewport)
        let hidden = panes.filter(overflow.hides)
        if !hidden.isEmpty {
            Menu {
                ForEach(hidden) { pane in
                    Button { choose(pane) } label: {
                        Label { Text(workspace.title(of: pane)) } icon: { Image(nsImage: menuIcon(pane)) }
                    }
                }
            } label: {
                TabOverflowCount(count: hidden.count)
            }
            .menuStyle(.button)
            .buttonStyle(.plain)
            .menuIndicator(.hidden)
            .fixedSize()
            .help(Self.label(hidden.count))
            .accessibilityLabel(Self.label(hidden.count))
            .padding(.leading, 4)
        }
    }

    private static func label(_ count: Int) -> String {
        "\(count) more tab\(count == 1 ? "" : "s")"
    }

    /// The pane's tab icon, at menu size. A page keeps its own icon.
    private func menuIcon(_ pane: WorkspacePane) -> NSImage {
        if let favicon = workspace.browser(pane)?.favicon, let image = favicon.copy() as? NSImage {
            image.size = NSSize(width: 16, height: 16)
            return image
        }
        return pane.icon.image
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
        // Hover or keyboard focus swaps the icon for the close button in the
        // same slot, so the tab never changes width.
        let showsClose = pane.closable && (hovering || keyboardFocused)
        HStack(spacing: 8) {
            if showsClose {
                Button(action: close) {
                    IconView(.x, size: 12)
                        .foregroundStyle(theme.text)
                        .frame(width: 14, height: 14).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("Close Tab")
                .accessibilityLabel("Close \(workspace.title(of: pane))")
            }
            Button { workspace.select(pane) } label: {
                HStack(spacing: 8) {
                    if !showsClose { icon.frame(width: 14, height: 14) }
                    title
                    if workspace.terminal(pane)?.bell == true {
                        Circle().fill(theme.accent).frame(width: 6, height: 6)
                            .accessibilityLabel("Bell")
                    }
                    if workspace.browser(pane)?.pageDialog != nil {
                        Circle().fill(theme.warning).frame(width: 6, height: 6)
                            .help("This page is waiting for an answer")
                            .accessibilityLabel("Page is waiting for an answer")
                    }
                }
                .frame(maxHeight: .infinity)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .focused($keyboardFocused)
            // Shrunk tabs cut their titles short.
            .help(workspace.title(of: pane))
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
                guard !workspace.dragCancelled(pane) else { return }
                workspace.tabDrag = .init(pane: pane, location: value.location, target: resolve(pane, value.location))
            }
            .onEnded { value in
                guard !workspace.dragCancelled(pane, ending: true) else { return }
                // Resolve at release so the drop matches the pointer, not the last update.
                if let target = resolve(pane, value.location) { workspace.move(pane, to: target) }
                workspace.tabDrag = nil
            })
        .onChange(of: keyboardFocused) { if keyboardFocused { workspace.select(pane) } }
        .contextMenu { WorkspaceTabActions(state: state, workspace: workspace, pane: pane) }
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityActions {
            if pane.closable { Button("Close \(workspace.title(of: pane))", action: close) }
        }
    }

    /// Page and shell titles are capped and trail off; other names keep
    /// both ends.
    @ViewBuilder private var title: some View {
        let capped = workspace.browser(pane) != nil || workspace.terminal(pane) != nil
        Text(workspace.title(of: pane)).lineLimit(1)
            .truncationMode(capped ? .tail : .middle)
            .font(.kit(size: 12, weight: selected ? .semibold : .regular))
            .foregroundStyle(selected ? theme.text : theme.muted)
            // A shrunk tab's title fills its slot, so the tab does too.
            .frame(maxWidth: capped ? 180 : .infinity, alignment: .leading)
    }

    /// Every tab shows its icon so activating one never shifts the strip; a
    /// running subagent shows its spinner in the icon's place. Hover swaps
    /// either for the close button.
    @ViewBuilder private var icon: some View {
        let tint = selected && focused ? theme.accent : theme.muted
        if case .agent = pane, running {
            KitSpinner().accessibilityLabel("Running")
        } else if let favicon = workspace.browser(pane)?.favicon {
            // The site's icon keeps its own colours.
            Image(nsImage: favicon).resizable().interpolation(.high).scaledToFit()
                .frame(width: 14, height: 14).clipShape(RoundedRectangle(cornerRadius: 3))
        } else {
            IconView(pane.icon).foregroundStyle(tint)
        }
    }

    private func close() { state.closePane(pane) }
}

/// The drop preview and a floating copy of the dragged tab, drawn above
/// every tile. Reading the drag here keeps pointer updates from
/// re-evaluating the whole workspace.
private struct TabDragOverlay: View {
    @Environment(\.mica) private var theme
    let workspace: WorkspaceState
    let frames: [UUID: CGRect]
    let tabFrames: TabFrames

    var body: some View {
        ZStack(alignment: .topLeading) {
            Color.clear
            if let drag = workspace.tabDrag {
                if let target = drag.target, let index = target.index, let frame = frames[target.tile] {
                    // An insertion marker in the strip, between tabs.
                    let x = insertionX(index, in: target.tile, frame: frame)
                    RoundedRectangle(cornerRadius: 1)
                        .fill(theme.accent)
                        .frame(width: 2, height: 20)
                        .offset(x: x - 1, y: frame.minY + (tabStripHeight - 20) / 2)
                } else if let target = drag.target, let frame = frames[target.tile] {
                    let zone = target.preview(in: frame)
                    RoundedRectangle(cornerRadius: 4)
                        .fill(theme.accent.opacity(0.14))
                        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.accent, lineWidth: 1.5))
                        .frame(width: zone.width, height: zone.height)
                        .offset(x: zone.minX, y: zone.minY)
                        .animation(.easeOut(duration: 0.12), value: target)
                }
                Text(workspace.title(of: drag.pane))
                    .lineLimit(1).frame(maxWidth: 220)
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
        .background(DragCancellation(active: workspace.tabDrag != nil) { [workspace] in
            guard workspace.tabDrag != nil else { return false }
            workspace.cancelTabDrag()
            return true
        })
    }

    /// The x position of the gap before tab `index`, or after the last tab.
    private func insertionX(_ index: Int, in tile: UUID, frame: CGRect) -> CGFloat {
        let tabs = (workspace.tiles[tile]?.panes ?? []).compactMap { tabFrames.frames[$0] }
        let x = index < tabs.count ? tabs[index].minX : (tabs.last?.maxX ?? frame.minX + 6)
        return min(max(x, frame.minX + 2), frame.maxX - 2)
    }
}

extension WorkspacePane {
    /// The icon its tab shows when nothing more specific, such as a page's
    /// own icon, replaces it.
    var icon: Icon {
        switch self {
        case .agent: .robot
        case .conversation: .chatCircle
        case .review: .gitDiff
        case .scratchpad: .note
        case .file, .hostFile, .localFile: .file
        case .image: .image
        case .browser: .browser
        case .terminal: .terminalWindow
        }
    }
}

/// Where each tab sits in the workspace's coordinate space, for strip
/// drops. Not observed: geometry changes shouldn't re-render the workspace.
final class TabFrames {
    var frames: [WorkspacePane: CGRect] = [:]
}

private struct WorkspaceTabActions: View {
    let state: SessionStore
    let workspace: WorkspaceState
    let pane: WorkspacePane

    var body: some View {
        if case .file(let file) = pane {
            FileTabMenuItems(actions: FileTabActions(file: file, cwd: state.selected?.cwd, gate: state.thisMacGate))
            Divider()
        } else if case .hostFile(let file) = pane, file.hasPrefix("/") {
            FileTabMenuItems(actions: FileTabActions(file: file, cwd: nil, gate: state.thisMacGate))
            Divider()
        } else if case .localFile(let file) = pane {
            FileTabMenuItems(actions: FileTabActions(file: file, cwd: nil, gate: .available))
            Divider()
        }
        if pane.isMarkdown {
            Button(workspace.showsMarkdownSource(pane) ? "Show Markdown Preview" : "Show Markdown Source") {
                workspace.toggleMarkdownSource(pane)
            }
            Divider()
        }
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
            Button("Close Tab") { state.closePane(pane) }
        }
    }
}

/// A file tab's own menu items. Copy Path works on any server; Reveal in
/// Finder and Open in Default App work on this Mac's files, so on another
/// machine's server they stay in the menu, dimmed with the reason.
struct FileTabMenuItems: View {
    let actions: FileTabActions

    var body: some View {
        Button("Copy Path") { actions.copyPath() }.disabled(actions.path == nil)
        Divider()
        item("Reveal in Finder") { actions.revealInFinder() }
        item("Open in Default App") { actions.openInDefaultApp() }
    }

    /// A menu item, with the reason it is unavailable as its subtitle.
    private func item(_ title: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
            if let reason = actions.localReason { Text("⊘ " + reason) }
        }
        .disabled(actions.localReason != nil)
    }
}

/// A 24 pt icon button for a tile's tab strip.
private struct TileIconButton: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let icon: Icon
    let help: String
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            IconView(icon)
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
                    .workspaceFocusTarget(.scratchpad)
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
    private var pane: WorkspacePane { .file(path) }
    private var active: Bool { workspace.isVisible(pane) }
    /// Whether the file shows its preview: Markdown, not switched to source.
    private var rendered: Bool { pane.isMarkdown && !workspace.showsMarkdownSource(pane) }
    /// The comments the preview shows and takes, on the file as loaded.
    private var previewAnnotations: MarkdownPreviewAnnotations? {
        guard let preview, let client = state.catalogClient as? any AnnotationClient else { return nil }
        return MarkdownPreviewAnnotations(document: AnnotationDocument(file: preview), state: state.annotationState,
            client: client, session: state.selectedID, canAnnotate: !loading && entry?.stale == false)
    }
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
                if pane.isMarkdown {
                    MarkdownModeToggle(workspace: workspace, pane: pane)
                }
                if state.catalogClient is any AnnotationClient && !rendered {
                    Button(state.annotationState.replacement == nil && state.annotationState.selectionDraft == nil ? "Annotate selection" : "Use selected range") { beginAnnotation() }
                        .font(.kit(size: 11)).buttonStyle(.plain)
                        .help("Annotate up to 200 selected lines")
                        .disabled(selectedLines == nil || loading || entry?.stale == true ||
                            state.annotationState.pending || state.annotationState.editor != nil)
                }
                Button { revision += 1 } label: { IconView(.arrowClockwise, size: 12) }
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
                        Label { Text("File changed") } icon: { IconView(.warning, size: 12) }
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
                    Text((state.annotationState.evidenceConflict == .staleFile ? "Refreshing the changed file. " : "") +
                        (rendered ? "Choose a block with + for your comment, or select lines in Source." :
                            "Select a new source range for your comment."))
                    Spacer()
                    Button("Cancel") { state.annotationState.cancelEditor() }
                }.font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
            }
            Group {
                if rendered, let source = preview?.content ?? workspace.fixture.files.first(where: { $0.path == path })?.content {
                    MarkdownFileText(workspace: workspace, pane: pane, source: source, cwd: state.selected?.cwd,
                                     annotations: previewAnnotations)
                } else if let preview {
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
            }
            .workspaceFocusTarget(pane)
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
