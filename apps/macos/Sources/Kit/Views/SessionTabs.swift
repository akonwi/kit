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
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement
    @State private var viewportWidth: CGFloat = 0
    @State private var offset: CGFloat = 0
    @State private var frames: [UUID: ClosedRange<CGFloat>] = [:]
    @State private var hovered: UUID?
    @State private var drag = SessionTabStripDrag()
    @State private var position = ScrollPosition(idType: UUID.self)
    /// The tab whose drag gesture is active. It clears when the gesture
    /// ends or is cancelled, which ends the drag even when the gesture
    /// doesn't report its end.
    @GestureState private var pressed: UUID?

    var body: some View {
        let selected = model.selectedTab
        let tabs = model.tabs
        let ids = tabs.map(\.id)
        // While a tab is held, the others make way for it.
        let arranged = drag.holds(ids) ? SessionTabStrip.arranged(tabs, moving: drag.origin, to: drag.target) : tabs
        let shown = arranged.map(\.id)
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
                        ForEach(Array(arranged.enumerated()), id: \.element.id) { index, tab in
                            if index > 0 {
                                SessionTabDivider(visible: SessionTabStrip.showsDivider(
                                    after: index - 1, of: shown, selected: model.selectedTabID, hovered: hovered, held: drag.id))
                            }
                            SessionTopTab(tab: tab, selected: tab.id == model.selectedTabID,
                                          hovering: tab.id == hovered,
                                          project: SessionTabStrip.project(of: tab, selected: selected),
                                          parent: model.parentTitle(of: tab),
                                          number: hints.tabs[tab.id],
                                          // Letting go of a dragged tab isn't a click.
                                          select: { if !drag.claimsRelease(of: tab.id) { model.select(tab) } },
                                          close: { model.requestClose(tab, from: .top) },
                                          hover: { inside in
                                              if inside { self.hovered = tab.id } else if self.hovered == tab.id { self.hovered = nil }
                                          },
                                          move: { model.move(tab, by: $0) })
                            // The held tab's place is an empty slot where it
                            // will land; its lifted copy follows the pointer.
                            // The slot covers the tab rather than hiding it,
                            // which would end the tab's drag gesture.
                            .overlay { if drag.id == tab.id { SessionTopTabSlot() } }
                            // The tab sits in the title bar, where a drag
                            // would move the window instead of the tab.
                            .background(TitlebarDragExclusion())
                            .id(tab.id)
                            .onGeometryChange(for: ClosedRange<CGFloat>.self) {
                                let frame = $0.frame(in: .named(Self.space))
                                return frame.minX...frame.maxX
                            } action: { frames[tab.id] = $0 }
                            .simultaneousGesture(reorder(tab))
                        }
                    }
                    .coordinateSpace(.named(Self.space))
                    .overlay(alignment: .leading) {
                        if let id = drag.id, let tab = tabs.first(where: { $0.id == id }) {
                            LiftedTopTab(drag: drag, tab: tab, selected: id == model.selectedTabID,
                                         project: SessionTabStrip.project(of: tab, selected: selected))
                        }
                    }
                    // Space beside the tabs belongs to the scroll view, so it
                    // carries the titlebar behavior itself.
                    .frame(minWidth: viewportWidth, maxHeight: .infinity, alignment: .leading)
                    .background(WindowTitlebarRegion())
                }
                .scrollIndicators(.never)
                .scrollPosition($position)
                .coordinateSpace(.named(Self.viewport))
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { viewportWidth = $0 }
                .onScrollGeometryChange(for: CGFloat.self) { $0.contentOffset.x } action: { _, x in
                    offset = x
                    // The lifted copy stays under the pointer as the strip scrolls.
                    if drag.dragging { follow() }
                }
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
                // A tab opened or closed mid-drag changes the places it
                // could land.
                drag.forget(keeping: ids)
            }
            .onChange(of: pressed) { old, _ in if let old { release(old) } }
            // Holding the tab past an edge scrolls the strip.
            .task(id: drag.edge) {
                guard drag.edge != 0 else { return }
                var last = ContinuousClock.now
                while true {
                    do { try await Task.sleep(for: .milliseconds(16)) } catch { return }
                    let now = ContinuousClock.now
                    // A stalled frame doesn't jump the strip.
                    autoscroll(for: min((now - last) / .seconds(1), 0.05))
                    last = now
                }
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
        .background(DragCancellation(active: drag.dragging) { cancelDrag() })
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session tabs")
    }

    private static let space = "SessionTopTabs"
    private static let viewport = "SessionTopTabsViewport"

    /// Dragging a tab lifts it; the others make way for it as it passes them.
    private func reorder(_ tab: SessionTab) -> some Gesture {
        DragGesture(minimumDistance: 4, coordinateSpace: .named(Self.viewport))
            .updating($pressed) { _, pressed, _ in pressed = tab.id }
            .onChanged { value in
                if drag.id != tab.id || !drag.dragging {
                    let slots = model.tabs.compactMap { frames[$0.id] }
                    guard let index = model.tabs.firstIndex(where: { $0.id == tab.id }),
                          slots.count == model.tabs.count,
                          drag.begin(tab.id, at: index, slots: slots,
                                     grab: value.startLocation.x + offset - slots[index].lowerBound)
                    else { return }
                }
                drag.point(at: value.location.x)
                follow()
            }
            .onEnded { _ in release(tab.id) }
    }

    /// Lets go of a tab, once its drag gesture ends or is cancelled.
    private func release(_ id: UUID) {
        let drop = drag.drop(id)
        if let gap = drop.gap, let tab = model.tabs.first(where: { $0.id == id }) { model.move(tab, toGap: gap) }
        if drop.held { settle(id) }
        // The tab's button sees the same release, before or after this.
        Task { @MainActor [drag] in drag.released(id) }
        // Hover wasn't followed during the drag; the pointer's next move restores it.
        hovered = nil
    }

    /// Moves the lifted copy under the pointer and the held tab's slot to
    /// where the copy would land, and starts or stops scrolling when the
    /// copy is held at an edge.
    private func follow() {
        guard drag.dragging, let slot = drag.slots.first, let last = drag.slots.last else { return }
        let desired = drag.pointer + offset - drag.grab
        let leading = SessionTabStrip.liftedLeading(desired, width: slot.upperBound - slot.lowerBound,
                                                    among: drag.slots, visible: clearSpan() ?? offset...(offset + viewportWidth))
        let farthest = max(last.upperBound - viewportWidth, 0)
        let overshoot = desired - leading
        let edge = overshoot > 0.5 && offset < farthest - 0.5 ? 1 : overshoot < -0.5 && offset > 0.5 ? -1 : 0
        drag.follow(leading: leading, overshoot: overshoot, edge: edge)
        let target = SessionTabStrip.target(of: leading, among: drag.slots)
        if target != drag.target {
            withAnimation(motion) { drag.retarget(target) }
        }
    }

    /// Scrolls the strip toward the edge the lifted copy is held at, faster
    /// the farther past it the pointer is.
    private func autoscroll(for seconds: Double) {
        guard drag.dragging, let last = drag.slots.last else { return }
        let farthest = max(last.upperBound - viewportWidth, 0)
        let next = min(max(offset + SessionTabStrip.scrollSpeed(past: drag.overshoot) * seconds, 0), farthest)
        guard abs(next - offset) > 0.01 else { return }
        position.scrollTo(x: next)
        offset = next
        follow()
    }

    /// Glides the lifted copy into the held tab's slot, then puts the tab
    /// down.
    private func settle(_ id: UUID) {
        guard drag.settling, drag.id == id else { return }
        withAnimation(motion) { drag.settle() } completion: { drag.finish(id) }
    }

    private var motion: Animation? { reduceMotion ? nil : .easeOut(duration: 0.15) }

    /// Esc returns the held tab to where it started.
    private func cancelDrag() -> Bool {
        guard let id = drag.id, withAnimation(motion, { drag.cancel() }) else { return false }
        settle(id)
        return true
    }

    /// The part of the strip in view and clear of the edge fades.
    private func clearSpan() -> ClosedRange<CGFloat>? {
        guard viewportWidth > 0 else { return nil }
        let overflow = TabStripOverflow(frames: frames, offset: offset, viewport: viewportWidth)
        return SessionTabStrip.clearSpan(offset: offset, viewport: viewportWidth,
                                         fadingBefore: overflow.hiddenBefore ? Self.leadingFade : 0,
                                         fadingAfter: overflow.hiddenAfter ? Self.trailingFade : 0)
    }

    private static let leadingFade: CGFloat = 32
    private static let trailingFade: CGFloat = 96
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

    /// The folder a top tab names: its project's, when that isn't the
    /// selected tab's. A tab on another server names it even at the same
    /// path, since nothing else on the tab tells the two apart.
    @MainActor static func project(of tab: SessionTab, selected: SessionTab) -> String? {
        guard !tab.directory.isEmpty,
              tab.directory != selected.directory || tab.identity.server != selected.identity.server else { return nil }
        return URL(fileURLWithPath: tab.directory).lastPathComponent
    }

    /// Whether the divider after tab `index` shows. Dividers hide beside the
    /// selected and hovered tabs, which stand on their own, and beside a
    /// held tab's empty slot.
    static func showsDivider(after index: Int, of ids: [UUID], selected: UUID?, hovered: UUID?, held: UUID?) -> Bool {
        guard index >= 0, index + 1 < ids.count else { return false }
        let raised = [selected, hovered, held].compactMap { $0 }
        return !raised.contains(ids[index]) && !raised.contains(ids[index + 1])
    }

    /// The tabs in the order they show while the tab at `origin` is held
    /// over `target`: the tabs it has passed make way for it.
    static func arranged<Tab>(_ tabs: [Tab], moving origin: Int, to target: Int) -> [Tab] {
        guard tabs.indices.contains(origin), tabs.indices.contains(target), origin != target else { return tabs }
        var tabs = tabs
        tabs.insert(tabs.remove(at: origin), at: target)
        return tabs
    }

    /// The place a lifted tab whose leading edge is at `leading` would land:
    /// the slot nearest it. A neighbor makes way once the lifted tab is
    /// nearer the neighbor's place than its own.
    static func target(of leading: CGFloat, among slots: [ClosedRange<CGFloat>]) -> Int {
        slots.indices.min { abs(slots[$0].lowerBound - leading) < abs(slots[$1].lowerBound - leading) } ?? 0
    }

    /// The gap that moving the tab at `origin` to `target` drops it into;
    /// nil when it stays where it was.
    static func gap(moving origin: Int, to target: Int) -> Int? {
        target == origin ? nil : target > origin ? target + 1 : target
    }

    /// The span of a scrolled strip's content in view and clear of the edge
    /// fades that are showing; nil when the fades cover it all.
    static func clearSpan(offset: CGFloat, viewport: CGFloat, fadingBefore: CGFloat,
                          fadingAfter: CGFloat) -> ClosedRange<CGFloat>? {
        let lower = offset + fadingBefore, upper = offset + viewport - fadingAfter
        return lower <= upper ? lower...upper : nil
    }

    /// The leading edge of a held tab's lifted copy, which follows the
    /// pointer to `desired`: kept within the tabs and within the part of
    /// the strip clear of the edge fades, or, when that's narrower than the
    /// tab, covering it. Holding the copy past there scrolls the strip.
    static func liftedLeading(_ desired: CGFloat, width: CGFloat, among slots: [ClosedRange<CGFloat>],
                              visible: ClosedRange<CGFloat>) -> CGFloat {
        let lower = max(slots.first?.lowerBound ?? -.infinity, min(visible.lowerBound, visible.upperBound - width))
        let upper = min((slots.last?.upperBound ?? .infinity) - width, max(visible.lowerBound, visible.upperBound - width))
        return min(max(desired, lower), max(upper, lower))
    }

    /// How fast, in points per second, the strip scrolls while the pointer
    /// is `overshoot` points past where the lifted copy is held at an edge.
    /// Negative scrolls toward the start.
    static func scrollSpeed(past overshoot: CGFloat) -> CGFloat {
        let speed = min(abs(overshoot) * 10, 1200)
        return overshoot < 0 ? -speed : speed
    }
}

