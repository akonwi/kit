import AppKit
import SwiftUI

struct SessionLauncherView: View {
    /// The server whose sessions the launcher lists.
    let connection: ServerConnection
    /// The status of each of the window's sessions that is working or
    /// awaiting a response.
    var activity: [String: SessionTabStatus] = [:]
    /// Opens the new session form.
    let create: () -> Void
    let open: (SessionExcerpt) -> Void
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var themeConfiguration = ""
    @Environment(\.installedThemes) private var installedThemes

    @State private var refreshRequest = 0
    @State private var target: SessionExcerpt?
    @State private var deleting = false

    var body: some View {
        let scheme = SystemAppearance.shared.resolve(appearance)
        let dark = scheme == .dark
        let theme = ThemeConfiguration.decode(themeConfiguration).resolved(dark: dark, installed: installedThemes)
        let catalog = connection.catalog
        SessionLauncherContent(sessions: catalog.sessions, connecting: catalog.loading,
                               connected: catalog.loaded, error: catalog.error, open: open, refresh: { refreshRequest += 1 },
                               create: create, focusRequest: target == nil, activity: activity,
                               rename: { deleting = false; target = $0 },
                               delete: { deleting = true; target = $0 })
        .modifier(SessionCatalogActions(client: connection.client, refresh: { await catalog.refresh() },
                                        target: $target, deleting: $deleting))
        .environment(\.mica, theme)
        .foregroundStyle(theme.text)
        .background(theme.surface)
        .tint(theme.accent)
        .preferredColorScheme(theme.dark ? .dark : .light)
        .frame(minWidth: 680, minHeight: 540)
        .onReceive(NotificationCenter.default.publisher(for: .kitSessionDeleted)) { notification in
            if let identity = notification.object as? SessionIdentity { catalog.sessionDeleted(identity) }
        }
        .task(id: refreshRequest) { await catalog.refresh() }
    }
}

/// Finds a session to open. The launcher lists the window's active sessions
/// above the recent ones; the compact form backs the Open session palette.
struct SessionLauncherContent: View {
    let sessions: [SessionExcerpt]
    let connecting: Bool
    let connected: Bool
    let error: String?
    let open: (SessionExcerpt) -> Void
    let refresh: () -> Void
    var create: (() -> Void)? = nil
    var focusRequest = true
    var compact = false
    var currentID: String? = nil
    var activity: [String: SessionTabStatus] = [:]
    var rename: ((SessionExcerpt) -> Void)? = nil
    var delete: ((SessionExcerpt) -> Void)? = nil
    /// The server to name beside a session from another server.
    var serverLabel: (SessionExcerpt) -> String? = { _ in nil }
    @Environment(\.mica) private var theme
    @State private var selection = SessionLauncher()
    @FocusState private var searchFocused: Bool

    private var sections: (active: [SessionExcerpt], recent: [SessionExcerpt]) {
        selection.sections(in: sessions, activity: compact ? [:] : activity)
    }
    private var results: [SessionExcerpt] { let sections = sections; return sections.active + sections.recent }
    /// A connected server without sessions shows only the way to start one.
    private var firstSession: Bool { !compact && connected && !connecting && error == nil && sessions.isEmpty }

