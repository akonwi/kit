import AppKit
import SwiftUI

struct SessionLauncherView: View {
    @Bindable var app: AppModel
    let open: (SessionExcerpt) -> Void
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var themeConfiguration = ""
    @Environment(\.colorScheme) private var colorScheme

    @State private var refreshRequest = 0
    @State private var creating = false
    @State private var target: SessionExcerpt?
    @State private var deleting = false

    var body: some View {
        let scheme: ColorScheme? = appearance == "system" ? nil : appearance == "dark" ? .dark : .light
        let dark = scheme == .dark || (scheme == nil && colorScheme == .dark)
        let theme = ThemeConfiguration.decode(themeConfiguration).theme(dark: dark)
        SessionLauncherContent(sessions: app.sessions, connecting: app.connecting,
                               connected: app.client != nil, error: app.error, open: open, refresh: { refreshRequest += 1 },
                               create: { creating = true }, focusRequest: target == nil,
                               rename: { deleting = false; target = $0 },
                               delete: { deleting = true; target = $0 })
        .modifier(SessionCatalogActions(client: app.client, refresh: { await app.refreshSessions() },
                                        target: $target, deleting: $deleting))
        .environment(\.mica, theme)
        .foregroundStyle(theme.text)
        .background(theme.surface)
        .tint(theme.accent)
        .preferredColorScheme(scheme)
        .navigationTitle("Kit")
        .toolbarBackground(theme.surface, for: .windowToolbar)
        .toolbarBackgroundVisibility(.visible, for: .windowToolbar)
        .frame(minWidth: 680, minHeight: 540)
        .sheet(isPresented: $creating) {
            if let client = app.client as? any SessionCreationClient {
                NewSessionView(client: client, suggestedModel: app.sessions.first?.model) { session in
                    app.recordCreated(session)
                    open(session)
                }.environment(\.mica, theme)
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .kitSessionDeleted)) { notification in
            if let identity = notification.object as? SessionIdentity { app.sessionDeleted(identity) }
        }
        .task(id: refreshRequest) { await app.refreshSessions() }
    }
}

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
    var rename: ((SessionExcerpt) -> Void)? = nil
    var delete: ((SessionExcerpt) -> Void)? = nil
    @Environment(\.mica) private var theme
    @State private var selection = SessionLauncher()
    @FocusState private var searchFocused: Bool

    private static let lightLogo = Bundle.module.url(forResource: "logo-light", withExtension: "png").flatMap { NSImage(contentsOf: $0) }
    private static let darkLogo = Bundle.module.url(forResource: "logo-dark", withExtension: "png").flatMap { NSImage(contentsOf: $0) }
    static func logo(dark: Bool) -> NSImage? { dark ? darkLogo : lightLogo }

    private var results: [SessionExcerpt] { selection.results(in: sessions) }

    var body: some View {
        VStack(alignment: .leading, spacing: 26) {
            if !compact { HStack {
            if let logo = Self.logo(dark: theme.dark) {
                Image(nsImage: logo).resizable().scaledToFit().frame(width: 96, height: 72)
                    .accessibilityLabel("Kit")
            }
            Spacer()
            if let create {
                Button(action: create) { Label("New session", systemImage: "plus") }
                    .disabled(!connected || connecting)
            }
            }
            }
            VStack(spacing: 0) {
                HStack(spacing: 10) {
                    Image(systemName: "magnifyingglass").foregroundStyle(theme.muted)
                    TextField("Search sessions or directories", text: $selection.query)
                        .textFieldStyle(.plain).focused($searchFocused)
                        .onSubmit { openHighlighted() }
                    if !selection.query.isEmpty {
                        Button { selection.query = "" } label: { Image(systemName: "xmark.circle.fill") }
                            .buttonStyle(.plain).foregroundStyle(theme.muted).help("Clear search")
                    }
                }
                .font(.kit(size: 14)).padding(14)
                .overlay(alignment: .bottom) { Rectangle().fill(searchFocused ? theme.accent : theme.border).frame(height: 1) }
                HStack {
                    Text("Recent sessions").font(.kit(size: 12, weight: .medium))
                    Spacer()
                    if connecting { KitSpinner() }
                    else {
                        Button(action: refresh) { Image(systemName: "arrow.clockwise") }
                            .buttonStyle(.plain).help("Refresh sessions").accessibilityLabel("Refresh sessions")
                    }
                }.foregroundStyle(theme.muted).padding(.top, 24).padding(.bottom, 10)
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(spacing: 4) {
                            ForEach(results) { session in
                                row(session).id(session.id)
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
                    Label("Connection unavailable", systemImage: "exclamationmark.circle")
                        .help(error)
                    Spacer()
                    Button("Retry connection", action: refresh)
                }.font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
        }
        .padding(compact ? 20 : 40).frame(maxWidth: 740, maxHeight: 680)
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

    private var selectedSession: SessionExcerpt? {
        results.first(where: { $0.id == selection.highlightedID }) ?? results.first
    }

    private func openHighlighted() {
        guard focusRequest, let session = selectedSession else { return }
        open(session)
    }

    private func row(_ session: SessionExcerpt) -> some View {
        let highlighted = selection.highlightedID == session.id
        let path = session.cwd ?? session.workspace
        return Button { open(session) } label: {
            HStack(spacing: 14) {
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        Text(session.title).font(.kit(size: 14, weight: .medium)).lineLimit(1)
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
                Image(systemName: session.id == currentID ? "checkmark" : compact ? "arrow.right" : "arrow.up.forward").font(.system(size: 11)).foregroundStyle(theme.muted)
            }
            .padding(.horizontal, 14).padding(.vertical, 14).contentShape(Rectangle())
            .background(highlighted ? theme.accent.opacity(0.14) : Color.clear, in: RoundedRectangle(cornerRadius: 4))
        }.buttonStyle(.plain)
        .contextMenu {
            if let rename { Button("Rename…", systemImage: "pencil") { rename(session) } }
            if let delete { Button(session.isTemporary == true ? "Dispose…" : "Delete…", systemImage: "trash", role: .destructive) { delete(session) } }
        }
        .accessibilityLabel("\(session.title), \(path)")
        .accessibilityAddTraits(highlighted ? .isSelected : [])
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
                Text("Try another name or directory.").foregroundStyle(theme.muted)
            } else {
                Text("No sessions yet").font(.kit(size: 15, weight: .medium))
                Text("Choose New session to get started.").foregroundStyle(theme.muted)
            }
        }.font(.kit(size: 13)).frame(maxWidth: .infinity)
    }
}
