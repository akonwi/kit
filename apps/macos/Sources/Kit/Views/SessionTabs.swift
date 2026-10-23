import SwiftUI

/// The selected session's bar above its tiles: its title (with side tabs),
/// working directory, Git branch, pull request, and plugin footer items.
struct SessionBar: View {
    @Environment(\.mica) private var theme
    @Environment(\.openURL) private var openURL
    let tab: SessionTab
    /// With top tabs, the title is on the tab and the bar is a shorter row.
    var showsTitle = true

    var body: some View {
        let session = tab.session
        HStack(spacing: 24) {
            HStack(spacing: 14) {
                if showsTitle {
                    Text(tab.title)
                        .font(.kit(size: 13, weight: .semibold))
                        .lineLimit(1).truncationMode(.tail)
                        .layoutPriority(2)
                }
                if let server = tab.serverLabel {
                    ServerChip(name: server, attention: tab.serverIssue != nil, reconnecting: tab.reconnecting)
                }
                if !tab.isLauncher, session?.pluginFooter?.locationHidden != true {
                    // The pull request opens in the session's browser.
                    WorkspaceLocation(session: session)
                        .environment(\.openURL, tab.store.map { .workspace($0.ui.workspace, beside: .conversation) } ?? openURL)
                }
            }
            .layoutPriority(1)
            if !tab.isLauncher {
                // Plugin items take what the title and location leave.
                PluginFooterView(session: session)
            }
        }
        // Without plugin items the row holds only the title and location,
        // which stay at the leading edge.
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.leading, 8).padding(.trailing, 14)
        .frame(height: showsTitle ? SessionChrome.rowHeight : SessionChrome.compactRowHeight)
        .background(WindowTitlebarRegion())
    }
}

enum SessionChrome {
    /// The window's top row: traffic lights, session tabs, and the session bar.
    static let rowHeight: CGFloat = 44
    /// The session bar below top tabs.
    static let compactRowHeight: CGFloat = 32
}

/// Side-tab layout: session tabs as a tree of families, each session under
/// the session it came from.
struct SessionSidebar: View {
    @Environment(\.mica) private var theme
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement
    @State private var drag = SessionTreeDrag()

    var body: some View {
        let rows = model.rows
        let dragged = drag.family(in: rows)
        let numbers = model.tabNumberHints(.side)
        VStack(spacing: 0) {
            HStack {
                Spacer()
                ChromeIconButton(icon: .plus, help: "New session tab (⌘T)") { model.openLauncher() }
            }
            .padding(.horizontal, 8)
            .frame(height: SessionChrome.rowHeight)
            .background(WindowTitlebarRegion())
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                        let next = rows.indices.contains(index + 1) ? rows[index + 1] : nil
                        SessionTreeRowView(row: row,
                                           rails: SessionTreeRails(depth: row.depth, next: next?.depth),
                                           selected: row.tab?.id == model.selectedTabID,
                                           number: numbers.number(for: row.content),
                                           model: model, drag: drag)
                        .opacity(dragged.contains(row.id) ? 0.4 : 1)
                    }
                }
                .coordinateSpace(.named(SessionTreeDrag.space))
                .overlay(alignment: .topLeading) { SessionTreeDropMarker(drag: drag, rows: rows) }
                .padding(.top, 8)
            }
            .scrollIndicators(.never)
            HStack(spacing: 8) {
                AppUpdateChip()
                Spacer(minLength: 0)
                SessionTabPlacementToggle(placement: $placement)
            }
            .padding(.leading, 8).padding(.trailing, 4)
            .frame(height: 32)
        }
        .padding(.horizontal, 8).padding(.bottom, 10)
        .frame(width: 248)
        .background(DragCancellation(active: drag.id != nil) { drag.cancel() })
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session tabs")
    }
}

/// A family being dragged among its siblings in the sidebar.
@MainActor @Observable
final class SessionTreeDrag {
    struct Target: Equatable {
        let id: String
        let after: Bool
    }

    static let space = "SessionSidebar"
    /// The dragged row.
    private(set) var id: String?
    private(set) var target: Target?
    /// Where each visible row sits in the list. Not observed: geometry
    /// changes shouldn't re-render the list.
    @ObservationIgnored var frames: [String: CGRect] = [:]
    /// A row whose drag was cancelled with Esc; its gesture ignores the rest
    /// of the drag.
    @ObservationIgnored private var cancelled: String?

    /// The dragged family's row IDs.
    func family(in rows: [SessionTreeRow]) -> Set<String> {
        guard let id, let index = rows.firstIndex(where: { $0.id == id }) else { return [] }
        return Set(SessionTree.family(index, in: rows).map { rows[$0].id })
    }

