import SwiftUI

/// The selected session's title row above its tiles in side-tab layout.
struct SessionBar: View {
    @Environment(\.mica) private var theme
    let tab: SessionTab

    var body: some View {
        HStack(spacing: 14) {
            Text(tab.title)
                .font(.kit(size: 13, weight: .semibold))
                .lineLimit(1).truncationMode(.tail)
            Spacer(minLength: 0)
        }
        .padding(.leading, 8).padding(.trailing, 14)
        .frame(height: SessionChrome.rowHeight)
        .contentShape(Rectangle())
        .gesture(WindowDragGesture())
    }
}

enum SessionChrome {
    /// The window's top row: traffic lights, session tabs, and the session bar.
    static let rowHeight: CGFloat = 44
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
            .contentShape(Rectangle())
            .gesture(WindowDragGesture())
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
                SessionTabStatusMarker(status: tab.status).frame(width: 14)
                Text(tab.title)
                    .font(.kit(size: 13, weight: selected ? .medium : .regular))
                    .foregroundStyle(tab.isLauncher ? theme.muted : theme.text)
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
        .help(tab.status.label.map { "\(tab.title) — \($0)" } ?? tab.title)
        .accessibilityLabel(tab.title)
        .accessibilityValue(tab.status.label ?? "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .contextMenu { Button("Close Tab", action: close) }
    }
}

/// Top-tab layout: traffic lights, session tabs, new tab, and placement.
struct SessionTopTabs: View {
    @Environment(\.mica) private var theme
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement

    var body: some View {
        let selectedDirectory = model.selectedTab.directory
        HStack(spacing: 6) {
            // The traffic lights occupy this space.
            Color.clear.frame(width: 68, height: 1)
            ScrollViewReader { proxy in
                ScrollView(.horizontal) {
                    HStack(spacing: 2) {
                        ForEach(model.displayedTabs) { tab in
                            SessionTopTab(tab: tab, selected: tab.id == model.selectedTabID,
                                          project: tab.directory != selectedDirectory && !tab.directory.isEmpty
                                            ? URL(fileURLWithPath: tab.directory).lastPathComponent : nil,
                                          select: { model.select(tab) },
                                          close: { model.close(tab) })
                            .id(tab.id)
                        }
                    }
                }
                .scrollIndicators(.never)
                .onChange(of: model.selectedTabID) { proxy.scrollTo(model.selectedTabID) }
            }
            ChromeIconButton(symbol: "plus", frame: 28, help: "New session tab (⌘T)") { model.openLauncher() }
            SessionTabPlacementToggle(placement: $placement).padding(.leading, 6)
        }
        .padding(.leading, 16).padding(.trailing, 10)
        .frame(height: SessionChrome.rowHeight)
        .contentShape(Rectangle())
        .gesture(WindowDragGesture())
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session tabs")
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
                SessionTabStatusMarker(status: tab.status).frame(width: 12)
                Text(tab.title)
                    .font(.kit(size: 12, weight: selected ? .medium : .regular))
                    .foregroundStyle(tab.isLauncher ? theme.muted : theme.text)
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
        .help(tab.status.label.map { "\(tab.title) — \($0)" } ?? tab.title)
        .accessibilityLabel(tab.title)
        .accessibilityValue(tab.status.label ?? "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .contextMenu { Button("Close Tab", action: close) }
    }
}

/// Activity on a session tab: working or awaiting a response.
struct SessionTabStatusMarker: View {
    @Environment(\.mica) private var theme
    let status: SessionTabStatus

    var body: some View {
        switch status {
        case .idle: Color.clear.frame(height: 14)
        case .running: KitSpinner().foregroundStyle(theme.accent)
        case .awaitingResponse:
            Image(systemName: "questionmark").font(.system(size: 10, weight: .bold)).foregroundStyle(theme.warning)
        }
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