/// A top tab being dragged to a new place in the strip.
@MainActor @Observable
final class SessionTabStripDrag {
    /// The held tab.
    private(set) var id: UUID?
    /// Where the held tab was in the tab order when it was picked up.
    private(set) var origin = 0
    /// Where it would land.
    private(set) var target = 0
    /// The leading edge of the tab's lifted copy, in the strip's content.
    /// Only the lifted copy reads it, so pointer moves redraw only that.
    private(set) var leading: CGFloat?
    /// The edge the strip scrolls toward while the copy is held there: -1
    /// for the start, 1 for the end, or 0.
    private(set) var edge = 0
    /// Whether the tab has been dropped, or its drag cancelled, and its
    /// lifted copy is gliding into its slot.
    private(set) var settling = false

    /// Each tab's span in the strip's content when the drag began: the
    /// places the tab can land.
    @ObservationIgnored private(set) var slots: [ClosedRange<CGFloat>] = []
    /// The pointer's distance from the held tab's leading edge.
    @ObservationIgnored private(set) var grab: CGFloat = 0
    /// The pointer's position in the strip's viewport.
    @ObservationIgnored private(set) var pointer: CGFloat = 0
    /// How far the copy is held short of the pointer at an edge.
    @ObservationIgnored private(set) var overshoot: CGFloat = 0
    /// A tab whose drag was cancelled; its gesture ignores the rest of the
    /// drag.
    @ObservationIgnored private var cancelled: UUID?
    /// A tab whose drag has just been let go of, whose button may still
    /// see the release.
    @ObservationIgnored private var dropped: UUID?