    func update(_ id: String, y: CGFloat, rows: [SessionTreeRow]) {
        guard cancelled != id else { return }
        self.id = id
        let next = resolve(id, y: y, rows: rows)
        if target != next { target = next }
    }

    /// Ends the drag, returning where to drop.
    func end(_ id: String, y: CGFloat, rows: [SessionTreeRow]) -> Target? {
        defer { self.id = nil; target = nil; cancelled = nil }
        guard cancelled != id else { return nil }
        return resolve(id, y: y, rows: rows)
    }

    func cancel() -> Bool {
        guard let id else { return false }
        cancelled = id
        self.id = nil
        target = nil
        return true
    }

    /// The sibling family under the pointer, and which half of it.
    func resolve(_ id: String, y: CGFloat, rows: [SessionTreeRow]) -> Target? {
        guard let index = rows.firstIndex(where: { $0.id == id }) else { return nil }
        let parent = SessionTree.parent(of: index, in: rows)
        let spans: [(id: String, span: ClosedRange<CGFloat>)] = rows.indices.compactMap { sibling in
            guard rows[sibling].depth == rows[index].depth, !SessionTree<SessionTab>.isLauncher(rows[sibling].id),
                  SessionTree.parent(of: sibling, in: rows) == parent else { return nil }
            let family = SessionTree.family(sibling, in: rows)
            guard let top = frames[rows[sibling].id], let bottom = frames[rows[family.upperBound - 1].id] else { return nil }
            return (rows[sibling].id, top.minY...bottom.maxY)
        }
        guard let first = spans.first, let last = spans.last else { return nil }
        let hit = spans.first { $0.span.contains(y) }
            ?? (y < first.span.lowerBound ? first : last)
        guard hit.id != id else { return nil }
        return Target(id: hit.id, after: y > (hit.span.lowerBound + hit.span.upperBound) / 2)
    }

    /// Where the marker for a target goes: the family's leading edge, and
    /// the gap before or after it.
    func marker(for target: Target, rows: [SessionTreeRow]) -> CGPoint? {
        guard let index = rows.firstIndex(where: { $0.id == target.id }), let top = frames[target.id] else { return nil }
        let family = SessionTree.family(index, in: rows)
        guard let bottom = frames[rows[family.upperBound - 1].id] else { return nil }
        return CGPoint(x: top.minX, y: target.after ? bottom.maxY + 1 : top.minY - 1)
    }
}

/// The line where a dragged family will land.
private struct SessionTreeDropMarker: View {
    @Environment(\.mica) private var theme
    let drag: SessionTreeDrag
    let rows: [SessionTreeRow]

    var body: some View {
        GeometryReader { proxy in
            if let target = drag.target, let point = drag.marker(for: target, rows: rows) {
                RoundedRectangle(cornerRadius: 1)
                    .fill(theme.accent)
                    .frame(width: max(proxy.size.width - point.x, 0), height: 2)
                    .offset(x: point.x, y: point.y - 1)
            }
        }
        .allowsHitTesting(false)
    }
}

/// A tree row's indentation: one rail per level, under the parent's title,
/// with the space below the row. Indentation stops after three levels, so
/// deeper rows share the third level's rail.
struct SessionTreeRails: Equatable {
    static let maximumLevel = 2
    /// A row's leading padding, status column, and spacing, so a child
    /// starts where its parent's title does.
    static let indent: CGFloat = 32
    static let rowHeight: CGFloat = 32
    /// Space between rows of a family.
    static let rowGap: CGFloat = 2
    /// Space between families.
    static let familyGap: CGFloat = 8

    /// The row's indentation level.
    let level: Int
    /// Space below the row.
    let gap: CGFloat
    /// The deepest rail that continues to the next row.
    let continuing: Int

    init(depth: Int, next: Int?) {
        level = min(depth, Self.maximumLevel)
        let nextLevel = next.map { min($0, Self.maximumLevel) }
        gap = switch nextLevel {
        case nil: 0
        case 0: Self.familyGap
        default: Self.rowGap
        }
        continuing = min(level, nextLevel ?? 0)
    }
}

private struct SessionTreeRowView: View {
    @Environment(\.mica) private var theme
    let row: SessionTreeRow
    let rails: SessionTreeRails
    let selected: Bool
    /// The row's ⌘ number, while ⌘ is held.
    let number: Int?
    let model: SessionWindowModel
    let drag: SessionTreeDrag

