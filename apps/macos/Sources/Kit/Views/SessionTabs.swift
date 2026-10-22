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
                // A family shares its server, so only its top row names it.
                if row.depth == 0, let server = tab.serverLabel { ServerLabel(name: server) }
                HStack(spacing: 0) {
                    SessionTreeDisclosure(row: row, number: number, model: model)
                    // The hint takes the close button's place.
                    if hovering, number == nil {
                        ChromeIconButton(icon: .x, size: 10, frame: 18, help: "Close tab") { model.requestClose(tab) }
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
        .help(tab.statusLabel.map { "\(tab.title) — \($0)" } ?? tab.title)
        .accessibilityLabel(tab.title)
        .accessibilityValue(tab.statusLabel ?? "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .sessionTreeDisclosureAction(row: row, model: model)
        .contextMenu { Button("Close Tab") { model.requestClose(tab) } }
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
                if row.depth == 0, let server = SessionWindowModel.identity(treeKey: session).serverLabel {
                    ServerLabel(name: server)
                }
                SessionTreeDisclosure(row: row, number: number, model: model)
            }
            .padding(.leading, 8).padding(.trailing, 4)
            .frame(height: SessionTreeRails.rowHeight)
            .background(hovering ? theme.frameHover : .clear, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help("\(title) — Closed")
        .accessibilityLabel(title)
        .accessibilityValue("Closed")
        .sessionTreeDisclosureAction(row: row, model: model)
        .contextMenu { Button("Open Tab", action: reopen) }
    }

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

/// Top-tab layout: traffic lights, session tabs in family order, new tab,
/// and placement. Tabs that do not fit scroll; faded edges and a count show
/// what is hidden, and the selected tab scrolls into view.
struct SessionTopTabs: View {
    @Environment(\.mica) private var theme
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement
    @State private var viewportWidth: CGFloat = 0
    @State private var offset: CGFloat = 0
    @State private var frames: [UUID: ClosedRange<CGFloat>] = [:]

    var body: some View {
        let selectedDirectory = model.selectedTab.directory
        let tabs = model.familyOrder
        let overflow = TabStripOverflow(frames: frames, offset: offset, viewport: viewportWidth)
        let hidden = tabs.filter { overflow.hides($0.id) }
        let hints = model.tabNumberHints(.top)
        HStack(spacing: 6) {
            // The traffic lights occupy this space.
            Color.clear.frame(width: 68, height: 1)
            ScrollViewReader { proxy in
                ScrollView(.horizontal) {
                    HStack(spacing: 2) {
                        ForEach(tabs) { tab in
                            SessionTopTab(tab: tab, selected: tab.id == model.selectedTabID,
                                          project: tab.directory != selectedDirectory && !tab.directory.isEmpty
                                            ? URL(fileURLWithPath: tab.directory).lastPathComponent : nil,
                                          parent: model.parentTitle(of: tab),
                                          number: hints.tabs[tab.id],
                                          select: { model.select(tab) },
                                          close: { model.requestClose(tab) })
                            .id(tab.id)
                            .onGeometryChange(for: ClosedRange<CGFloat>.self) {
                                let frame = $0.frame(in: .named(Self.space))
                                return frame.minX...frame.maxX
                            } action: { frames[tab.id] = $0 }
                        }
                    }
                    .coordinateSpace(.named(Self.space))
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
                                     leading: 32, trailing: 96)
                }
                .background(HorizontalWheelScrolling())
                .onChange(of: model.selectedTabID) { withAnimation(.easeOut(duration: 0.15)) { proxy.scrollTo(model.selectedTabID) } }
                .onAppear { proxy.scrollTo(model.selectedTabID) }
            }
            .onChange(of: tabs.map(\.id)) { frames = frames.filter { id, _ in tabs.contains { $0.id == id } } }
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
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session tabs")
    }

    private static let space = "SessionTopTabs"
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
    let project: String?
    /// The title of the session this one came from.
    let parent: String?
    /// The tab's ⌘ number, while ⌘ is held.
    let number: Int?
    let select: () -> Void
    let close: () -> Void
    @State private var hovering = false

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
                if let server = tab.serverLabel { ServerLabel(name: server) }
                if let number {
                    // The hint takes the close button's place; labels stay.
                    if tab.serverLabel == nil, let project { projectLabel(project) }
                    TabNumberHint(number: number)
                } else if selected || hovering {
                    ChromeIconButton(icon: .x, size: 10, frame: 16, help: "Close tab", action: close)
                } else if tab.serverLabel == nil, let project {
                    projectLabel(project)
                }
            }
            .padding(.horizontal, 10)
            .frame(width: 220, height: 30)
            .background { background }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(help)
        .accessibilityLabel(tab.title)
        .accessibilityValue(tab.statusLabel ?? "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .contextMenu { Button("Close Tab", action: close) }
    }

    private func projectLabel(_ project: String) -> some View {
        Text(project).font(.kit(size: 10, design: .monospaced)).foregroundStyle(theme.strongBorder)
            .lineLimit(1).truncationMode(.middle)
    }

    private var help: String {
        let title = tab.statusLabel.map { "\(tab.title) — \($0)" } ?? tab.title
        return parent.map { "\(title)\nFrom \($0)" } ?? title
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

/// Names another server after a session's title. The title shortens first.
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