    var body: some View {
        Group {
            if compact { palette }
            else if firstSession { firstSessionPrompt }
            else { launcher }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .task(id: focusRequest) {
            guard focusRequest else { searchFocused = false; return }
            // Let the outgoing palette field leave the responder chain before
            // assigning focus to the newly mounted session search field.
            do { try await Task.sleep(for: .milliseconds(100)) } catch { return }
            searchFocused = true
        }
        .onChange(of: results.map(\.id), initial: true) {
            if !results.contains(where: { $0.id == selection.highlightedID }) {
                selection.highlightedID = results.first?.id
            }
        }
        .onChange(of: selection.query) { selection.highlightedID = results.first?.id }
        .onKeyPress(.pageDown) { selection.move(5, in: results); return .handled }
        .onKeyPress(.pageUp) { selection.move(-5, in: results); return .handled }
        .onKeyPress(.downArrow) { selection.move(1, in: results); return .handled }
        .onKeyPress(.upArrow) { selection.move(-1, in: results); return .handled }
        .onKeyPress(.return) { openHighlighted(); return .handled }
    }

    // MARK: Launcher

    private var launcher: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 28) {
                    if let create { NewSessionButton(action: create).disabled(!connected || connecting) }
                    VStack(alignment: .leading, spacing: 20) {
                        searchField
                        let sections = sections
                        if !sections.active.isEmpty { section("Active", sections.active) }
                        if !sections.recent.isEmpty { section("Recent", sections.recent) }
                        if results.isEmpty { emptyState.padding(.vertical, 42) }
                        if let error, !results.isEmpty { connectionNotice(error) }
                    }
                }
                .frame(width: 560, alignment: .leading)
                .padding(.top, 150).padding(.bottom, 40)
                .frame(maxWidth: .infinity)
            }
            .scrollIndicators(.automatic)
            .onChange(of: selection.highlightedID) {
                if let id = selection.highlightedID { proxy.scrollTo(id) }
            }
        }
        .background { catalogShortcuts }
    }

    private var firstSessionPrompt: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Start your first session").font(.kit(size: 18, weight: .semibold))
            if let create { NewSessionButton(action: create) }
        }
        .frame(width: 440, alignment: .leading)
        .padding(.bottom, 60)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var searchField: some View {
        HStack(spacing: 10) {
            IconView(.magnifyingGlass, size: 14).foregroundStyle(theme.muted)
            TextField("Search sessions or folders", text: $selection.query)
                .textFieldStyle(.plain).focused($searchFocused)
                .onSubmit { openHighlighted() }
            if connecting { KitSpinner() }
            else if !selection.query.isEmpty {
                Button { selection.query = "" } label: { IconView(.xCircle, size: 12) }
                    .buttonStyle(.plain).foregroundStyle(theme.muted).help("Clear search").accessibilityLabel("Clear search")
            }
        }
        .font(.kit(size: 13))
        .padding(.horizontal, 10).frame(height: 32)
        .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
        .overlay { RoundedRectangle(cornerRadius: 3).strokeBorder(searchFocused ? theme.accent : theme.border) }
    }

    private func section(_ title: String, _ sessions: [SessionExcerpt]) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title.uppercased()).font(.kit(size: 11, weight: .medium)).tracking(1.32)
                .foregroundStyle(theme.muted).padding(.horizontal, 10)
                .accessibilityAddTraits(.isHeader)
            LazyVStack(spacing: 2) {
                ForEach(sessions) { session in
                    LauncherRow(session: session, status: activity[session.id] ?? .idle,
                                highlighted: selection.highlightedID == session.id,
                                open: { open(session) }, rename: rename, delete: delete)
                        .id(session.id)
                }
            }
        }
    }

    private func connectionNotice(_ error: String) -> some View {
        HStack(spacing: 12) {
            Label("Connection unavailable", icon: .warningCircle).help(error)
            Spacer()
            Button("Retry connection", action: refresh)
        }.font(.kit(size: 12)).foregroundStyle(theme.muted).padding(.horizontal, 10)
    }

    /// Rename and delete act on the highlighted session from the keyboard;
    /// each row's context menu offers them by pointer.
    private var catalogShortcuts: some View {
        HStack {
            if let rename {
                Button("Rename…") { if let selectedSession { rename(selectedSession) } }
                    .keyboardShortcut("r", modifiers: .command)
            }
            if let delete {
                Button("Delete…", role: .destructive) { if let selectedSession { delete(selectedSession) } }
                    .keyboardShortcut(KeyEquivalent("\u{7f}"), modifiers: .command)
            }
        }
        .disabled(selectedSession == nil || !focusRequest)
        .opacity(0).allowsHitTesting(false).accessibilityHidden(true)
    }

    // MARK: Palette

    private var palette: some View {
        VStack(alignment: .leading, spacing: 26) {
            VStack(spacing: 0) {
                HStack(spacing: 10) {
                    IconView(.magnifyingGlass, size: 16).foregroundStyle(theme.muted)
                    TextField("Search sessions or directories", text: $selection.query)
                        .textFieldStyle(.plain).focused($searchFocused)
                        .onSubmit { openHighlighted() }
                    if !selection.query.isEmpty {
                        Button { selection.query = "" } label: { IconView(.xCircle) }
                            .buttonStyle(.plain).foregroundStyle(theme.muted).help("Clear search").accessibilityLabel("Clear search")
                    }
                }
                .font(.kit(size: 14)).padding(14)
                .overlay(alignment: .bottom) { Rectangle().fill(searchFocused ? theme.accent : theme.border).frame(height: 1) }
                HStack {
                    Text("Recent sessions").font(.kit(size: 12, weight: .medium))
                    Spacer()
                    if connecting { KitSpinner() }
                    else {
                        Button(action: refresh) { IconView(.arrowClockwise) }
                            .buttonStyle(.plain).help("Refresh sessions").accessibilityLabel("Refresh sessions")
                    }
                }.foregroundStyle(theme.muted).padding(.top, 24).padding(.bottom, 10)
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(spacing: 4) {
                            ForEach(results) { session in
                                paletteRow(session).id(session.id)
                            }
                            if results.isEmpty { emptyState.padding(.vertical, 42) }
                        }
                    }
                    .onChange(of: selection.highlightedID) {
                        if let id = selection.highlightedID { proxy.scrollTo(id) }
                    }
                }.frame(maxHeight: .infinity)
            }
            if rename != nil || delete != nil {
                HStack {
                    if let rename {
                        Button { if let selectedSession { rename(selectedSession) } } label: {
                            HStack(spacing: 8) {
                                Text("Rename…")
                                Text("⌘R").foregroundStyle(theme.muted)
                            }
                        }
                            .keyboardShortcut("r", modifiers: .command)
                    }
                    if let delete {
                        Button(role: .destructive) { if let selectedSession { delete(selectedSession) } } label: {
                            HStack(spacing: 8) {
                                Text(selectedSession?.isTemporary == true ? "Dispose…" : "Delete…")
                                Text("⌘⌫").foregroundStyle(theme.muted)
                            }
                        }
                            .keyboardShortcut(KeyEquivalent("\u{7f}"), modifiers: .command)
                    }
                    Spacer()
                }
                .font(.kit(size: 12))
                .disabled(selectedSession == nil || !focusRequest)
            }
            if let error, !results.isEmpty {
                HStack(spacing: 12) {
                    Label("Connection unavailable", icon: .warningCircle)
                        .help(error)
                    Spacer()
                    Button("Retry connection", action: refresh)
                }.font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
        }
        .padding(20).frame(maxWidth: 740, maxHeight: 680)
    }

    private func paletteRow(_ session: SessionExcerpt) -> some View {
        let highlighted = selection.highlightedID == session.id
        let path = session.cwd ?? session.workspace
        return Button { open(session) } label: {
            HStack(spacing: 14) {
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        Text(session.title).font(.kit(size: 14, weight: .medium)).lineLimit(1)
                        if let server = serverLabel(session) {
                            Text(server).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                                .lineLimit(1).fixedSize()
                        }
                        if session.isTemporary == true {
                            Text("Temporary").font(.kit(size: 11)).foregroundStyle(theme.muted)
                        }
                    }
                    Text(WorkspaceLocation.homeRelative(path)).font(.kit(size: 12, design: .monospaced))
                        .foregroundStyle(theme.muted).lineLimit(1).truncationMode(.head).help(path)
                }
                Spacer(minLength: 12)
                if let date = SessionLauncher.timestamp(session.lastActivity ?? session.date) {
                    Text(date.formatted(.relative(presentation: .numeric, unitsStyle: .abbreviated))).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                }
                IconView(session.id == currentID ? .check : .arrowRight, size: 12).foregroundStyle(theme.muted)
            }
            .padding(.horizontal, 14).padding(.vertical, 14).contentShape(Rectangle())
            .background(highlighted ? theme.accent.opacity(0.14) : Color.clear, in: RoundedRectangle(cornerRadius: 4))
        }.buttonStyle(.plain)
        .contextMenu { SessionCatalogMenu(session: session, rename: rename, delete: delete) }
        .accessibilityLabel("\(session.title), \(path)")
        .accessibilityAddTraits(highlighted ? .isSelected : [])
    }

    // MARK: Shared

    private var selectedSession: SessionExcerpt? {
        results.first(where: { $0.id == selection.highlightedID }) ?? results.first
    }

    private func openHighlighted() {
        guard focusRequest, let session = selectedSession else { return }
        open(session)
    }

    @ViewBuilder private var emptyState: some View {
        VStack(spacing: 10) {
            if let error {
                Text("Couldn’t connect to Kit").font(.kit(size: 15, weight: .medium))
                Text(error).font(.kit(size: 13)).foregroundStyle(theme.muted).multilineTextAlignment(.center)
                Button("Retry connection", action: refresh)
            } else if connecting || !connected {
                Text("Loading sessions…").foregroundStyle(theme.muted)
            } else if !selection.query.isEmpty {
                Text("No matching sessions").font(.kit(size: 15, weight: .medium))
                Text("Try another name or folder.").foregroundStyle(theme.muted)
            } else {
                Text("No sessions yet").font(.kit(size: 15, weight: .medium))
                Text("Choose New session to get started.").foregroundStyle(theme.muted)
            }
        }.font(.kit(size: 13)).frame(maxWidth: .infinity)
    }
}