    var body: some View {
        HStack(spacing: 0) {
            ForEach(0..<rails.level, id: \.self) { _ in
                Color.clear.frame(width: SessionTreeRails.indent)
            }
            content
                .onGeometryChange(for: CGRect.self) { $0.frame(in: .named(SessionTreeDrag.space)) } action: {
                    drag.frames[row.id] = $0
                }
                .onDisappear { drag.frames[row.id] = nil }
        }
        .padding(.bottom, rails.gap)
        .simultaneousGesture(reorder, isEnabled: !SessionTree<SessionTab>.isLauncher(row.id))
        .overlay(alignment: .topLeading) {
            ForEach(1..<(rails.level + 1), id: \.self) { level in
                Rectangle().fill(theme.border)
                    .frame(width: 1, height: SessionTreeRails.rowHeight + (level <= rails.continuing ? rails.gap : 0))
                    .offset(x: CGFloat(level) * SessionTreeRails.indent - 1)
            }
        }
    }

    /// Dragging a row moves its family among its siblings.
    private var reorder: some Gesture {
        DragGesture(minimumDistance: 4, coordinateSpace: .named(SessionTreeDrag.space))
            .onChanged { value in drag.update(row.id, y: value.location.y, rows: model.rows) }
            .onEnded { value in
                // Resolve at release so the drop matches the pointer.
                if let target = drag.end(row.id, y: value.location.y, rows: model.rows) {
                    model.move(row.id, beside: target.id, after: target.after)
                }
            }
    }

    @ViewBuilder private var content: some View {
        switch row.content {
        case .tab(let tab):
            SessionSidebarRow(tab: tab, selected: selected, row: row, number: number, model: model)
        case .closedParent(let session, let title):
            ClosedParentRow(session: session, title: title, row: row, number: number, model: model)
        }
    }
}

private struct SessionSidebarRow: View {
    @Environment(\.mica) private var theme
    let tab: SessionTab
    let selected: Bool
    let row: SessionTreeRow
    let number: Int?
    let model: SessionWindowModel
    @State private var hovering = false

