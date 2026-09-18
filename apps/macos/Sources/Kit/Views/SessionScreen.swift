import SwiftUI

struct SessionScreen: View {
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var themeConfiguration = ""
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.openWindow) private var openWindow
    let windows: SessionWindowRegistry
    var onSelection: (String) -> Void = { _ in }
    @State private var state: SessionStore

    init(fixture: Fixture, sessionID: String? = nil, windows: SessionWindowRegistry, client: (any SessionClient)? = nil, onSelection: @escaping (String) -> Void = { _ in }) {
        self.windows = windows
        self.onSelection = onSelection
        // SwiftUI can construct this value repeatedly without retaining its State.
        // Selecting here must not start a stream; attachment belongs to onAppear.
        _state = State(initialValue: SessionStore(fixture: fixture, client: client, sessionID: sessionID))
    }

    var body: some View {
        @Bindable var ui = state.ui
        let windowScheme: ColorScheme? = appearance == "system" ? nil : appearance == "dark" ? .dark : .light
        let isDark = appearance == "dark" || (appearance == "system" && colorScheme == .dark)
        let theme = ThemeConfiguration.decode(themeConfiguration).theme(dark: isDark)
        let serverID = state.serverID
        let content = VStack(spacing: 0) {
            if state.unavailable {
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("Session unavailable").fontWeight(.medium)
                        Text("This session is no longer available on its server. Your transcript, draft, and workspace are retained.")
                            .foregroundStyle(theme.muted)
                        if state.connectionState != .unavailable && state.connectionState != .loading {
                            Text(state.connectionStatus).foregroundStyle(theme.warning)
                        }
                    }
                    Spacer()
                    Button(state.connectionState == .loading ? "Retrying…" : "Retry") { state.attach() }
                        .disabled(state.connectionState == .loading)
                    Button("Open another session") {
                        windows.showLauncher { openWindow(id: "session", value: $0) }
                    }
                }.font(.kit(size: 12)).padding(16).background(theme.raised)
                Rule()
            }
            Group {
                if state.selected == nil {
                    VStack(spacing: 16) {
                        if !state.selectedID.isEmpty && !state.unavailable {
                            Text(state.connectionState == .loading ? "Opening session…" : "Couldn’t open session").font(.title2)
                            Text("The session may have been deleted or its server may be unavailable.")
                                .foregroundStyle(.secondary)
                            Text(state.connectionStatus).font(.caption)
                            Button("Retry") { state.attach() }
                        }
                        SessionChooserView(state: state)
                    }.frame(minWidth: 490)
                } else {
                    WorkspaceLayout(state: state).frame(minWidth: 490)
                }
            }
        }
        return content
        .background(SelectionActions(theme: theme, enabled: !state.ui.palette && !state.ui.filePicker) { text in
            state.ui.draft = MessageText.quote(text, draft: state.ui.draft)
            state.ui.composerFocus += 1
        })
        .onReceive(NotificationCenter.default.publisher(for: .kitSessionDeleted)) { notification in
            if let identity = notification.object as? SessionIdentity { state.sessionDeleted(identity) }
        }
        .onAppear { state.attach(); onSelection(state.selectedID) }
        .onChange(of: state.selectedID) { onSelection(state.selectedID) }
        .onDisappear { state.detach() }
        .navigationTitle(state.selected?.title ?? "Kit")
        .navigationSubtitle((state.isTemporary ? "Temporary · " : "") + (state.selected?.workspace ?? ""))
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                Button {
                    windows.showLauncher { openWindow(id: "session", value: $0) }
                } label: { Image(systemName: "plus") }
                .help("New tab").accessibilityLabel("New tab")
                Button { state.ui.workspace.open(.review) } label: { Image(systemName: "chevron.left.forwardslash.chevron.right") }
                    .help("Diff").accessibilityLabel("Diff").disabled(!(state.catalogClient is any DiffClient))
                Button { state.ui.workspace.open(.scratchpad) } label: { Image(systemName: "note.text") }
                    .help("Scratchpad").accessibilityLabel("Scratchpad").disabled(!state.isDemo)
                Button { state.ui.subagentsPresented.toggle() } label: { Image(systemName: "person.2") }
                    .help("Subagents").accessibilityLabel("Subagents")
                    .popover(isPresented: $ui.subagentsPresented, arrowEdge: .bottom) {
                        SubagentsPopover(state: state)
                            .environment(\.mica, theme)
                            .preferredColorScheme(isDark ? .dark : .light)
                    }
                Button { state.showFiles(intent: "open") } label: { Image(systemName: "doc") }
                    .help("Open file").accessibilityLabel("Open file").disabled(state.unavailable)
                Button { state.ui.palette = true } label: { Image(systemName: "command") }
                    .help("Commands (⌘K)").accessibilityLabel("Open command palette")

            }
        }
        .toolbarBackground(theme.raised, for: .windowToolbar)
        .toolbarBackgroundVisibility(.visible, for: .windowToolbar)
        .font(.kit(size: 13))
        .foregroundStyle(theme.text)
        .background(theme.surface)
        .environment(\.mica, theme)
        .environment(\.transcriptSessionLink, TranscriptSessionLink(serverID: serverID) { id in
            windows.openSession(id, serverID: serverID) { openWindow(id: "session", value: $0) }
        })
        .font(.kit(size: 13))
        .tint(theme.accent)
        .preferredColorScheme(windowScheme)
        .frame(minWidth: 800, minHeight: 640)
        .disabled(state.ui.palette || state.ui.filePicker)
        .accessibilityHidden(state.ui.palette || state.ui.filePicker)
        .overlay {
            if state.ui.palette || state.ui.filePicker {
                GeometryReader { geometry in
                    ZStack(alignment: .top) {
                        Color.clear.contentShape(Rectangle()).onTapGesture {
                            state.ui.palette = false; state.ui.filePicker = false; state.ui.composerFocus += 1
                        }
                        Group {
                            if state.ui.palette { CommandPalette(state: state) { id in
                                windows.openSession(id, serverID: state.serverID) { openWindow(id: "session", value: $0) }
                            } }
                            else { FileMentionPicker(state: state) }
                        }
                        .environment(\.mica, theme)
        .font(.kit(size: 13))
                        .preferredColorScheme(windowScheme)
                        .clipShape(RoundedRectangle(cornerRadius: 12))
                        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(theme.border, lineWidth: 1))
                        .shadow(color: .black.opacity(0.18), radius: 20, y: 8)
                        .padding(.top, min(100, geometry.size.height * 0.14))
                    }
                }
            }
        }
        .background {
            Button("Open command palette") { state.ui.palette = true }
                .keyboardShortcut("k", modifiers: .command).hidden()
        }
        .background {
            SessionWindowBridge(registry: windows, serverID: state.serverID, sessionID: state.selectedID,
                                sessions: state.sessions, colorScheme: windowScheme,
                                tabStatus: state.tabStatus, sessionTitle: state.selected?.title ?? "Kit") {
                openWindow(id: "session", value: $0)
            }.frame(width: 0, height: 0)
        }
        .onDisappear { state.stop() }
    }

}
