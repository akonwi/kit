import SwiftUI

/// The selected session's bar above its tiles: its title (with side tabs),
/// working directory, Git branch, pull request, and plugin footer items.
struct SessionBar: View {
    @Environment(\.mica) private var theme
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
                if !tab.isLauncher, session?.pluginFooter?.locationHidden != true {
                    WorkspaceLocation(session: session)
                }
            }
            .layoutPriority(1)
            if !tab.isLauncher {
                // Plugin items take what the title and location leave.
                PluginFooterView(session: session)
            } else {
                Spacer(minLength: 0)
            }
        }
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

/// Side-tab layout: session tabs grouped by project.
struct SessionSidebar: View {
    @Environment(\.mica) private var theme
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement

    var body: some View {
        let groups = model.groups
        VStack(spacing: 0) {
            HStack {
                Spacer()
                ChromeIconButton(symbol: "plus", help: "New session tab (⌘T)") { model.openLauncher() }
            }
            .padding(.horizontal, 8)
            .frame(height: SessionChrome.rowHeight)
            .background(WindowTitlebarRegion())
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    ForEach(groups) { group in
                        VStack(alignment: .leading, spacing: 2) {
                            if let name = group.name {
                                HStack(spacing: 8) {
                                    Text(name.uppercased())
                                        .font(.kit(size: 10, weight: .medium)).tracking(1.2)
                                        .foregroundStyle(theme.muted).lineLimit(1)
                                    Spacer(minLength: 0)
                                    Text(WorkspaceLocation.homeRelative(group.directory))
                                        .font(.kit(size: 10, design: .monospaced))
                                        .foregroundStyle(theme.strongBorder)
                                        .lineLimit(1).truncationMode(.head)
                                }
                                .padding(.horizontal, 8).frame(height: 24)
                                .help(group.directory)
                            }
                            ForEach(group.tabs) { tab in
                                SessionSidebarRow(tab: tab,
                                                  selected: tab.id == model.selectedTabID,
                                                  select: { model.select(tab) },
                                                  close: { model.close(tab) })
                            }
                        }
                    }
                }.padding(.top, 8)
            }
            .scrollIndicators(.never)
            HStack {
                Spacer()
                SessionTabPlacementToggle(placement: $placement)
            }
            .padding(.leading, 8).padding(.trailing, 4)
            .frame(height: 32)
        }
        .padding(.horizontal, 8).padding(.bottom, 10)
        .frame(width: 248)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session tabs")
    }
}

private struct SessionSidebarRow: View {
    @Environment(\.mica) private var theme
    let tab: SessionTab
    let selected: Bool
    let select: () -> Void
    let close: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: select) {
            HStack(spacing: 10) {
                SessionTabStatusMarker(status: tab.status, finished: tab.finished).frame(width: 14)
                Text(tab.title)
                    .font(.kit(size: 13, weight: selected ? .medium : .regular))
                    .foregroundStyle(SessionTabStatusMarker.titleProminent(tab, selected: selected) ? theme.text : theme.muted)
                    .lineLimit(1).truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if hovering {
                    ChromeIconButton(symbol: "xmark", size: 10, frame: 18, help: "Close tab", action: close)
                }
            }
            .padding(.horizontal, 8)
            .frame(height: 32)
            .background(selected || hovering ? theme.frameHover : .clear, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(tab.statusLabel.map { "\(tab.title) — \($0)" } ?? tab.title)
        .accessibilityLabel(tab.title)
        .accessibilityValue(tab.statusLabel ?? "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .contextMenu { Button("Close Tab", action: close) }
    }
}

/// Top-tab layout: traffic lights, session tabs, new tab, and placement.
/// Tabs that do not fit scroll; faded edges and a count show what is hidden,
/// and the selected tab scrolls into view.
struct SessionTopTabs: View {
    @Environment(\.mica) private var theme
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement
    @State private var viewportWidth: CGFloat = 0
    @State private var offset: CGFloat = 0
    @State private var frames: [UUID: ClosedRange<CGFloat>] = [:]

    var body: some View {
        let selectedDirectory = model.selectedTab.directory
        let tabs = model.displayedTabs
        let overflow = SessionTabOverflow(frames: frames, offset: offset, viewport: viewportWidth)
        let hidden = tabs.filter { overflow.hides($0.id) }
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
                                          select: { model.select(tab) },
                                          close: { model.close(tab) })
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
                    HStack(spacing: 0) {
                        LinearGradient(colors: [.black.opacity(overflow.hiddenBefore ? 0 : 1), .black],
                                       startPoint: .leading, endPoint: .trailing).frame(width: 32)
                        Rectangle()
                        LinearGradient(colors: [.black, .black.opacity(overflow.hiddenAfter ? 0 : 1)],
                                       startPoint: .leading, endPoint: .trailing).frame(width: 96)
                    }
                }
                .onChange(of: model.selectedTabID) { withAnimation(.easeOut(duration: 0.15)) { proxy.scrollTo(model.selectedTabID) } }
                .onAppear { proxy.scrollTo(model.selectedTabID) }
            }
            .onChange(of: tabs.map(\.id)) { frames = frames.filter { id, _ in tabs.contains { $0.id == id } } }
            if !hidden.isEmpty {
                SessionTabOverflowMenu(hidden: hidden) { model.select($0) }
            }
            ChromeIconButton(symbol: "plus", frame: 28, help: "New session tab (⌘T)") { model.openLauncher() }
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

