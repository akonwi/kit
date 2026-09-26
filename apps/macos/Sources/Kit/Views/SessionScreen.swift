import SwiftUI

struct SessionScreen: View {
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var themeConfiguration = ""
    @Environment(\.installedThemes) private var installedThemes
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
        let windowScheme = SystemAppearance.shared.resolve(appearance)
        let isDark = windowScheme == .dark
        let theme = ThemeConfiguration.decode(themeConfiguration).theme(dark: isDark, installed: installedThemes)
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
            state.ui.composerFocusAtEndRequest = state.ui.composerFocus
        })
        .onReceive(NotificationCenter.default.publisher(for: .kitSessionDeleted)) { notification in
            if let identity = notification.object as? SessionIdentity { state.sessionDeleted(identity) }
        }
        .onAppear { state.attach(); onSelection(state.selectedID) }
        .onChange(of: state.selectedID) { onSelection(state.selectedID) }
        .onDisappear { state.detach() }
        .navigationTitle(state.selected?.title ?? "Kit")
        .navigationSubtitle((state.isTemporary ? "Temporary · " : "") + (state.selected?.workspace ?? ""))
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
        .overlay(alignment: .topTrailing) {
            if state.selected != nil {
                SessionFeedbackView(feedback: state.feedback)
                    .environment(\.mica, theme)
                    .id(state.serverID + "|" + state.selectedID)
                    .padding(.top, 16).padding(.trailing, 16)
            }
        }
        .disabled(state.ui.palette || state.ui.filePicker)
        .accessibilityHidden(state.ui.palette || state.ui.filePicker)
        .overlay {
            if state.ui.palette || state.ui.filePicker {
                GeometryReader { _ in
                    ZStack {
                        Color.clear.contentShape(Rectangle()).onTapGesture {
                            state.ui.palette = false; state.ui.filePicker = false; state.ui.composerFocus += 1
                        }
                        SessionPickerOverlayLayout(palette: state.ui.palette) {
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
                        }
                    }
                }
            }
        }
        .background {
            Button("Open command palette") { state.ui.palette = true }
                .keyboardShortcut("p", modifiers: .command).hidden()
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

// Position the measured picker synchronously, including when the palette changes
// to a taller nested screen at the minimum window height.
struct SessionPickerOverlayLayout: Layout {
    let palette: Bool

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        CGSize(width: proposal.width ?? 0, height: proposal.height ?? 0)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard let child = subviews.first else { return }
        let size = child.sizeThatFits(.unspecified)
        let top = Self.topOffset(availableHeight: bounds.height, pickerHeight: size.height, palette: palette)
        child.place(at: CGPoint(x: bounds.minX + max(0, (bounds.width - size.width) / 2), y: bounds.minY + top),
                    proposal: ProposedViewSize(size))
    }

    static func topOffset(availableHeight: CGFloat, pickerHeight: CGFloat, palette: Bool) -> CGFloat {
        if !palette { return min(100, availableHeight * 0.14) }
        let preferred = availableHeight * 0.30
        // Leave room for the panel's downward shadow as well as its content.
        return min(preferred, max(0, availableHeight - pickerHeight - 32))
    }
}