    /// Whether a tab is held.
    var dragging: Bool { id != nil && !settling }

    /// Whether a tab is held among `ids`, the tabs it was picked up from,
    /// in the same order: the tabs it passes make way for it only then.
    func holds(_ ids: [UUID]) -> Bool {
        dragging && ids.count == slots.count && ids.indices.contains(origin) && ids[origin] == id
    }

    /// Picks up the tab at `index`, unless its drag was cancelled.
    func begin(_ id: UUID, at index: Int, slots: [ClosedRange<CGFloat>], grab: CGFloat) -> Bool {
        guard cancelled != id, slots.indices.contains(index) else { return false }
        reset()
        dropped = nil
        self.id = id
        origin = index
        target = index
        self.slots = slots
        self.grab = grab
        return true
    }

    func point(at x: CGFloat) { pointer = x }

    func follow(leading: CGFloat, overshoot: CGFloat, edge: Int) {
        guard dragging else { return }
        if self.leading != leading { self.leading = leading }
        self.overshoot = overshoot
        if self.edge != edge { self.edge = edge }
    }

    func retarget(_ target: Int) {
        guard dragging, slots.indices.contains(target), self.target != target else { return }
        self.target = target
    }

    /// Drops the held tab, returning whether it was held, after which its
    /// lifted copy settles, and the gap to move it into. A tab whose drag
    /// was cancelled, or that was already dropped, isn't held.
    func drop(_ id: UUID) -> (held: Bool, gap: Int?) {
        dropped = id
        if cancelled == id { cancelled = nil; return (false, nil) }
        guard self.id == id, dragging else { return (false, nil) }
        let gap = SessionTabStrip.gap(moving: origin, to: target)
        // The tab order now has the tab where it landed.
        origin = target
        settling = true
        edge = 0
        return (true, gap)
    }