/// Which top tabs are not fully visible in the scrolled strip.
struct SessionTabOverflow {
    let frames: [UUID: ClosedRange<CGFloat>]
    let offset: CGFloat
    let viewport: CGFloat

    func hides(_ id: UUID) -> Bool {
        guard viewport > 0, let frame = frames[id] else { return false }
        return frame.lowerBound - offset < -1 || frame.upperBound - offset > viewport + 1
    }
    var hiddenBefore: Bool { viewport > 0 && frames.values.contains { $0.lowerBound - offset < -1 } }
    var hiddenAfter: Bool { viewport > 0 && frames.values.contains { $0.upperBound - offset > viewport + 1 } }
}

/// The count of hidden session tabs; choosing one selects it.
private struct SessionTabOverflowMenu: View {
    @Environment(\.mica) private var theme
    let hidden: [SessionTab]
    let select: (SessionTab) -> Void

    var body: some View {
        Menu {
            ForEach(hidden) { tab in
                Button(tab.statusLabel.map { "\(tab.title) — \($0)" } ?? tab.title) { select(tab) }
            }
        } label: {
            Text("+\(hidden.count)")
                .font(.kit(size: 11, design: .monospaced))
                .foregroundStyle(theme.muted)
                .padding(.horizontal, 7)
                .frame(height: 22)
                .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                .contentShape(Rectangle())
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
    let select: () -> Void
    let close: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: select) {
            HStack(spacing: 8) {
                SessionTabStatusMarker(status: tab.status, finished: tab.finished).frame(width: 12)
                Text(tab.title)
                    .font(.kit(size: 12, weight: selected ? .medium : .regular))
                    .foregroundStyle(SessionTabStatusMarker.titleProminent(tab, selected: selected) ? theme.text : theme.muted)
                    .lineLimit(1).truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if selected || hovering {
                    ChromeIconButton(symbol: "xmark", size: 10, frame: 16, help: "Close tab", action: close)
                } else if let project {
                    Text(project).font(.kit(size: 10, design: .monospaced)).foregroundStyle(theme.strongBorder)
                }
            }
            .padding(.horizontal, 10)
            .frame(width: 220, height: 30)
            .background(selected || hovering ? theme.frameHover : .clear, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(tab.statusLabel.map { "\(tab.title) — \($0)" } ?? tab.title)
        .accessibilityLabel(tab.title)
        .accessibilityValue(tab.statusLabel ?? "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .contextMenu { Button("Close Tab", action: close) }
    }
}

/// Activity on a session tab: working, awaiting a response, or finished
/// while the user was elsewhere.
struct SessionTabStatusMarker: View {
    @Environment(\.mica) private var theme
    let status: SessionTabStatus
    let finished: Bool

    var body: some View {
        switch status {
        case .running: KitSpinner().foregroundStyle(theme.accent)
        case .awaitingResponse:
            Image(systemName: "questionmark").font(.system(size: 10, weight: .bold)).foregroundStyle(theme.warning)
        case .idle where finished:
            Image(systemName: "checkmark").font(.system(size: 10, weight: .bold)).foregroundStyle(theme.success)
                .accessibilityLabel("Finished")
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
            option(.side, symbol: "sidebar.left", label: "Session tabs in sidebar")
            option(.top, symbol: "rectangle.topthird.inset.filled", label: "Session tabs on top")
        }
        .padding(2)
        .background(theme.surface, in: RoundedRectangle(cornerRadius: 3))
        .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
    }

    private func option(_ value: SessionTabPlacement, symbol: String, label: String) -> some View {
        Button { placement = value } label: {
            Image(systemName: symbol)
                .font(.system(size: 12))
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
    let symbol: String
    var size: CGFloat = 13
    var frame: CGFloat = 26
    let help: String
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: size))
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