/// The launcher's filled button for starting a session.
private struct NewSessionButton: View {
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                IconView(.plus, size: 13)
                Text("New session").font(.kit(size: 13, weight: .medium))
            }
        }.buttonStyle(Style())
    }

    private struct Style: ButtonStyle {
        @Environment(\.mica) private var theme
        @Environment(\.isEnabled) private var enabled

        func makeBody(configuration: Configuration) -> some View {
            configuration.label
                .foregroundStyle(theme.surface)
                .padding(.horizontal, 12).frame(height: 30)
                .background(theme.text.opacity(configuration.isPressed ? 0.8 : 1), in: RoundedRectangle(cornerRadius: 3))
                .contentShape(Rectangle())
                .opacity(enabled ? 1 : 0.45)
        }
    }
}

/// A launcher row: the session's status, title, folder, and either what it
/// is doing or how long ago it was active.
private struct LauncherRow: View {
    @Environment(\.mica) private var theme
    let session: SessionExcerpt
    let status: SessionTabStatus
    let highlighted: Bool
    let open: () -> Void
    let rename: ((SessionExcerpt) -> Void)?
    let delete: ((SessionExcerpt) -> Void)?
    @State private var hovering = false

    var body: some View {
        let path = session.cwd ?? session.workspace
        Button(action: open) {
            HStack(spacing: 10) {
                SessionTabStatusMarker(status: status, finished: false).frame(width: 14)
                HStack(spacing: 8) {
                    Text(session.title).lineLimit(1).truncationMode(.tail)
                    if session.isTemporary == true {
                        Text("Temporary").font(.kit(size: 11)).foregroundStyle(theme.muted)
                    }
                }
                .font(.kit(size: 13))
                .frame(maxWidth: .infinity, alignment: .leading)
                Text(session.workspace).font(.kit(size: 11, design: .monospaced))
                    .foregroundStyle(theme.muted).lineLimit(1).help(WorkspaceLocation.homeRelative(path))
                Text(trailing).font(.kit(size: 12)).foregroundStyle(theme.muted)
                    .lineLimit(1).frame(width: 76, alignment: .trailing)
            }
            .padding(.horizontal, 10).frame(height: 30)
            .background(background, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .contextMenu { SessionCatalogMenu(session: session, rename: rename, delete: delete) }
        .accessibilityLabel("\(session.title), \(path)")
        .accessibilityValue(status.label ?? "")
        .accessibilityAddTraits(highlighted ? .isSelected : [])
    }

    private var trailing: String {
        switch status {
        case .running: "running"
        case .awaitingResponse: "waiting"
        case .idle: SessionLauncher.timestamp(session.lastActivity ?? session.date).map { SessionLauncher.age(of: $0) } ?? ""
        }
    }

    private var background: Color {
        highlighted ? theme.accent.opacity(0.14) : hovering ? theme.hover : .clear
    }
}

/// Rename and delete for a session in a catalog list.
private struct SessionCatalogMenu: View {
    let session: SessionExcerpt
    let rename: ((SessionExcerpt) -> Void)?
    let delete: ((SessionExcerpt) -> Void)?

    var body: some View {
        if let rename { Button { rename(session) } label: { Label { Text("Rename…") } icon: { Image(.pencilSimple) } } }
        if let delete { Button(role: .destructive) { delete(session) } label: { Label { Text(session.isTemporary == true ? "Dispose…" : "Delete…") } icon: { Image(.trash) } } }
    }
}
