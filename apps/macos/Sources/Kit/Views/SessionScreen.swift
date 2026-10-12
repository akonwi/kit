import SwiftUI

/// Requests that open sessions as tabs of the current window.
struct SessionNavigation {
    var openSession: (String) -> Void = { _ in }
    var openLauncher: () -> Void = {}
}

extension EnvironmentValues {
    @Entry var sessionNavigation = SessionNavigation()
}

/// One session tab's content. The tab owns the store and its stream; this
/// view only presents it.
struct SessionScreen: View {
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var themeConfiguration = ""
    @Environment(\.installedThemes) private var installedThemes
    @Environment(\.sessionNavigation) private var navigation
    @Bindable var state: SessionStore
    var onSelection: (String) -> Void = { _ in }

    var body: some View {
        @Bindable var ui = state.ui
        let windowScheme = SystemAppearance.shared.resolve(appearance)
        let isDark = windowScheme == .dark
        let theme = ThemeConfiguration.decode(themeConfiguration).theme(dark: isDark, installed: installedThemes)
        let serverID = state.serverID
        // An open session's tiles sit on the window frame; anything else
        // fills the window's single tile.
        let tiled = state.selected != nil
        let content = VStack(spacing: tiled ? tileGap : 0) {
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
                    Button("Open another session") { navigation.openLauncher() }
                }.font(.kit(size: 12)).padding(16).background(theme.raised)
                .clipShape(RoundedRectangle(cornerRadius: tiled ? 4 : 0))
                .overlay { if tiled { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) } }
                if !tiled { Rule() }
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
            state.ui.workspace.select(.conversation)
            state.ui.composerFocus += 1
            state.ui.composerFocusAtEndRequest = state.ui.composerFocus
        })
        .onReceive(NotificationCenter.default.publisher(for: .kitSessionDeleted)) { notification in
            if let identity = notification.object as? SessionIdentity { state.sessionDeleted(identity) }
        }
        .onChange(of: state.selectedID) { onSelection(state.selectedID) }
        .onChange(of: ForkFirstTurnFailures.shared.revision) { state.applyForkFirstTurnFailure() }
        .font(.kit(size: 13))
        .foregroundStyle(theme.text)
        .background(tiled ? .clear : theme.surface)
        .environment(\.mica, theme)
        .environment(\.transcriptSessionLink, TranscriptSessionLink(serverID: serverID) { id in
            navigation.openSession(id)
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
                GeometryReader { proxy in
                    ZStack {
                        Color.clear.contentShape(Rectangle()).onTapGesture {
                            state.ui.palette = false; state.ui.filePicker = false
                        }
                        SessionPickerOverlayLayout(palette: state.ui.palette, frame: proxy.frame(in: .global)) {
                            Group {
                                if state.ui.palette { CommandPalette(state: state) { id in
                                    navigation.openSession(id)
                                } }
                                else { FileMentionPicker(state: state) }
                            }
                            .environment(\.mica, theme)
                            .font(.kit(size: 13))
                            .preferredColorScheme(windowScheme)
                            .clipShape(RoundedRectangle(cornerRadius: 4))
                            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border, lineWidth: 1))
                            .shadow(color: .black.opacity(theme.dark ? 0.5 : 0.18), radius: 24, y: 18)
                        }
                    }
                }
            }
        }
    }
}

// Position the measured picker synchronously, including when the palette changes
// to a taller nested screen at the minimum window height.
struct SessionPickerOverlayLayout: Layout {
    let palette: Bool
    /// The overlay's frame in window coordinates. The palette is placed
    /// against the window, not the tiles it covers.
    var frame: CGRect = .zero

    /// The palette's distance from the top of the window: a quarter of its
    /// height, like the TUI's picker placement.
    static func paletteTop(windowHeight: CGFloat) -> CGFloat { windowHeight * 0.25 }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        CGSize(width: proposal.width ?? 0, height: proposal.height ?? 0)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard let child = subviews.first else { return }
        let size = child.sizeThatFits(.unspecified)
        let x = palette
            ? Self.leadingOffset(availableWidth: bounds.width, pickerWidth: size.width, originX: frame.minX,
                                 // The window's trailing tile gap lies outside the overlay.
                                 windowWidth: frame.maxX + tileGap)
            : max(0, (bounds.width - size.width) / 2)
        // The window's bottom tile gap lies outside the overlay.
        let top = Self.topOffset(availableHeight: bounds.height, pickerHeight: size.height, palette: palette,
                                 originY: frame.minY, windowHeight: frame.maxY + tileGap)
        child.place(at: CGPoint(x: bounds.minX + x, y: bounds.minY + top), proposal: ProposedViewSize(size))
    }

    static func topOffset(availableHeight: CGFloat, pickerHeight: CGFloat, palette: Bool,
                          originY: CGFloat = 0, windowHeight: CGFloat? = nil) -> CGFloat {
        if !palette { return min(100, availableHeight * 0.14) }
        let window = windowHeight ?? originY + availableHeight
        // Leave room for the panel's downward shadow as well as its content.
        return max(0, min(paletteTop(windowHeight: window) - originY, availableHeight - pickerHeight - 32))
    }

    /// Centres the palette on the window while keeping it inside the overlay.
    static func leadingOffset(availableWidth: CGFloat, pickerWidth: CGFloat, originX: CGFloat, windowWidth: CGFloat) -> CGFloat {
        let centred = windowWidth / 2 - originX - pickerWidth / 2
        return min(max(0, centred), max(0, availableWidth - pickerWidth))
    }
}