    var body: some View {
        Button { model.select(tab) } label: {
            HStack(spacing: 10) {
                SessionTabStatusMarker(status: tab.status, finished: tab.finished, serverIssue: tab.serverIssue != nil)
                    .frame(width: 14)
                Text(tab.title)
                    .font(.kit(size: 13, weight: selected ? .medium : .regular))
                    .foregroundStyle(SessionTabStatusMarker.titleProminent(tab, selected: selected) ? theme.text : theme.muted)
                    .lineLimit(1).truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .leading)
                HStack(spacing: 0) {
                    SessionTreeDisclosure(row: row, number: number, model: model)
                    // The hint takes the close button's place.
                    if hovering, number == nil {
                        ChromeIconButton(icon: .x, size: 10, frame: 18, help: "Close tab") { model.requestClose(tab, from: .side) }
                    }
                }
            }
            .padding(.leading, 8).padding(.trailing, 4)
            .frame(height: SessionTreeRails.rowHeight)
            .background(selected || hovering ? theme.frameHover : .clear, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(SessionTabHelp.text(title: tab.title, status: tab.statusLabel, server: tab.serverLabel))
        .accessibilityLabel(SessionTabHelp.accessibilityLabel(title: tab.title, server: tab.serverLabel))
        .accessibilityValue(tab.statusLabel ?? "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .sessionTreeDisclosureAction(row: row, model: model)
        .contextMenu { Button("Close Tab") { model.requestClose(tab, from: .side) } }
    }
}

/// A parent whose tab is closed, kept while its children are open.
/// Choosing it opens its tab again.
private struct ClosedParentRow: View {
    @Environment(\.mica) private var theme
    @Environment(\.sessionNavigation) private var navigation
    /// The parent's tree key.
    let session: String
    let title: String
    let row: SessionTreeRow
    let number: Int?
    let model: SessionWindowModel
    @State private var hovering = false

    var body: some View {
        Button(action: reopen) {
            HStack(spacing: 10) {
                Color.clear.frame(width: 14, height: 14)
                Text(title)
                    .font(.kit(size: 13))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1).truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .leading)
                SessionTreeDisclosure(row: row, number: number, model: model)
            }
            .padding(.leading, 8).padding(.trailing, 4)
            .frame(height: SessionTreeRails.rowHeight)
            .background(hovering ? theme.frameHover : .clear, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(SessionTabHelp.text(title: title, status: "Closed", server: server))
        .accessibilityLabel(SessionTabHelp.accessibilityLabel(title: title, server: server))
        .accessibilityValue("Closed")
        .sessionTreeDisclosureAction(row: row, model: model)
        .contextMenu { Button("Open Tab", action: reopen) }
    }

    private var server: String? { SessionWindowModel.identity(treeKey: session).serverLabel }

    private func reopen() { navigation.openSession(SessionWindowModel.identity(treeKey: session)) }
}

/// A family's caret. A collapsed family also shows how many tabs it hides
/// and the most urgent of their markers. While ⌘ is held, the row's number
/// takes the caret's place.
private struct SessionTreeDisclosure: View {
    @Environment(\.mica) private var theme
    let row: SessionTreeRow
    let number: Int?
    let model: SessionWindowModel

    var body: some View {
        if let number, !row.hasChildren {
            hint(number)
        } else if row.hasChildren {
            HStack(spacing: 6) {
                if row.collapsed {
                    if let urgent = SessionTreeRollUp(row.hiddenTabs).marker {
                        SessionTabStatusMarker(status: urgent.status, finished: urgent.finished)
                            .frame(width: 12)
                    }
                    Text("\(row.hiddenTabs.count)")
                        .font(.kit(size: 11, design: .monospaced))
                        .foregroundStyle(theme.muted)
                }
                if let number {
                    hint(number)
                } else {
                    Button { model.toggleCollapsed(row.id) } label: {
                        IconView(row.collapsed ? .caretRight : .caretDown, size: 10)
                            .foregroundStyle(theme.muted)
                            .frame(width: 18, height: 18)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help(row.collapsed ? "Show sessions from this one" : "Hide sessions from this one")
                    .accessibilityHidden(true)
                }
            }
        }
    }

    /// The hint sits in the caret's 18 pt slot.
    private func hint(_ number: Int) -> some View {
        TabNumberHint(number: number).frame(width: 18, height: 18)
    }
}

/// The marker a collapsed family shows for the tabs it hides: input ahead
/// of work, and work ahead of a finished turn.
struct SessionTreeRollUp {
    let marker: (status: SessionTabStatus, finished: Bool)?

    @MainActor init(_ tabs: [SessionTab]) {
        self.init(tabs.map { ($0.status, $0.finished) })
    }

    init(_ markers: [(status: SessionTabStatus, finished: Bool)]) {
        if markers.contains(where: { $0.status == .awaitingResponse }) {
            marker = (.awaitingResponse, false)
        } else if markers.contains(where: { $0.status == .running }) {
            marker = (.running, false)
        } else if markers.contains(where: \.finished) {
            marker = (.idle, true)
        } else {
            marker = nil
        }
    }
}

private extension View {
    /// Lets assistive technology expand or collapse a family.
    @ViewBuilder func sessionTreeDisclosureAction(row: SessionTreeRow, model: SessionWindowModel) -> some View {
        if row.hasChildren {
            accessibilityAction(named: row.collapsed ? "Expand" : "Collapse") { model.toggleCollapsed(row.id) }
        } else {
            self
        }
    }
}

/// Top-tab layout: traffic lights, session tabs in the window's tab order,
/// new tab, and placement. A hairline divides inactive tabs. Dragging a tab
/// moves it alone. Tabs that do not fit scroll; faded edges and a count show
/// what is hidden, and the selected tab scrolls into view.
struct SessionTopTabs: View {
    @Environment(\.mica) private var theme
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement
    @State private var viewportWidth: CGFloat = 0
    @State private var offset: CGFloat = 0
    @State private var frames: [UUID: ClosedRange<CGFloat>] = [:]
    @State private var hovered: UUID?
    @State private var drag = SessionTabStripDrag()

    var body: some View {
        let selected = model.selectedTab
        let tabs = model.tabs
        let ids = tabs.map(\.id)
        let overflow = TabStripOverflow(frames: frames, offset: offset, viewport: viewportWidth)
        let hidden = tabs.filter { overflow.hides($0.id) }
        let hints = model.tabNumberHints(.top)
        // Hover doesn't follow the pointer while a tab is dragged.
        let hovered = drag.id == nil ? hovered : nil
        HStack(spacing: 6) {
            // The traffic lights occupy this space.
            Color.clear.frame(width: 68, height: 1)
            ScrollViewReader { proxy in
                ScrollView(.horizontal) {
                    HStack(spacing: 0) {
                        ForEach(Array(tabs.enumerated()), id: \.element.id) { index, tab in
                            if index > 0 {
                                SessionTabDivider(visible: SessionTabStrip.showsDivider(
                                    after: index - 1, of: ids, selected: model.selectedTabID, hovered: hovered, dropGap: drag.gap))
                            }
                            SessionTopTab(tab: tab, selected: tab.id == model.selectedTabID,
                                          hovering: tab.id == hovered,
                                          project: SessionTabStrip.project(of: tab, selected: selected),
                                          parent: model.parentTitle(of: tab),
                                          number: hints.tabs[tab.id],
                                          select: { model.select(tab) },
                                          close: { model.requestClose(tab, from: .top) },
                                          hover: { inside in
                                              if inside { self.hovered = tab.id } else if self.hovered == tab.id { self.hovered = nil }
                                          },
                                          move: { model.move(tab, by: $0) })
                            .opacity(drag.id == tab.id ? 0.4 : 1)
                            .id(tab.id)
                            .onGeometryChange(for: ClosedRange<CGFloat>.self) {
                                let frame = $0.frame(in: .named(Self.space))
                                return frame.minX...frame.maxX
                            } action: { frames[tab.id] = $0 }
                            .simultaneousGesture(reorder(tab, at: index))
                        }
                    }
                    .coordinateSpace(.named(Self.space))
                    .overlay(alignment: .leading) { dropMarker(tabs) }
                    // Space beside the tabs belongs to the scroll view, so it
                    // carries the titlebar behavior itself.
                    .frame(minWidth: viewportWidth, maxHeight: .infinity, alignment: .leading)
                    .background(WindowTitlebarRegion())
                }
                .scrollIndicators(.never)
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { viewportWidth = $0 }
                .onScrollGeometryChange(for: CGFloat.self) { $0.contentOffset.x } action: { _, x in offset = x }
                .mask {
                    TabStripEdgeMask(hiddenBefore: overflow.hiddenBefore, hiddenAfter: overflow.hiddenAfter,
                                     leading: Self.leadingFade, trailing: Self.trailingFade)
                }
                .background(HorizontalWheelScrolling())
                .onChange(of: model.selectedTabID) { withAnimation(.easeOut(duration: 0.15)) { proxy.scrollTo(model.selectedTabID) } }
                .onAppear { proxy.scrollTo(model.selectedTabID) }
            }
            .onChange(of: ids) {
                frames = frames.filter { id, _ in ids.contains(id) }
                // A tab closed mid-drag never reports the drag's end.
                drag.forget(keeping: ids)
            }
            if !hidden.isEmpty {
                SessionTabOverflowMenu(hidden: hidden, numbers: model.shortcutNumbers(.top)) { model.select($0) }
            }
            ChromeIconButton(icon: .plus, frame: 28, help: "New session tab (⌘T)") { model.openLauncher() }
            AppUpdateChip().padding(.leading, 6)
            SessionTabPlacementToggle(placement: $placement).padding(.leading, 6)
        }
        .padding(.leading, 16).padding(.trailing, 10)
        .frame(height: SessionChrome.rowHeight)
        .background(WindowTitlebarRegion())
        .background(DragCancellation(active: drag.id != nil) { [drag] in drag.cancel() })
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session tabs")
    }

    private static let space = "SessionTopTabs"

    /// Dragging a tab moves it to the gap nearest the pointer.
    private func reorder(_ tab: SessionTab, at index: Int) -> some Gesture {
        DragGesture(minimumDistance: 4, coordinateSpace: .named(Self.space))
            .onChanged { value in drag.update(tab.id, gap: dropGap(at: value.location.x, dragging: index)) }
            .onEnded { value in
                // Resolve at release so the drop matches the pointer.
                if let gap = drag.end(tab.id, gap: dropGap(at: value.location.x, dragging: index)) {
                    model.move(tab, toGap: gap)
                }
                // Hover wasn't followed during the drag; the pointer's next move restores it.
                hovered = nil
            }
    }

    /// The gap a drag at `x` drops into, among the gaps clear of the edge
    /// fades.
    private func dropGap(at x: CGFloat, dragging index: Int) -> Int? {
        let spans = model.tabs.compactMap { frames[$0.id] }
        guard spans.count == model.tabs.count, viewportWidth > 0 else { return nil }
        let overflow = TabStripOverflow(frames: frames, offset: offset, viewport: viewportWidth)
        guard let visible = SessionTabStrip.clearSpan(offset: offset, viewport: viewportWidth,
                                                      fadingBefore: overflow.hiddenBefore ? Self.leadingFade : 0,
                                                      fadingAfter: overflow.hiddenAfter ? Self.trailingFade : 0)
        else { return nil }
        return SessionTabStrip.dropGap(at: x, dragging: index, frames: spans, visible: visible)
    }

    private static let leadingFade: CGFloat = 32
    private static let trailingFade: CGFloat = 96

    /// The accent bar in the gap a dragged tab would drop into.
    @ViewBuilder private func dropMarker(_ tabs: [SessionTab]) -> some View {
        let spans = tabs.compactMap { frames[$0.id] }
        if let gap = drag.gap, spans.count == tabs.count {
            let x = SessionTabStrip.position(ofGap: gap, in: spans)
            RoundedRectangle(cornerRadius: 1)
                .fill(theme.accent)
                .frame(width: 2, height: SessionTabStrip.markerHeight)
                // Centred on the gap, and kept inside the strip at its ends.
                .offset(x: min(max(x - 1, 0), max((spans.last?.upperBound ?? 0) - 2, 0)))
                .allowsHitTesting(false)
        }
    }
}

/// The top strip's geometry: fixed-width tabs with a divider slot between
/// each pair. Gap `i` is the space before tab `i`, and the tab count is the
/// space after the last.
enum SessionTabStrip {
    static let tabWidth: CGFloat = 220
    static let tabHeight: CGFloat = 30
    /// The space between two tabs, which holds their divider.
    static let separatorWidth: CGFloat = 9
    static let dividerHeight: CGFloat = 16
    static let markerHeight: CGFloat = 22

    /// The folder a top tab names: its project's, when that isn't the
    /// selected tab's. A tab on another server names it even at the same
    /// path, since nothing else on the tab tells the two apart.
    @MainActor static func project(of tab: SessionTab, selected: SessionTab) -> String? {
        guard !tab.directory.isEmpty,
              tab.directory != selected.directory || tab.identity.server != selected.identity.server else { return nil }
        return URL(fileURLWithPath: tab.directory).lastPathComponent
    }

    /// Whether the divider after tab `index` shows. Dividers hide beside the
    /// selected and hovered tabs, which stand on their own, and in the gap a
    /// dragged tab would drop into, where the drop marker takes their place.
    static func showsDivider(after index: Int, of ids: [UUID], selected: UUID?, hovered: UUID?, dropGap: Int?) -> Bool {
        guard index >= 0, index + 1 < ids.count, dropGap != index + 1 else { return false }
        let raised = [selected, hovered].compactMap { $0 }
        return !raised.contains(ids[index]) && !raised.contains(ids[index + 1])
    }

    /// The gap a tab dragged to `x` drops into: the gap whose tabs' middles
    /// bracket the pointer, kept to the gaps in view so a drop never lands
    /// beside a tab scrolled out of sight. Nil when the tab would stay where
    /// it is.
    /// - Parameters:
    ///   - index: The dragged tab's position.
    ///   - frames: Each tab's span in the strip's content, in order.
    ///   - visible: The span of the content in view.
    static func dropGap(at x: CGFloat, dragging index: Int, frames: [ClosedRange<CGFloat>],
                        visible: ClosedRange<CGFloat>) -> Int? {
        let inView = (0...frames.count).filter { gap in
            let position = position(ofGap: gap, in: frames)
            return position >= visible.lowerBound - 1 && position <= visible.upperBound + 1
        }
        guard !frames.isEmpty, let first = inView.first, let last = inView.last else { return nil }
        let nearest = frames.filter { ($0.lowerBound + $0.upperBound) / 2 < x }.count
        let gap = min(max(nearest, first), last)
        return gap == index || gap == index + 1 ? nil : gap
    }

    /// The span of a scrolled strip's content in view and clear of the edge
    /// fades that are showing; nil when the fades cover it all.
    static func clearSpan(offset: CGFloat, viewport: CGFloat, fadingBefore: CGFloat,
                          fadingAfter: CGFloat) -> ClosedRange<CGFloat>? {
        let lower = offset + fadingBefore, upper = offset + viewport - fadingAfter
        return lower <= upper ? lower...upper : nil
    }

    /// Where a gap's drop marker goes: midway between two tabs, or at the
    /// strip's ends.
    static func position(ofGap gap: Int, in frames: [ClosedRange<CGFloat>]) -> CGFloat {
        guard let first = frames.first, let last = frames.last else { return 0 }
        if gap <= 0 { return first.lowerBound }
        if gap >= frames.count { return last.upperBound }
        return (frames[gap - 1].upperBound + frames[gap].lowerBound) / 2
    }
}

/// A top tab being dragged to a new place in the strip.
@MainActor @Observable
final class SessionTabStripDrag {
    /// The dragged tab.
    private(set) var id: UUID?
    /// The gap it would drop into, if that moves it.
    private(set) var gap: Int?
    /// A tab whose drag was cancelled with Esc; its gesture ignores the rest
    /// of the drag.
    @ObservationIgnored private var cancelled: UUID?

    func update(_ id: UUID, gap: Int?) {
        guard cancelled != id else { return }
        if self.id != id { self.id = id }
        if self.gap != gap { self.gap = gap }
    }

    /// Ends the drag, returning the gap to drop into.
    func end(_ id: UUID, gap: Int?) -> Int? {
        defer { self.id = nil; self.gap = nil; cancelled = nil }
        return cancelled == id ? nil : gap
    }

    func cancel() -> Bool {
        guard let id else { return false }
        cancelled = id
        self.id = nil
        gap = nil
        return true
    }

    /// Ends a drag whose tab has closed, since its gesture won't report
    /// the end.
    func forget(keeping ids: [UUID]) {
        if let id, !ids.contains(id) { self.id = nil; gap = nil }
        if let cancelled, !ids.contains(cancelled) { self.cancelled = nil }
    }
}

/// The hairline between two top tabs, or the space it leaves when hidden.
private struct SessionTabDivider: View {
    @Environment(\.mica) private var theme
    let visible: Bool

    var body: some View {
        Rectangle()
            .fill(visible ? theme.border : .clear)
            .frame(width: 1, height: SessionTabStrip.dividerHeight)
            .frame(width: SessionTabStrip.separatorWidth)
            .accessibilityHidden(true)
    }
}

/// The count of hidden session tabs; choosing one selects it.
private struct SessionTabOverflowMenu: View {
    let hidden: [SessionTab]
    /// Each tab's ⌘ number, shown as the item's shortcut. The item handles
    /// it while it's here, so it must select what Show Tab N would: on top,
    /// every numbered row is a tab.
    let numbers: TabShortcutNumbers
    let select: (SessionTab) -> Void

    var body: some View {
        Menu {
            ForEach(hidden) { tab in
                Button(tab.statusLabel.map { "\(tab.title) — \($0)" } ?? tab.title) { select(tab) }
                    .keyboardShortcut(numbers.tabs[tab.id].map { KeyboardShortcut(KeyEquivalent(Character("\($0)")), modifiers: .command) })
            }
        } label: {
            TabOverflowCount(count: hidden.count)
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .fixedSize()
        .help("\(hidden.count) more session\(hidden.count == 1 ? "" : "s")")
        .accessibilityLabel("\(hidden.count) more sessions")
    }
}

private struct SessionTopTab: View {
    @Environment(\.mica) private var theme
    let tab: SessionTab
    let selected: Bool
    let hovering: Bool
    /// The tab's folder, when it differs from the selected tab's.
    let project: String?
    /// The title of the session this one came from.
    let parent: String?
    /// The tab's ⌘ number, while ⌘ is held.
    let number: Int?
    let select: () -> Void
    let close: () -> Void
    let hover: (Bool) -> Void
    /// Moves the tab one place left (-1) or right (1).
    let move: (Int) -> Void

    var body: some View {
        Button(action: select) {
            HStack(spacing: 8) {
                SessionTabStatusMarker(status: tab.status, finished: tab.finished, serverIssue: tab.serverIssue != nil)
                    .frame(width: 12)
                Text(tab.title)
                    .font(.kit(size: 12, weight: selected ? .medium : .regular))
                    .foregroundStyle(SessionTabStatusMarker.titleProminent(tab, selected: selected) ? theme.text : theme.muted)
                    .lineLimit(1).truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if let number {
                    // The hint takes the close button's place; the project stays.
                    if let project { projectLabel(project) }
                    TabNumberHint(number: number)
                } else if selected || hovering {
                    ChromeIconButton(icon: .x, size: 10, frame: 16, help: "Close tab", action: close)
                } else if let project {
                    projectLabel(project)
                }
            }
            .padding(.horizontal, 10)
            .frame(width: SessionTabStrip.tabWidth, height: SessionTabStrip.tabHeight)
            .background { background }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover(perform: hover)
        .help(SessionTabHelp.text(title: tab.title, status: tab.statusLabel, server: tab.serverLabel, parent: parent))
        .accessibilityLabel(SessionTabHelp.accessibilityLabel(title: tab.title, server: tab.serverLabel))
        .accessibilityValue(tab.statusLabel ?? "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityAction(named: "Move Left") { move(-1) }
        .accessibilityAction(named: "Move Right") { move(1) }
        .contextMenu { Button("Close Tab", action: close) }
    }

    private func projectLabel(_ project: String) -> some View {
        Text(project).font(.kit(size: 10, design: .monospaced)).foregroundStyle(theme.strongBorder)
            .lineLimit(1).truncationMode(.middle)
    }

    /// The selected tab is raised in the tiles' surface so it stands apart
    /// from the hover fill.
    @ViewBuilder private var background: some View {
        let shape = RoundedRectangle(cornerRadius: 3)
        if selected {
            shape.fill(theme.surface)
                .overlay(shape.strokeBorder(theme.border))
                .shadow(color: .black.opacity(theme.dark ? 0 : 0.06), radius: 1, y: 1)
        } else if hovering {
            shape.fill(theme.frameHover)
        }
    }
}

/// A session tab's tooltip and spoken name. Another server's name isn't
/// shown on the tab; it's named here and in the session bar.
enum SessionTabHelp {
    /// The title and status, the server when it isn't this Mac's, and the
    /// session the tab came from, one per line.
    static func text(title: String, status: String?, server: String?, parent: String? = nil) -> String {
        var lines = [status.map { "\(title) — \($0)" } ?? title]
        if let server { lines.append("On \(server)") }
        if let parent { lines.append("From \(parent)") }
        return lines.joined(separator: "\n")
    }

    /// The title, and the server when it isn't this Mac's.
    static func accessibilityLabel(title: String, server: String?) -> String {
        server.map { "\(title), on \($0)" } ?? title
    }
}

/// Activity on a session tab: working, awaiting a response, or finished
/// while the user was elsewhere. A tab whose server needs the user shows a
/// warning instead.
struct SessionTabStatusMarker: View {
    @Environment(\.mica) private var theme
    let status: SessionTabStatus
    let finished: Bool
    var serverIssue = false

    var body: some View {
        if serverIssue {
            IconView(.warning, size: 12).foregroundStyle(theme.warning)
        } else {
            activity
        }
    }

    @ViewBuilder private var activity: some View {
        switch status {
        case .running: KitSpinner().foregroundStyle(theme.accent)
        case .awaitingResponse:
            IconView(.questionMark, size: 12).foregroundStyle(theme.warning)
        case .idle where finished:
            IconView(.check, size: 12, label: "Finished").foregroundStyle(theme.success)
        case .idle: Color.clear.frame(height: 14)
        }
    }

    /// Selected tabs and tabs whose marker calls for attention keep full
    /// strength; idle tabs and the launcher recede.
    @MainActor static func titleProminent(_ tab: SessionTab, selected: Bool) -> Bool {
        tab.isLauncher ? false : selected || tab.needsAttention
    }
}

/// Switches session tabs between the sidebar and the top strip.
struct SessionTabPlacementToggle: View {
    @Environment(\.mica) private var theme
    @Binding var placement: SessionTabPlacement

    var body: some View {
        HStack(spacing: 2) {
            option(.side, icon: .sidebarSimple, label: "Session tabs in sidebar")
            option(.top, icon: .browser, label: "Session tabs on top")
        }
        .padding(2)
        .background(theme.surface, in: RoundedRectangle(cornerRadius: 3))
        .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
    }

    private func option(_ value: SessionTabPlacement, icon: Icon, label: String) -> some View {
        Button { placement = value } label: {
            IconView(icon)
                .foregroundStyle(placement == value ? theme.text : theme.muted)
                .frame(width: 26, height: 22)
                .background(placement == value ? theme.frameHover : .clear, in: RoundedRectangle(cornerRadius: 2))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(label)
        .accessibilityLabel(label)
        .accessibilityAddTraits(placement == value ? .isSelected : [])
    }
}

/// A square, borderless icon button for window chrome.
struct ChromeIconButton: View {
    @Environment(\.mica) private var theme
    let icon: Icon
    var size: CGFloat = 14
    var frame: CGFloat = 26
    let help: String
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            IconView(icon, size: size)
                .foregroundStyle(theme.muted)
                .frame(width: frame, height: frame)
                .background(hovering ? theme.frameHover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(help)
        .accessibilityLabel(help)
    }
}

/// Names another server after a session's title, as the session picker's
/// rows do. Session tabs don't; their tooltip and the session bar name it.
/// The title shortens first.
struct ServerLabel: View {
    @Environment(\.mica) private var theme
    let name: String

    var body: some View {
        Text(name).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
            .lineLimit(1).fixedSize()
            .accessibilityLabel("on \(name)")
    }
}

/// The session bar's server, before its folder, for a session on another
/// server. It turns to the warning colour while the server needs the user,
/// and says so while a missed check is retried.
struct ServerChip: View {
    @Environment(\.mica) private var theme
    let name: String
    var attention = false
    var reconnecting = false

    var body: some View {
        HStack(spacing: 5) {
            IconView(.hardDrives, size: 12)
            Text(name).font(.kit(size: 11, design: .monospaced)).lineLimit(1)
            if reconnecting { Text("· Reconnecting…").font(.kit(size: 11)).lineLimit(1) }
        }
        .foregroundStyle(attention ? theme.warning : theme.muted)
        .padding(.horizontal, 6).frame(height: 20)
        .background(theme.frameHover, in: RoundedRectangle(cornerRadius: 3))
        .fixedSize()
        .help("On \(name)")
        .accessibilityElement(children: .combine)
        .accessibilityLabel(reconnecting ? "Server \(name), reconnecting" : "Server \(name)")
    }
}