    /// Returns the held tab to where it started; its lifted copy then
    /// settles there.
    func cancel() -> Bool {
        guard let id, dragging else { return false }
        cancelled = id
        target = origin
        settling = true
        edge = 0
        return true
    }

    /// Moves the lifted copy over the tab's slot.
    func settle() {
        guard settling, slots.indices.contains(target) else { return }
        leading = slots[target].lowerBound
    }

    /// Whether the release a tab's button sees ends its drag rather than
    /// clicking it.
    func claimsRelease(of id: UUID) -> Bool {
        self.id == id || dropped == id || cancelled == id
    }

    /// The release that ended a tab's drag has been handled.
    func released(_ id: UUID) {
        if dropped == id { dropped = nil }
    }

    /// Puts the settled tab down.
    func finish(_ id: UUID) {
        guard self.id == id, settling else { return }
        reset()
    }

    /// Ends a drag when the tabs change other than by its own drop, since
    /// the places it could land have changed. Its gesture ignores the rest
    /// of the drag.
    func forget(keeping ids: [UUID]) {
        if let cancelled, !ids.contains(cancelled) { self.cancelled = nil }
        guard let id, ids.count != slots.count || ids.firstIndex(of: id) != origin else { return }
        if dragging, ids.contains(id) { cancelled = id }
        reset()
    }

    private func reset() {
        id = nil
        origin = 0
        target = 0
        leading = nil
        edge = 0
        settling = false
        slots = []
        overshoot = 0
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

/// The slot a held top tab leaves in the strip.
private struct SessionTopTabSlot: View {
    @Environment(\.mica) private var theme

    var body: some View {
        RoundedRectangle(cornerRadius: 3).fill(theme.frameHover)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

/// The held top tab's copy, following the pointer along the strip.
/// It reads the pointer position itself, so only it redraws as the
/// pointer moves.
private struct LiftedTopTab: View {
    let drag: SessionTabStripDrag
    let tab: SessionTab
    let selected: Bool
    let project: String?

    var body: some View {
        if let leading = drag.leading {
            SessionTopTab(tab: tab, selected: selected, hovering: false, project: project, parent: nil, number: nil,
                          select: {}, close: {}, hover: { _ in }, move: { _ in }, lifted: true)
                .offset(x: leading)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
        }
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
    /// Drawn as the held tab's copy that follows the pointer.
    var lifted = false

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
                } else if (selected || hovering) && !lifted {
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
        if lifted {
            // Picked up: raised like the selected tab, with a deeper shadow.
            shape.fill(theme.surface)
                .overlay(shape.strokeBorder(theme.border))
                .shadow(color: .black.opacity(theme.dark ? 0.5 : 0.16), radius: 4, y: 2)
        } else if selected {
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
